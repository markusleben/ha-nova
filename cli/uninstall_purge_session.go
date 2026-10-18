package main

import "fmt"

// pinnedPurgeDeviceTargets collects the device credential slots a full purge
// inspects, on the backend the purge itself pins (same order as
// finalizeLocalUninstallWithProgressUnlocked). The caller must call release.
func pinnedPurgeDeviceTargets(
	paths runtimePaths,
) (targets []profilePurgeTarget, release func(), err error) {
	retirementProfiles, err := deviceCredentialRetirementCheckpointProfiles(paths)
	if err != nil {
		return nil, nil, err
	}
	if err := validatePurgeServiceTokenFile(paths); err != nil {
		return nil, nil, fmt.Errorf("failed to remove relay auth token: %w", err)
	}
	cloudTargets, err := collectCloudPurgeTargets(paths.ConfigFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to inspect Home Assistant Link authorization: %w", err)
	}
	targets, err = collectProfilePurgeTargets(paths)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to inspect device credential cleanup: %w", err)
	}
	release = pinPurgeDeviceBackend(paths, targets, cloudTargets, retirementProfiles, nil)
	return targets, release, nil
}

// purgeSecureStorageSessionError returns the raw read error when this
// terminal can't reach the secure storage a full purge must inspect (SSH,
// administrator; rc2 lane N1). A missing credential is fine; any other
// problem is left to the purge itself, which reports it.
func purgeSecureStorageSessionError(paths runtimePaths) error {
	targets, release, err := pinnedPurgeDeviceTargets(paths)
	if err != nil {
		return nil
	}
	defer release()
	for _, target := range targets {
		for _, service := range []string{
			deviceCredentialPendingServiceForProfile(target.name),
			deviceCredentialServiceForProfile(target.name),
		} {
			if _, err := secretGet(service); err != nil &&
				err != errSecretNotFound &&
				secureStorageSessionProblem(err) {
				return err
			}
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
		return false
	}
	rerun := "ha-nova uninstall --purge"
	if yes {
		rerun = "ha-nova uninstall --yes --purge"
	}
	printHumanErr("%s, so a full purge can't check this computer's saved HA NOVA credential. Nothing changed.", secureStorageSessionReason)
	printHumanWarn("Next step: %s, then run: %s", secureStorageSessionNextStep(), rerun)
	printHumanWarn("Details: %s", err)
	return true
}

// purgeReadyForWindowsHandoff reports the helper's own checks here first. A
// guided teardown that just ran keeps the old path: its evidence must reach
// the recovery status even if the helper then fails.
func purgeReadyForWindowsHandoff(paths runtimePaths, mode uninstallMode, teardown teardownOutcome) bool {
	if mode != uninstallModePurge || teardown == teardownCompleted {
		return true
	}
	if err := validatePurgeBeforeWindowsHandoff(paths); err != nil {
		printHumanErr("%s", err)
		return false
	}
	return true
}

// validatePurgeBeforeWindowsHandoff runs the purge's read-only device
// credential checks in this terminal. The detached Windows helper runs them
// again, but its failure would only show later in doctor.
func validatePurgeBeforeWindowsHandoff(paths runtimePaths) error {
	targets, release, err := pinnedPurgeDeviceTargets(paths)
	if err != nil {
		return err
	}
	defer release()
	if err := validateProfilePurgeTargets(targets); err != nil {
		return fmt.Errorf("failed to validate device credential cleanup: %w", err)
	}
	return nil
}
