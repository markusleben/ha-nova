package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
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
	// A file-backed device credential proves nothing about the native store.
	// Read what the purge itself must read there: the relay token (from its
	// file when one is configured; a keyring copy is then best-effort) and
	// each Link authorization.
	if _, err := purgeRelayTokenRead(); problem(err) {
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
		return purgeRefusedForLockedKeyring(paths, yes, rerun)
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
		// The Link store reports a keyring that isn't set up as locked too.
		return !isDesktopKeyringInitializationRequiredError(err) &&
			(isDesktopKeyringLockedError(err) || IsCloudErrorCode(err, CloudErrSecretStoreLocked))
	})
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
// window unlocked something. The guided menu calls it first, so the Relay
// can still be asked whether the App may go too.
func offerKeyringUnlockForPurge(paths runtimePaths) bool {
	if purgeKeyringLockedError(paths) == nil || !keyringUnlockWindowPossible(false) {
		return false
	}
	state := setupSecureStorageRecoveryState{unlockOnly: true}
	result, err := runSetupSecureStorageRecoveryFlow(
		bufio.NewReader(os.Stdin), os.Stdout, keyringUnlockTrigger, &state,
		setupSecureStorageRecoveryInitialAttempt,
	)
	return err == nil && result == setupSecureStorageRecoveryRecovered
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
	opened := possible && offerKeyringUnlockForPurge(paths)
	if opened && purgeKeyringLockedError(paths) == nil {
		return false
	}
	switch {
	case yes && keyringUnlockWindowPossible(false):
		printHumanErr("This computer's keyring is locked, so HA NOVA can't remove its saved key. Nothing was changed.")
		printHumanWarn("Next step: run ha-nova uninstall --purge (without --yes) and answer Y. A window opens to unlock the keyring.")
	case opened:
		printHumanErr("The keyring opened, but HA NOVA's key is still locked. Nothing was changed.")
		printHumanWarn("Next step: %s. Then run: %s", keyringUnlockByHandStep, rerun)
	case possible:
		printHumanErr("This computer's keyring is locked, so HA NOVA can't remove its saved key. Nothing was changed.")
		printHumanWarn("Next step: run %s again, answer Y, and type your login password in the window that opens.", rerun)
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
		printHumanErr("The full purge can't start: %s", err)
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
