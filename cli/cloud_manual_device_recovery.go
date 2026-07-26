package main

import (
	"context"
	"errors"
)

// planManuallyConfirmedCloudDeviceRevocation converts only an exact missing-slot
// stop into durable progress. Corrupt, replaced, untracked, or identity-mismatched
// values remain hard failures even after an Owner confirmation.
func planManuallyConfirmedCloudDeviceRevocation(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
	remoteOnly bool,
) (*cloudDeviceRevocationCheckpoint, error) {
	_, checkpoint, err := planCloudDeviceRevocation(
		ctx,
		cfg,
		profileName,
		remoteOnly,
	)
	if err == nil {
		if err := validateManualCloudDeviceSlots(
			ctx,
			profileName,
			remoteOnly,
			checkpoint,
		); err != nil {
			return nil, err
		}
		return checkpoint, nil
	}
	if !errors.Is(err, errCloudDeviceRevocationCredentialMissing) {
		return nil, err
	}
	checkpoint, err = manuallyConfirmedMissingCloudDeviceCheckpoint(
		ctx,
		cfg,
		profileName,
		remoteOnly,
	)
	if err != nil {
		return nil, err
	}
	if err := validateManualCloudDeviceSlots(
		ctx,
		profileName,
		remoteOnly,
		checkpoint,
	); err != nil {
		return nil, err
	}
	return checkpoint, nil
}

func manuallyConfirmedMissingCloudDeviceCheckpoint(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
	remoteOnly bool,
) (*cloudDeviceRevocationCheckpoint, error) {
	if cfg.Cloud == nil {
		return nil, newCloudError(
			CloudErrSecretCorrupt,
			"confirm missing Cloud device credential",
			nil,
		)
	}
	current, currentExists, err := readCredentialSlotWithPolicy(
		ctx,
		deviceCredentialServiceForProfile(profileName),
		SecretStoreForbidUI,
	)
	if err != nil {
		return nil, err
	}
	pending, pendingExists, err :=
		readPendingDeviceCredentialRecordWithPolicy(
			ctx,
			SecretStoreForbidUI,
		)
	if err != nil {
		return nil, err
	}
	if pendingExists &&
		pending.Source == pendingDeviceCredentialSourceCloud &&
		cfg.RelayInstanceID != "" &&
		pending.RelayInstanceID != cfg.RelayInstanceID {
		return nil, cloudDeviceRevocationIdentityMismatch(
			profileName,
			"pending",
		)
	}

	if !cloudLifecycleMayHaveActivatedPendingDevice(cfg.Cloud) {
		if currentExists || (pendingExists &&
			pending.Source == pendingDeviceCredentialSourceCloud) {
			return nil, unidentifiedManualCloudDeviceCredential(
				profileName,
				"untracked",
			)
		}
		return nil, nil
	}

	expectedID := cfg.Cloud.DeviceActivationDeviceID
	checkpoint := &cloudDeviceRevocationCheckpoint{}
	if pendingExists &&
		pending.Source == pendingDeviceCredentialSourceCloud {
		if deviceIDOf(pending.Credential) != expectedID {
			return nil, cloudDeviceRevocationIdentityMismatch(
				profileName,
				"pending",
			)
		}
		checkpoint.PendingDeviceID = expectedID
	}
	if currentExists && remoteOnly {
		currentID := deviceIDOf(current)
		switch {
		case currentID == expectedID:
			checkpoint.CurrentDeviceID = expectedID
		case cfg.Cloud.Current != nil:
			checkpoint.CurrentDeviceID = currentID
		default:
			return nil, unidentifiedManualCloudDeviceCredential(
				profileName,
				"current",
			)
		}
	}
	if pendingExists {
		return checkpoint, nil
	}
	if cfg.Cloud.State == cloudStateDeviceBoundOrPaired {
		if currentExists && deviceIDOf(current) != expectedID {
			return nil, cloudDeviceRevocationIdentityMismatch(
				profileName,
				"promoted current",
			)
		}
		checkpoint.CurrentDeviceID = expectedID
		return checkpoint, nil
	}
	checkpoint.PendingDeviceID = expectedID
	return checkpoint, nil
}
