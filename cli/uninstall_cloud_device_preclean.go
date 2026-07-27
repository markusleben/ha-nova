package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// prepareCloudDeviceRevocationsBeforeGuidedTeardown finishes every Cloud
// device revoke while its Relay is still known to exist. The later manual App
// deletion can then never turn a negative reachability probe into revocation
// evidence. OAuth authorizations stay intact until the final purge so the
// owner can still complete the Home Assistant walkthrough.
func prepareCloudDeviceRevocationsBeforeGuidedTeardown(
	paths runtimePaths,
	targets []cloudPurgeTarget,
) error {
	if len(targets) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for i := range targets {
		target := &targets[i]
		store, err := newCloudSecretStoreForCLI(target.profileID)
		if err != nil {
			return cloudPurgeFailure(paths, *target, fmt.Errorf(
				"open Cloud credentials for server %q: %w",
				target.profileName,
				err,
			))
		}
		if _, err := inspectCloudAuthorizationCleanup(
			ctx,
			target.config,
			store,
		); err != nil {
			return cloudPurgeFailure(paths, *target, fmt.Errorf(
				"inspect Cloud credentials for server %q: %w",
				target.profileName,
				cloudAuthorizationCleanupErrorWithRecoveryCommand(
					err,
					target.profileName,
				),
			))
		}
		if err := revokeCloudDeviceForUninstall(
			ctx,
			paths,
			target.config,
			target.profileName,
			store,
			nil,
			target,
		); err != nil {
			return cloudPurgeFailure(paths, *target, fmt.Errorf(
				"revoke Cloud device for server %q before Home Assistant removal: %w",
				target.profileName,
				err,
			))
		}
	}
	return nil
}

func revokeCloudDeviceForUninstall(
	ctx context.Context,
	paths runtimePaths,
	cfg runtimeConfig,
	profileName string,
	store OAuthSecretStore,
	report *uninstallReport,
	target *cloudPurgeTarget,
) error {
	_, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
		ctx,
		cfg,
		profileName,
		store,
		report,
		func(
			checkpoint cloudDeviceRevocationCheckpoint,
		) error {
			checkpointed, expected, err :=
				checkpointCloudDeviceRevocationUnlocked(
					paths,
					target.recovery,
					checkpoint,
				)
			if err != nil {
				return err
			}
			target.config = checkpointed
			target.recovery = expected
			return nil
		},
	)
	return err
}

// Uninstall calls this while holding the client mutation lock. Persisting the
// per-profile hold here ensures a multi-profile purge cannot return with an
// ambiguous authorization outcome represented only in process memory.
func cloudPurgeFailure(
	paths runtimePaths,
	target cloudPurgeTarget,
	cause error,
) error {
	_, err := checkpointCloudRecoveryHoldUnlocked(
		paths,
		target.recovery,
		cause,
	)
	if err != nil {
		return errors.Join(
			cause,
			fmt.Errorf("persist Cloud recovery safety hold: %w", err),
		)
	}
	return cause
}
