package main

import (
	"bufio"
	"context"
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
// A purge that failed earlier for this reason clears the same way.
func purgeRefusedOutsideDesktop(paths runtimePaths, mode uninstallMode, yes bool) bool {
	if mode != uninstallModePurge {
		return false
	}
	err := purgeSecureStorageSessionError(paths)
	if err == nil {
		return purgeRefusedForLockedKeyring(paths, yes)
	}
	printHumanErr("%s, so a full purge can't check this computer's saved HA NOVA credentials. This run changed nothing.", secureStorageSessionReason)
	printHumanWarn("Next step: %s, then run: %s", secureStorageSessionNextStep(), purgeRerunCommand(yes))
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
		return isDesktopKeyringLockedError(err) || IsCloudErrorCode(err, CloudErrSecretStoreLocked)
	})
}

// purgeRefusedForLockedKeyring: a full purge must read every saved key before
// it revokes or removes anything. A locked keyring gets setup's unlock window
// (the desktop's own dialog; HA NOVA never sees the password) when this
// terminal can show it. Otherwise, or when it stays locked, the run stops
// before any change and names one next step.
func purgeRefusedForLockedKeyring(paths runtimePaths, yes bool) bool {
	if purgeKeyringLockedError(paths) == nil {
		return false
	}
	trigger := desktopKeyringLockedError("a full removal reads this computer's keyring")
	if !yes && uninstallInteractive() && setupSecureStorageRecoveryAvailableNow(trigger) {
		var state setupSecureStorageRecoveryState
		result, err := runSetupSecureStorageRecoveryFlow(
			bufio.NewReader(os.Stdin), os.Stdout, trigger, &state,
			setupSecureStorageRecoveryInitialAttempt,
		)
		if err == nil && result == setupSecureStorageRecoveryRecovered &&
			purgeKeyringLockedError(paths) == nil {
			return false
		}
	}
	printHumanErr("This computer's keyring is locked, so HA NOVA can't remove its saved key. Nothing was changed.")
	printHumanWarn("Next step: unlock the keyring (log in to the desktop, or open Passwords and Keys), then run: %s", purgeRerunCommand(yes))
	return true
}

// keyringRemovalError words a Linux keyring failure while HA NOVA removes
// its own keys (uninstall, server remove) with the plain text get and doctor
// print, not setup's "rerun ha-nova setup" (0.27 lane G4). A keyring that
// isn't set up keeps setup's text: setup is its fix.
func keyringRemovalError(action string, err error) error {
	if !isLinuxKeyringError(err) || isDesktopKeyringInitializationRequiredError(err) {
		return relayAuthTokenSetupOperationError(action, err)
	}
	return fmt.Errorf("cannot %s: %s", action, relayAuthTokenProblemMessage(err))
}

func purgeRerunCommand(yes bool) string {
	if yes {
		return "ha-nova uninstall --yes --purge"
	}
	return "ha-nova uninstall --purge"
}

// purgeReadyForWindowsHandoff reports the helper's own checks here first. A
// guided teardown that just ran keeps the old path: its evidence must reach
// the recovery status even if the helper then fails.
func purgeReadyForWindowsHandoff(paths runtimePaths, mode uninstallMode, teardown teardownOutcome, yes bool) bool {
	if mode != uninstallModePurge || teardown == teardownCompleted {
		return true
	}
	if err := validatePurgeBeforeWindowsHandoff(paths); err != nil {
		printHumanErr("The full purge can't start: %s", err)
		printHumanWarn("The uninstall stopped before the background step. Fix the problem above, then run: %s", purgeRerunCommand(yes))
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
