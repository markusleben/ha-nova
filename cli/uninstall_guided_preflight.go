package main

import (
	"context"
	"fmt"
	"time"
)

// prepareUninstallBeforeGuidedTeardown resolves every deterministic blocker
// before the walkthrough can ask the user to delete anything in Home Assistant.
// Pending device retirements are completed while their Relay still exists;
// negative reachability after App removal is never retirement-revocation proof.
func prepareUninstallBeforeGuidedTeardown(
	paths runtimePaths,
	mode uninstallMode,
) error {
	return withClientMutationLock(paths, func() (resultErr error) {
		if mode == uninstallModePurge {
			defer func() {
				resultErr = resetPurgeStorageProofAfterError(
					paths,
					resultErr,
				)
			}()
		}
		profiles, err := deviceCredentialRetirementCheckpointProfiles(paths)
		if err != nil {
			return fmt.Errorf(
				"cannot validate uninstall before Home Assistant removal: %w",
				err,
			)
		}
		if mode != uninstallModePurge && len(profiles) > 0 {
			return fmt.Errorf(
				"device credential retirement is pending for server %q; run `%s` to finish it before uninstalling, or choose Full purge",
				profiles[0],
				deviceRetirementSetupCommand(profiles[0]),
			)
		}
		if mode != uninstallModePurge {
			return nil
		}
		if _, err := resumeKeyringDeviceCredentialCleanup(); err != nil {
			return relayAuthTokenSetupOperationError(
				"finish device credential storage recovery before Home Assistant removal",
				err,
			)
		}
		cloudTargets, err := collectCloudPurgeTargets(paths.ConfigFile)
		if err != nil {
			return fmt.Errorf(
				"cannot validate Home Assistant Cloud cleanup before Home Assistant removal: %w",
				err,
			)
		}
		deviceTargets, err := collectProfilePurgeTargets(paths)
		if err != nil {
			return fmt.Errorf(
				"cannot validate device cleanup before Home Assistant removal: %w",
				err,
			)
		}
		if err := validateUninstallSecureStorageBeforeGuidedTeardown(
			paths,
			cloudTargets,
			deviceTargets,
		); err != nil {
			return err
		}
		targets, err := collectDeviceCredentialRetirementPurgeTargets(paths)
		if err != nil {
			return fmt.Errorf(
				"cannot validate pending device retirement before Home Assistant removal: %w",
				err,
			)
		}
		err = executeDeviceCredentialRetirementPurgeTargets(
			paths,
			nil,
			targets,
		)
		if err != nil {
			return fmt.Errorf(
				"cannot finish pending device retirement before Home Assistant removal: %w",
				err,
			)
		}
		if err := prepareCloudDeviceRevocationsBeforeGuidedTeardown(
			paths,
			cloudTargets,
		); err != nil {
			return fmt.Errorf(
				"cannot finish Cloud device cleanup before Home Assistant removal: %w",
				err,
			)
		}
		return nil
	})
}

func validateUninstallSecureStorageBeforeGuidedTeardown(
	paths runtimePaths,
	cloudTargets []cloudPurgeTarget,
	deviceTargets []profilePurgeTarget,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, target := range cloudTargets {
		store, err := newCloudSecretStoreForCLI(target.profileID)
		if err != nil {
			return fmt.Errorf(
				"cannot inspect Home Assistant Cloud credentials for server %q before Home Assistant removal: %w",
				target.profileName,
				cloudProblemForError(err),
			)
		}
		if _, err := inspectCloudAuthorizationCleanup(
			ctx,
			target.config,
			store,
		); err != nil {
			return fmt.Errorf(
				"cannot inspect Home Assistant Cloud credentials for server %q before Home Assistant removal: %w",
				target.profileName,
				cloudProblemForError(
					cloudAuthorizationCleanupErrorWithRecoveryCommand(
						err,
						target.profileName,
					),
				),
			)
		}
		if err := validateCloudDeviceRevocationPlan(
			ctx,
			target.config,
			target.profileName,
		); err != nil {
			return fmt.Errorf(
				"cannot validate Home Assistant Cloud device cleanup for server %q before Home Assistant removal: %w",
				target.profileName,
				cloudProblemForError(err),
			)
		}
	}

	if err := validateProfilePurgeTargets(deviceTargets); err != nil {
		return err
	}
	if _, err := readRelayAuthToken(); err != nil &&
		!isMissingRelayAuthTokenError(err) {
		return fmt.Errorf(
			"cannot inspect legacy relay authorization before Home Assistant removal: %s",
			relayAuthTokenProblemMessage(err),
		)
	}
	return nil
}
