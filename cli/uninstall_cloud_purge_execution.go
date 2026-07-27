package main

import (
	"context"
	"fmt"
)

type cloudPurgeExecution struct {
	target            *cloudPurgeTarget
	store             OAuthSecretStore
	authorizationPlan cloudAuthorizationCleanupPlan
}

// prepareCloudPurgeExecutions proves every profile's OAuth and device state
// before the first remote mutation. A malformed later profile therefore cannot
// create partial teardown of an earlier one.
func prepareCloudPurgeExecutions(
	ctx context.Context,
	paths runtimePaths,
	targets []cloudPurgeTarget,
) ([]cloudPurgeExecution, error) {
	executions := make([]cloudPurgeExecution, 0, len(targets))
	for i := range targets {
		target := &targets[i]
		store, err := newCloudSecretStoreForCLI(target.profileID)
		if err != nil {
			return nil, cloudPurgeFailure(paths, *target, fmt.Errorf(
				"open Cloud credentials for server %q: %w",
				target.profileName,
				err,
			))
		}
		plan, err := inspectCloudAuthorizationCleanup(
			ctx,
			target.config,
			store,
		)
		if err != nil {
			return nil, cloudPurgeFailure(paths, *target, fmt.Errorf(
				"inspect Cloud credentials for server %q: %w",
				target.profileName,
				cloudAuthorizationCleanupErrorWithRecoveryCommand(
					err,
					target.profileName,
				),
			))
		}
		if err := validateCloudDeviceRevocationPlan(
			ctx,
			target.config,
			target.profileName,
		); err != nil {
			return nil, cloudPurgeFailure(paths, *target, fmt.Errorf(
				"validate Cloud device for server %q: %w",
				target.profileName,
				cloudAuthorizationCleanupErrorWithRecoveryCommand(
					err,
					target.profileName,
				),
			))
		}
		executions = append(executions, cloudPurgeExecution{
			target:            target,
			store:             store,
			authorizationPlan: plan,
		})
	}
	return executions, nil
}

// executeCloudPurgePlans completes every remote device revoke, then every
// remote OAuth revoke, then durably checkpoints every profile before deleting
// any local OAuth proof. A later local failure can therefore resume without
// repeating remote revocation or mistaking an already deleted proof for drift.
func executeCloudPurgePlans(
	ctx context.Context,
	paths runtimePaths,
	executions []cloudPurgeExecution,
	report *uninstallReport,
) error {
	for i := range executions {
		execution := &executions[i]
		target := execution.target
		if err := revokeCloudDeviceForUninstall(
			ctx,
			paths,
			target.config,
			target.profileName,
			execution.store,
			report,
			target,
		); err != nil {
			return cloudPurgeFailure(paths, *target, fmt.Errorf(
				"revoke Cloud device for server %q: %w",
				target.profileName,
				err,
			))
		}
	}
	for i := range executions {
		execution := &executions[i]
		if err := revokeCloudAuthorizationCleanupPlan(
			ctx,
			execution.authorizationPlan,
		); err != nil {
			return cloudPurgeFailure(
				paths,
				*execution.target,
				fmt.Errorf(
					"revoke Cloud authorization for server %q: %w",
					execution.target.profileName,
					err,
				),
			)
		}
	}
	for i := range executions {
		execution := &executions[i]
		target := execution.target
		checkpointed, expected, err :=
			checkpointCloudAuthorizationRevocationUnlocked(
				paths,
				target.recovery,
				execution.authorizationPlan,
				false,
			)
		if err != nil {
			return cloudPurgeFailure(
				paths,
				*target,
				fmt.Errorf(
					"checkpoint Cloud authorization revocation for server %q: %w",
					target.profileName,
					err,
				),
			)
		}
		target.config = checkpointed
		target.recovery = expected
	}
	for i := range executions {
		execution := &executions[i]
		if err := deleteRevokedCloudAuthorizationPlan(
			ctx,
			execution.store,
			execution.authorizationPlan,
		); err != nil {
			return cloudPurgeFailure(
				paths,
				*execution.target,
				fmt.Errorf(
					"remove Cloud credentials for server %q: %w",
					execution.target.profileName,
					err,
				),
			)
		}
		if execution.authorizationPlan.hasAuthorization() &&
			report != nil {
			report.addRemoved(fmt.Sprintf(
				"Home Assistant Cloud authorization (server %q)",
				execution.target.profileName,
			))
		}
	}
	return nil
}
