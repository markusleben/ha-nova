package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// purgeRelayTokenRead is a test seam for the native relay token read.
var purgeRelayTokenRead = readRelayAuthToken

// pinnedPurgeTargets collects what a full purge inspects, with the device
// credential backend the purge itself pins (same order as
// finalizeLocalUninstallWithProgressUnlocked). The caller must call release.
func pinnedPurgeTargets(
	paths runtimePaths,
) (targets []profilePurgeTarget, cloudTargets []cloudPurgeTarget, release func(), err error) {
	retirementProfiles, err := deviceCredentialRetirementCheckpointProfiles(paths)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := validatePurgeServiceTokenFile(paths); err != nil {
		return nil, nil, nil, fmt.Errorf("cannot check relay token cleanup: %w", err)
	}
	cloudTargets, err = collectCloudPurgeTargets(paths.ConfigFile)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("cannot check Home Assistant Link authorization: %w", err)
	}
	targets, err = collectProfilePurgeTargets(paths)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("cannot check device credential cleanup: %w", err)
	}
	release = pinPurgeDeviceBackend(paths, targets, cloudTargets, retirementProfiles, nil)
	return targets, cloudTargets, release, nil
}

// purgeSecureStorageSessionError returns the raw read error when this
// terminal can't reach the secure storage a full purge reads (SSH,
// administrator; rc2 lane N1). A missing credential is fine; any other
// problem is left to the purge itself, which reports it.
func purgeSecureStorageSessionError(paths runtimePaths) error {
	if (secureStorageSessionPlatformOS != "windows" && secureStorageSessionPlatformOS != "darwin") ||
		nativeSecretPromptBaseContextAvailable() {
		return nil
	}
	return purgeSecureStorageReadError(paths, secureStorageSessionProblem)
}

// purgeSecureStorageReadError returns the first error, for which problem
// holds, from the secure-storage reads a full purge needs; nil when the
// targets can't be collected (the purge reports that itself).
func purgeSecureStorageReadError(paths runtimePaths, problem func(error) bool) error {
	targets, cloudTargets, release, err := pinnedPurgeTargets(paths)
	if err != nil {
		return nil
	}
	defer release()
	for _, target := range targets {
		for _, service := range []string{
			deviceCredentialPendingServiceForProfile(target.name),
			deviceCredentialServiceForProfile(target.name),
		} {
			if _, err := secretGet(service); problem(err) {
				return err
			}
		}
	}
	// An interrupted keyring-to-file move still has keyring copies to check
	// (resumeKeyringDeviceCredentialCleanup reads them from the keyring).
	if cleanup, exists, err := readKeyringDeviceCredentialCleanup(); err == nil && exists {
		for _, service := range cleanup.Services {
			if _, _, err := readKeyringDeviceSecret(service); problem(err) {
				return err
			}
		}
	}
	// A file-backed device credential proves nothing about the native store.
	// Read what the purge itself must read there: the relay token (from its
	// file when one is configured; a keyring copy is then best-effort) and
	// each Link authorization.
	// The purge skips a relay token it can't reach
	// (purgeSkipsRelayTokenError), so this check does too.
	if _, err := purgeRelayTokenRead(); !purgeSkipsRelayTokenError(err) && problem(err) {
		return err
	}
	for _, target := range cloudTargets {
		store, err := newCloudSecretStoreForCLI(target.profileID)
		if err == nil {
			ctx, cancel := boundedNativeOAuthSecretContext(context.Background(), SecretStoreForbidUI)
			_, _, err = store.LoadCurrent(ctx, SecretStoreForbidUI)
			cancel()
		}
		if problem(err) {
			return err
		}
	}
	return nil
}

// purgeRefusedOutsideDesktop: the purge can't prove the device credential is
// gone from here, so it stops before changing anything and names the one fix.
// A purge that failed earlier for this reason clears the same way. rerun is
// the command that repeats this run.
func purgeRefusedOutsideDesktop(paths runtimePaths, mode uninstallMode, yes bool, rerun string) bool {
	if mode != uninstallModePurge {
		return false
	}
	err := purgeSecureStorageSessionError(paths)
	if err == nil {
		return purgeRefusedForUnreachableKeyring(paths, rerun) ||
			purgeRefusedForLockedKeyring(paths, yes, rerun) ||
			purgeRefusedForUnreadableRelayToken(paths, rerun)
	}
	printHumanErr("%s, so a full purge can't check this computer's saved HA NOVA credentials. This run changed nothing.", secureStorageSessionReason)
	printHumanWarn("Next step: %s, then run: %s", secureStorageSessionNextStep(), rerun)
	printHumanWarn("Details: %s", err)
	return true
}

// purgeKeyringLockedError returns the first read a full purge needs that a
// locked Linux keyring refuses. GDM autologin leaves the keyring locked after
// every boot (0.27 lane G4). nil when the purge reads only files.
func purgeKeyringLockedError(paths runtimePaths) error {
	if secureStorageSessionPlatformOS != "linux" {
		return nil
	}
	return purgeSecureStorageReadError(paths, func(err error) bool {
		// The Link store reports a keyring that isn't set up as locked too,
		// and a locked one it may not prompt for as UI-forbidden: ask the
		// keyring itself (read-only) which it is.
		if IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
			return isDesktopKeyringLockedError(deviceCredentialPreflight())
		}
		return !isDesktopKeyringInitializationRequiredError(err) &&
			(isDesktopKeyringLockedError(err) || IsCloudErrorCode(err, CloudErrSecretStoreLocked))
	})
}

// isLinuxKeyringOutOfReach: no session bus (SSH, a console login) or no
// Secret Service on it. Locked and not-set-up keyrings have their own paths.
func isLinuxKeyringOutOfReach(err error) bool {
	return isDesktopKeyringSessionUnavailableError(err) || isSessionBusUnreachableText(err) ||
		isDesktopKeyringUnavailableError(err) || isSecretServiceExitedText(err)
}

// purgeSkipsRelayTokenError: the purge skips the legacy relay token, with a
// note, when its keyring isn't running. On a file-backed install whose default
// server doesn't connect with it, it also skips a keyring this terminal can't
// reach (a headless server has no desktop to sign in to) or that isn't set up
// (it holds nothing). A locked one keeps its unlock step: it may still hold a
// device key from before the switch to files.
func purgeSkipsRelayTokenError(err error) bool {
	return isDesktopKeyringUnavailableError(err) ||
		(isLinuxKeyringOutOfReach(err) || isDesktopKeyringInitializationRequiredError(err)) &&
			deviceSecretFileBacked() && !relayTokenConnectsDefaultServer()
}

// relayTokenConnectsDefaultServer: the default server isn't paired and its
// relay takes the legacy token. A config that can't be read counts too.
func relayTokenConnectsDefaultServer() bool {
	paths, err := detectPaths()
	if err != nil {
		return true
	}
	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	return err != nil || cfg.RelaySecureBaseURL == "" && cfg.RelayBaseURL != ""
}

// purgeKeyringOutOfReachError returns the first read a full purge needs that
// fails because this terminal can't reach the Linux keyring. Without this
// check the purge removed part of HA NOVA and then failed there.
func purgeKeyringOutOfReachError(paths runtimePaths) error {
	if secureStorageSessionPlatformOS != "linux" {
		return nil
	}
	return purgeSecureStorageReadError(paths, isLinuxKeyringOutOfReach)
}

// keyringOutOfReachFromHere: the fix is a terminal on the desktop (SSH, sudo,
// no session bus), not restarting a keyring that isn't running.
func keyringOutOfReachFromHere(err error) bool {
	return !nativeSecretPromptBaseContextAvailable() ||
		isDesktopKeyringSessionUnavailableError(err) || isSessionBusUnreachableText(err)
}

// purgeRefusedForUnreachableKeyring: a full purge must read every saved key
// before it revokes or removes anything; from here it can't, so the run stops
// before any change and names one next step.
func purgeRefusedForUnreachableKeyring(paths runtimePaths, rerun string) bool {
	err := purgeKeyringOutOfReachError(paths)
	if err == nil {
		return false
	}
	resetLinkStorageProofForEarlyStop(paths, err)
	if keyringOutOfReachFromHere(err) {
		printHumanErr("HA NOVA can't reach this computer's keyring from this terminal (for example over SSH), so it can't remove its saved key. Nothing was changed.")
		printHumanWarn("Next step: %s. Then run: %s", secureStorageSessionNextStep(), rerun)
		return true
	}
	printHumanErr("This computer's keyring isn't running, so HA NOVA can't remove its saved key. Nothing was changed.")
	printHumanWarn("Next step: log out of the desktop and back in. Then run: %s", rerun)
	return true
}

// purgeRefusedForUnreadableRelayToken: the purge reads the legacy relay token
// late, after it removed client integrations and PATH. On Linux any read
// error it won't skip (applyUninstallTokenPolicy) stops the run up front
// instead, like the guided preflight does.
func purgeRefusedForUnreadableRelayToken(paths runtimePaths, rerun string) bool {
	if secureStorageSessionPlatformOS != "linux" {
		return false
	}
	// A configured token file is removed, not read; a keyring copy is then
	// best-effort.
	if _, configured, _ := relayAuthTokenFilePathFromConfig(); configured {
		return false
	}
	_, err := purgeRelayTokenRead()
	if err == nil || isMissingRelayAuthTokenError(err) || purgeSkipsRelayTokenError(err) {
		return false
	}
	resetLinkStorageProofForEarlyStop(paths, err)
	printHumanErr("HA NOVA can't read its saved key in this computer's keyring, so it can't remove it. Nothing was changed.")
	if plain := plainKeyringProblem(err); plain != "" {
		printHumanWarn("Next step: %s Then run: %s", strings.TrimSuffix(plain, " Then try again."), rerun)
		return true
	}
	printHumanWarn("Details: %s", err)
	printHumanWarn("Then run: %s", rerun)
	return true
}

// resetLinkStorageProofForEarlyStop keeps the relock contract of a purge
// that fails later: the next Link step must be unlock, not cleanup.
func resetLinkStorageProofForEarlyStop(paths runtimePaths, cause error) {
	if err := withClientMutationLock(paths, func() error {
		return resetVerifiedCloudStorageAfterPurgeRelock(paths, cause)
	}); err != nil {
		printHumanWarn("Could not reset the Home Assistant Link storage check: %s", err)
	}
}

var keyringUnlockTrigger = desktopKeyringLockedError("a full removal reads this computer's keyring")

// keyringUnlockWindowPossible: this terminal can show setup's unlock window
// (a desktop terminal, and not --yes, which asks nothing).
func keyringUnlockWindowPossible(yes bool) bool {
	return !yes && uninstallInteractive() && setupSecureStorageRecoveryAvailableNow(keyringUnlockTrigger)
}

// offerKeyringUnlockForPurge opens setup's unlock window (the desktop's own
// dialog; HA NOVA never sees the password) when a full purge would hit a
// locked keyring and this terminal can show it. It reports whether the
// window unlocked something, and stop=true when the user pressed Ctrl+C
// (Esc is "Not now"). The guided menu calls it first, so the Relay can still
// be asked whether the App may go too.
func offerKeyringUnlockForPurge(paths runtimePaths) (unlocked, stop bool) {
	if purgeKeyringLockedError(paths) == nil || !keyringUnlockWindowPossible(false) {
		return false, false
	}
	state := setupSecureStorageRecoveryState{unlockOnly: true}
	result, err := runSetupSecureStorageRecoveryFlow(
		bufio.NewReader(os.Stdin), os.Stdout, keyringUnlockTrigger, &state,
		setupSecureStorageRecoveryInitialAttempt,
	)
	switch {
	case errors.Is(err, errSetupExit):
		return false, true
	case errors.Is(err, errSetupBack):
		return false, false
	case err != nil:
		printHumanWarn("The keyring window didn't work: %s", keyringErrorText(err))
	}
	return err == nil && result == setupSecureStorageRecoveryRecovered, false
}

// keyringUnlockByHandStep works on GNOME and KDE, and with any keyring name.
const keyringUnlockByHandStep = "on this computer's desktop, open Passwords and Keys (or KDE Wallet) and unlock your login keyring, or log out and back in with your password"

// purgeRefusedForLockedKeyring: a full purge must read every saved key before
// it revokes or removes anything. A locked keyring gets the unlock window when
// this terminal can show it. Otherwise, or when it stays locked, the run stops
// before any change and names one next step.
func purgeRefusedForLockedKeyring(paths runtimePaths, yes bool, rerun string) bool {
	if purgeKeyringLockedError(paths) == nil {
		return false
	}
	possible := keyringUnlockWindowPossible(yes)
	opened, stop := false, false
	if possible {
		opened, stop = offerKeyringUnlockForPurge(paths)
	}
	lockedErr := purgeKeyringLockedError(paths)
	if lockedErr == nil && !stop {
		return false
	}
	if lockedErr != nil {
		resetLinkStorageProofForEarlyStop(paths, lockedErr)
	}
	if stop { // Ctrl+C: stop even if the keyring opened some other way
		printHumanInfo("Uninstall cancelled. Nothing was changed.")
		return true
	}
	switch {
	case yes && keyringUnlockWindowPossible(false):
		printHumanErr("This computer's keyring is locked, so HA NOVA can't remove its saved key. Nothing was changed.")
		printHumanWarn("Next step: run ha-nova uninstall --purge (without --yes) and pick Yes. A window opens to unlock the keyring.")
	case opened:
		printHumanErr("The keyring opened, but HA NOVA's key is still locked. Nothing was changed.")
		printHumanWarn("Next step: %s. Then run: %s", keyringUnlockByHandStep, rerun)
	case possible:
		printHumanErr("This computer's keyring is locked, so HA NOVA can't remove its saved key. Nothing was changed.")
		printHumanWarn("Next step: run %s again, pick Yes, and type your keyring password (usually your login password) in the window that opens.", rerun)
	default:
		printHumanErr("This computer's keyring is locked, so HA NOVA can't remove its saved key. Nothing was changed.")
		printHumanWarn("Next step: %s. Then run: %s", keyringUnlockByHandStep, rerun)
	}
	return true
}

// keyringRemovalProblem is a Linux keyring failure while HA NOVA removes its
// own keys (uninstall, server remove): the plain keyring text get and doctor
// print, not setup's "rerun ha-nova setup" (0.27 lane G4). It keeps the cause
// for error classification; action stays in the details only.
type keyringRemovalProblem struct {
	action string
	cause  error
}

func (e *keyringRemovalProblem) Error() string {
	return "HA NOVA can't remove its saved key. " + relayAuthTokenProblemMessage(e.cause)
}

func (e *keyringRemovalProblem) Unwrap() error { return e.cause }

// keyringRemovalError: a keyring that isn't set up keeps setup's text, since
// setup is its fix; anything that isn't a Linux keyring state too.
func keyringRemovalError(action string, err error) error {
	if !isLinuxKeyringError(err) || isDesktopKeyringInitializationRequiredError(err) {
		return relayAuthTokenSetupOperationError(action, err)
	}
	return &keyringRemovalProblem{action: action, cause: err}
}

// uninstallErrorText prints a keyring removal problem on its own: the steps
// that wrap it ("failed to validate device credential cleanup: …") mean
// nothing to the user.
func uninstallErrorText(err error) string {
	if text, ok := cloudAuthorizationStopText(err); ok {
		return text
	}
	var keyring *keyringRemovalProblem
	if !errors.As(err, &keyring) {
		return err.Error()
	}
	text := keyring.Error()
	// resetPurgeStorageProofAfterError may join a second failure: keep it.
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, other := range joined.Unwrap() {
			if !errors.As(other, new(*keyringRemovalProblem)) {
				text += "\n" + other.Error()
			}
		}
	}
	return text
}

// uninstallRerunCommand is the command that repeats this uninstall run.
func uninstallRerunCommand(yes, purge bool) string {
	switch {
	case yes:
		return "ha-nova uninstall --yes --purge"
	case purge:
		return "ha-nova uninstall --purge"
	}
	return "ha-nova uninstall"
}

// purgeReadyForWindowsHandoff reports the helper's own checks here first. A
// guided teardown that just ran keeps the old path: its evidence must reach
// the recovery status even if the helper then fails.
func purgeReadyForWindowsHandoff(paths runtimePaths, mode uninstallMode, teardown teardownOutcome, rerun string) bool {
	if mode != uninstallModePurge || teardown == teardownCompleted {
		return true
	}
	if err := validatePurgeBeforeWindowsHandoff(paths); err != nil {
		printHumanErr("The full purge can't start: %s", uninstallErrorText(err))
		printHumanWarn("The uninstall stopped before the background step. Fix the problem above, then run: %s", rerun)
		return false
	}
	return true
}

// validatePurgeBeforeWindowsHandoff runs the purge's read-only Link, device
// credential, and relay token checks (the guided preflight's) in this
// terminal. The detached Windows helper runs them again, but its failure
// would only show later in doctor.
func validatePurgeBeforeWindowsHandoff(paths runtimePaths) error {
	targets, cloudTargets, release, err := pinnedPurgeTargets(paths)
	if err != nil {
		return err
	}
	defer release()
	return validateUninstallSecureStorageBeforeGuidedTeardown(paths, cloudTargets, targets)
}
