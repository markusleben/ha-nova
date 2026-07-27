package main

import (
	"context"
	"errors"
)

func rollbackCloudReconnectAfterUserConflict(
	ctx context.Context,
	cfg *runtimeConfig,
	save cloudConfigSaver,
) error {
	if cfg == nil || cfg.Cloud == nil ||
		cfg.Cloud.Current == nil || cfg.Cloud.Pending == nil {
		return errors.New("Cloud reconnect rollback has no current and pending authorization")
	}
	if err := rejectCloudSetupDuringDeviceRevocation(*cfg); err != nil {
		return err
	}
	if cfg.Cloud.State != cloudStateRollingBack {
		cfg.Cloud.State = cloudStateRollingBack
		if err := save(*cfg); err != nil {
			return err
		}
	}
	store, exists := cloudSecretStoreForOperation(ctx, cfg.ProfileID)
	if !exists {
		var err error
		store, err = newCloudSecretStoreForCLI(cfg.ProfileID)
		if err != nil {
			return err
		}
	}
	pending, exists, err := store.LoadPending(ctx, SecretStoreForbidUI)
	if err != nil {
		return err
	}
	if exists {
		if err := validatePendingCloudPreflight(*cfg, pending); err != nil {
			return err
		}
		if err := revokeAndVerifyCloudAuthorizationForCLI(ctx, pending); err != nil {
			return err
		}
		exact, err := exactOAuthCleanupStoreFor(store)
		if err != nil {
			return err
		}
		if err := exact.DeletePendingExact(
			ctx,
			pending,
			SecretStoreForbidUI,
		); err != nil {
			return err
		}
	}
	pendingMetadata := cfg.Cloud.Pending
	activationStarted := cfg.Cloud.DeviceActivationStarted
	activationDeviceID := cfg.Cloud.DeviceActivationDeviceID
	cfg.Cloud.Pending = nil
	cfg.Cloud.DeviceActivationStarted = false
	cfg.Cloud.DeviceActivationDeviceID = ""
	cfg.Cloud.State = cloudStateReady
	if err := save(*cfg); err != nil {
		cfg.Cloud.Pending = pendingMetadata
		cfg.Cloud.DeviceActivationStarted = activationStarted
		cfg.Cloud.DeviceActivationDeviceID = activationDeviceID
		cfg.Cloud.State = cloudStateRollingBack
		return err
	}
	return nil
}

func cloudAccountSwitchRolledBackProblem() *cloudProblem {
	return &cloudProblem{
		Code:        cloudProblemIdentityMismatch,
		Remediation: cloudRemediationPair,
		Detail: "the new authorization belongs to a different Home Assistant user; " +
			"the previous Cloud connection was restored. Pair this computer again " +
			"for the intended user, then reconnect Cloud access",
	}
}

func cloudAccountSwitchRollbackFailedProblem(err error) *cloudProblem {
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: "the different-user reconnect was stopped, but its saved rollback " +
			"checkpoint could not be completed; rerun Cloud reconnect from an " +
			"interactive desktop before changing this profile",
		Cause: err,
	}
}
