package main

import (
	"context"
	"errors"
	"fmt"
)

func newCloudSetupRequest(
	cfg *runtimeConfig,
	save cloudConfigSaver,
) cloudSetupRequest {
	guardDeviceRevocation := func() error {
		if cfg == nil {
			return errors.New("Cloud setup configuration is missing")
		}
		return rejectCloudSetupDuringDeviceRevocation(*cfg)
	}
	return cloudSetupRequest{
		ProfileName: activeServerProfile(),
		Config:      *cfg,
		PersistPendingMetadata: func(metadata cloudConnectionMetadata) error {
			if err := guardDeviceRevocation(); err != nil {
				return err
			}
			if err := validateCloudConnectionMetadata(metadata); err != nil {
				return fmt.Errorf("invalid pending Cloud metadata: %w", err)
			}
			if cfg.Cloud == nil {
				return errors.New("Cloud lifecycle is missing")
			}
			if cfg.Cloud.Pending != nil &&
				cfg.Cloud.Pending.CredentialGeneration != metadata.CredentialGeneration {
				return errors.New("a different pending Cloud credential already exists")
			}
			if cfg.Cloud.Pending == nil &&
				cfg.Cloud.State != cloudStateAuthorizing {
				return fmt.Errorf(
					"cannot add pending Cloud metadata in lifecycle state %q",
					cfg.Cloud.State,
				)
			}
			cfg.Cloud.Pending = &metadata
			return save(*cfg)
		},
		ClearPendingAuthorization: func(generation string) error {
			if err := guardDeviceRevocation(); err != nil {
				return err
			}
			if cfg.Cloud == nil || cfg.Cloud.Pending == nil {
				return errors.New(
					"cannot clear missing pending Cloud authorization metadata",
				)
			}
			if cfg.Cloud.State != cloudStateAuthorizing {
				return fmt.Errorf(
					"cannot clear pending Cloud authorization in lifecycle state %q",
					cfg.Cloud.State,
				)
			}
			if generation == "" ||
				cfg.Cloud.Pending.CredentialGeneration != generation {
				return newCloudError(
					CloudErrSecretConflict,
					"clear pending Cloud authorization metadata",
					nil,
				)
			}
			pending := cfg.Cloud.Pending
			cfg.Cloud.Pending = nil
			if err := save(*cfg); err != nil {
				cfg.Cloud.Pending = pending
				return err
			}
			return nil
		},
		AdvancePendingLifecycle: func(next cloudLifecycleState) error {
			if err := guardDeviceRevocation(); err != nil {
				return err
			}
			if cfg.Cloud == nil || cfg.Cloud.Pending == nil {
				return errors.New(
					"cannot advance Cloud lifecycle before pending credentials are stored",
				)
			}
			currentRank, currentOK := pendingCloudLifecycleRank(cfg.Cloud.State)
			nextRank, nextOK := pendingCloudLifecycleRank(next)
			if !currentOK || !nextOK || nextRank > currentRank+1 {
				return fmt.Errorf(
					"invalid Cloud lifecycle transition %q -> %q",
					cfg.Cloud.State,
					next,
				)
			}
			if nextRank <= currentRank {
				return nil
			}
			cfg.Cloud.State = next
			return save(*cfg)
		},
		CheckpointDeviceActivation: func(deviceID string) error {
			if err := guardDeviceRevocation(); err != nil {
				return err
			}
			if !validDeviceID(deviceID) {
				return newCloudError(
					CloudErrIdentityMismatch,
					"checkpoint Cloud device activation",
					nil,
				)
			}
			if cfg.Cloud == nil {
				return errors.New(
					"cannot checkpoint Cloud device activation without a lifecycle",
				)
			}
			if cfg.Cloud.Pending == nil ||
				(cfg.Cloud.State != cloudStateCloudVerified &&
					cfg.Cloud.State != cloudStateDeviceBoundOrPaired) {
				return fmt.Errorf(
					"cannot checkpoint Cloud device activation in lifecycle state %q",
					cfg.Cloud.State,
				)
			}
			if cfg.Cloud.DeviceActivationStarted {
				if cfg.Cloud.DeviceActivationDeviceID == deviceID {
					return nil
				}
				return newCloudError(
					CloudErrIdentityMismatch,
					"checkpoint Cloud device activation",
					nil,
				)
			}
			cfg.Cloud.DeviceActivationStarted = true
			cfg.Cloud.DeviceActivationDeviceID = deviceID
			if err := save(*cfg); err != nil {
				cfg.Cloud.DeviceActivationStarted = false
				cfg.Cloud.DeviceActivationDeviceID = ""
				return err
			}
			return nil
		},
		ClearDeviceActivation: func() error {
			if err := guardDeviceRevocation(); err != nil {
				return err
			}
			if cfg.Cloud == nil {
				return errors.New(
					"cannot clear Cloud device activation without a lifecycle",
				)
			}
			if cfg.Cloud.Pending == nil ||
				(cfg.Cloud.State != cloudStateCloudVerified &&
					cfg.Cloud.State != cloudStateDeviceBoundOrPaired) {
				return fmt.Errorf(
					"cannot clear Cloud device activation in lifecycle state %q",
					cfg.Cloud.State,
				)
			}
			if !cfg.Cloud.DeviceActivationStarted {
				if cfg.Cloud.DeviceActivationDeviceID != "" {
					return errors.New(
						"cannot clear an inconsistent Cloud device activation checkpoint",
					)
				}
				return nil
			}
			deviceID := cfg.Cloud.DeviceActivationDeviceID
			cfg.Cloud.DeviceActivationStarted = false
			cfg.Cloud.DeviceActivationDeviceID = ""
			if err := save(*cfg); err != nil {
				cfg.Cloud.DeviceActivationStarted = true
				cfg.Cloud.DeviceActivationDeviceID = deviceID
				return err
			}
			return nil
		},
		CheckpointDeviceBinding: func(relayInstanceID string) error {
			if err := guardDeviceRevocation(); err != nil {
				return err
			}
			if !validIdentifier(relayInstanceID, 256) {
				return newCloudError(
					CloudErrRelayInstance,
					"checkpoint Cloud device binding",
					nil,
				)
			}
			if cfg.Cloud == nil || cfg.Cloud.Pending == nil {
				return errors.New(
					"cannot checkpoint a Cloud device before pending credentials are stored",
				)
			}
			if cfg.RelayInstanceID != "" &&
				cfg.RelayInstanceID != relayInstanceID {
				return newCloudError(
					CloudErrRelayInstance,
					"checkpoint Cloud device binding",
					nil,
				)
			}
			currentRank, currentOK := pendingCloudLifecycleRank(cfg.Cloud.State)
			nextRank, _ := pendingCloudLifecycleRank(
				cloudStateDeviceBoundOrPaired,
			)
			if !currentOK || currentRank < nextRank-1 ||
				currentRank > nextRank {
				return fmt.Errorf(
					"invalid Cloud device checkpoint in lifecycle state %q",
					cfg.Cloud.State,
				)
			}
			cfg.RelayInstanceID = relayInstanceID
			cfg.Cloud.State = cloudStateDeviceBoundOrPaired
			return save(*cfg)
		},
	}
}

func commitCloudConnection(
	ctx context.Context,
	cfg runtimeConfig,
	result cloudSetupResult,
	coordinator cloudSetupCoordinator,
	save cloudConfigSaver,
) (runtimeConfig, error) {
	if err := validateCloudSetupResult(cfg, result); err != nil {
		return cfg, err
	}

	cfg.RelayInstanceID = result.RelayInstanceID
	cfg.Cloud.Current = &result.Current
	cfg.Cloud.DeviceActivationStarted = false
	cfg.Cloud.DeviceActivationDeviceID = ""
	cfg.Cloud.State = cloudStateCommitted
	if err := save(cfg); err != nil {
		return cfg, err
	}
	cfg.Cloud.Pending = nil
	cfg.Cloud.State = cloudStateRetiringPrevious
	if err := save(cfg); err != nil {
		return cfg, err
	}
	if err := retirePreviousCloudAuthorization(
		ctx,
		coordinator,
		cfg.ProfileID,
	); err != nil {
		return cfg, err
	}
	cfg.Cloud.State = cloudStateReady
	if cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
		cfg.RoutePolicy = routePolicyAutomatic
	} else {
		cfg.RoutePolicy = routePolicyCloud
	}
	if err := save(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
