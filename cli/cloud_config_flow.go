package main

import (
	"context"
	"errors"
)

type cloudConfigSaver func(runtimeConfig) error

var errCloudAlreadyConfigured = errors.New(
	"Home Assistant Cloud access is already configured",
)

func connectExistingDeviceToCloud(
	ctx context.Context,
	paths runtimePaths,
	cfg runtimeConfig,
	coordinator cloudSetupCoordinator,
	reconnect bool,
	save cloudConfigSaver,
	mutations ...*pausableClientMutationLock,
) (result runtimeConfig, resultErr error) {
	result = cfg
	if save == nil {
		save = func(value runtimeConfig) error {
			return saveConfig(paths, value)
		}
	}
	defer func() {
		resultErr = persistCloudRecoveryHoldForError(
			&result,
			resultErr,
			save,
		)
	}()
	if coordinator == nil || !coordinator.Available() {
		return cfg, cloudAdapterUnavailableProblem()
	}
	if err := validateCloudConnectIntent(cfg, reconnect); err != nil {
		return cfg, err
	}
	if err := requireSettledDeviceCredentialRetirement(
		paths,
		activeServerProfile(),
	); err != nil {
		return cfg, err
	}
	if err := preflightCloudDeviceAccess(
		ctx,
		cfg.RelayInstanceID,
		false,
		secretStoreUIPolicyForSetup(SecretStoreAllowUI),
	); err != nil {
		return cfg, err
	}
	if err := ensureProfileIdentityForSetup(paths, &cfg); err != nil {
		return cfg, err
	}
	if err := save(cfg); err != nil {
		return cfg, err
	}
	var err error
	ctx, err = preflightCloudSecretAccessSession(
		ctx,
		coordinator,
		cfg,
		cloudSecretPreflightSetup,
	)
	if err != nil {
		return cfg, err
	}
	if cfg.Cloud != nil && cfg.Cloud.State == cloudStateRollingBack {
		if err := rollbackCloudReconnectAfterUserConflict(ctx, &cfg, save); err != nil {
			return cfg, cloudAccountSwitchRollbackFailedProblem(err)
		}
		return cfg, cloudAccountSwitchRolledBackProblem()
	}
	if cfg.Cloud != nil &&
		(cfg.Cloud.State == cloudStateCommitted ||
			cfg.Cloud.State == cloudStateRetiringPrevious) {
		resumed, err := resumeCommittedCloudSetup(
			ctx,
			coordinator,
			paths,
			&cfg,
			save,
		)
		if err != nil {
			return cfg, err
		}
		if resumed {
			return cfg, nil
		}
	}
	if err := retirePreviousCloudAuthorization(
		ctx,
		coordinator,
		cfg.ProfileID,
	); err != nil {
		return cfg, err
	}
	switch {
	case reconnect && !cfg.Cloud.configured():
		return cfg, cloudNotConfiguredProblem()
	case reconnect && cfg.Cloud.ready():
		cfg.Cloud.State = cloudStateAuthorizing
		cfg.Cloud.Pending = nil
		if err := save(cfg); err != nil {
			return cfg, err
		}
	case !reconnect && cfg.Cloud.configured():
		return cfg, errCloudAlreadyConfigured
	case cfg.Cloud == nil:
		cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing}
		if err := save(cfg); err != nil {
			return cfg, err
		}
	}
	request := newCloudSetupRequest(&cfg, save, mutations...)
	setupResult, err := coordinator.AddAwayWithExistingDevice(ctx, request)
	if err != nil {
		if reconnect && IsCloudErrorCode(err, CloudErrDeviceUserConflict) {
			if rollbackErr := rollbackCloudReconnectAfterUserConflict(
				ctx,
				&cfg,
				save,
			); rollbackErr != nil {
				return cfg, cloudAccountSwitchRollbackFailedProblem(
					errors.Join(err, rollbackErr),
				)
			}
			return cfg, cloudAccountSwitchRolledBackProblem()
		}
		return cfg, err
	}
	return commitCloudConnection(ctx, cfg, setupResult, coordinator, save)
}

func connectRemoteToCloud(
	ctx context.Context,
	paths runtimePaths,
	cfg runtimeConfig,
	coordinator cloudRemoteSetupCoordinator,
	origin CloudOrigin,
	pairingCode cloudRemotePairingCodeProvider,
	reconnect bool,
	save cloudConfigSaver,
	mutations ...*pausableClientMutationLock,
) (result runtimeConfig, resultErr error) {
	result = cfg
	if save == nil {
		save = func(value runtimeConfig) error {
			return saveConfig(paths, value)
		}
	}
	defer func() {
		resultErr = persistCloudRecoveryHoldForError(
			&result,
			resultErr,
			save,
		)
	}()
	if coordinator == nil || !coordinator.Available() {
		return cfg, cloudAdapterUnavailableProblem()
	}
	if err := validateCloudConnectIntent(cfg, reconnect); err != nil {
		return cfg, err
	}
	if err := requireSettledDeviceCredentialRetirement(
		paths,
		activeServerProfile(),
	); err != nil {
		return cfg, err
	}
	// Prove any current credential before the writable canary, profile/install
	// creation, OAuth, or Cloud lifecycle checkpoints. A Cloud-only setup
	// cannot safely replace a credential whose Relay identity is unknown.
	expectedRelayInstanceID, err := expectedRemoteCloudRelayIdentity(
		ctx,
		cloudRemoteSetupRequest{
			cloudSetupRequest: cloudSetupRequest{Config: cfg},
			Origin:            origin,
		},
	)
	if err != nil {
		return cfg, err
	}
	if err := inspectCloudDeviceAccess(
		ctx,
		expectedRelayInstanceID,
		true,
		true,
		secretStoreUIPolicyForSetup(SecretStoreAllowUI),
	); err != nil {
		return cfg, err
	}
	if _, err := probeCloudDeviceStorageForSetup(
		ctx,
		secretStoreUIPolicyForSetup(SecretStoreAllowUI),
	); err != nil {
		return cfg, err
	}
	if cfg.RelayInstanceID == "" {
		cfg.RelayInstanceID = expectedRelayInstanceID
	}
	if err := ensureProfileIdentityForSetup(paths, &cfg); err != nil {
		return cfg, err
	}
	if _, err := getOrCreateClientInstallID(
		&cfg,
		func(value *runtimeConfig) error { return save(*value) },
	); err != nil {
		return cfg, err
	}
	// Persist the selected profile identity even when this install already had
	// a client_install_id. If OAuth secure-storage preflight needs recovery,
	// cloud unlock must be able to reopen the exact same profile-scoped slots.
	if err := save(cfg); err != nil {
		return cfg, err
	}
	ctx, err = preflightCloudSecretAccessSession(
		ctx,
		coordinator,
		cfg,
		cloudSecretPreflightSetup,
	)
	if err != nil {
		return cfg, err
	}
	if cfg.Cloud != nil && cfg.Cloud.State == cloudStateRollingBack {
		if err := rollbackCloudReconnectAfterUserConflict(ctx, &cfg, save); err != nil {
			return cfg, cloudAccountSwitchRollbackFailedProblem(err)
		}
		return cfg, cloudAccountSwitchRolledBackProblem()
	}
	if cfg.Cloud != nil &&
		(cfg.Cloud.State == cloudStateCommitted ||
			cfg.Cloud.State == cloudStateRetiringPrevious) {
		resumed, err := resumeCommittedCloudSetup(
			ctx,
			coordinator,
			paths,
			&cfg,
			save,
		)
		if err != nil {
			return cfg, err
		}
		if resumed {
			return cfg, nil
		}
	}
	if err := retirePreviousCloudAuthorization(
		ctx,
		coordinator,
		cfg.ProfileID,
	); err != nil {
		return cfg, err
	}
	if reconnect && !cfg.Cloud.configured() {
		return cfg, cloudNotConfiguredProblem()
	}
	if reconnect && cfg.Cloud.ready() {
		cfg.Cloud.State = cloudStateAuthorizing
		cfg.Cloud.Pending = nil
		if err := save(cfg); err != nil {
			return cfg, err
		}
	} else if !reconnect && cfg.Cloud.configured() {
		return cfg, errCloudAlreadyConfigured
	}
	if cfg.Cloud == nil {
		cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing}
		if err := save(cfg); err != nil {
			return cfg, err
		}
	}
	request := cloudRemoteSetupRequest{
		cloudSetupRequest: newCloudSetupRequest(&cfg, save, mutations...),
		Origin:            origin,
		PairingCode:       pairingCode,
	}
	setupResult, err := coordinator.AddRemoteWithPairing(ctx, request)
	if err != nil {
		if reconnect && IsCloudErrorCode(err, CloudErrDeviceUserConflict) {
			if rollbackErr := rollbackCloudReconnectAfterUserConflict(
				ctx,
				&cfg,
				save,
			); rollbackErr != nil {
				return cfg, cloudAccountSwitchRollbackFailedProblem(
					errors.Join(err, rollbackErr),
				)
			}
			return cfg, cloudAccountSwitchRolledBackProblem()
		}
		return cfg, err
	}
	return commitCloudConnection(ctx, cfg, setupResult, coordinator, save)
}

func validateCloudConnectIntent(
	cfg runtimeConfig,
	reconnect bool,
) error {
	if err := rejectCloudSetupDuringDeviceRevocation(cfg); err != nil {
		return err
	}
	if problem := cloudRecoveryHoldProblem(cfg); problem != nil {
		return problem
	}
	if reconnect {
		if !cfg.Cloud.configured() {
			return cloudNotConfiguredProblem()
		}
		return nil
	}
	if cfg.Cloud.ready() {
		return errCloudAlreadyConfigured
	}
	if cfg.Cloud.configured() &&
		cfg.Cloud.State != cloudStateCommitted &&
		cfg.Cloud.State != cloudStateRetiringPrevious {
		return errCloudAlreadyConfigured
	}
	return nil
}
