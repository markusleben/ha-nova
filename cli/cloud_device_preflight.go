package main

import (
	"context"
)

var (
	probeCloudDeviceStorageForSetup = probeDeviceCredentialStorageWithPolicy
	readCloudPendingDeviceForSetup  = readPendingDeviceCredentialRecordWithPolicy
	readCloudDeviceForSetup         = readDeviceCredentialWithPolicy
)

// preflightCloudDeviceAccess unlocks only the selected existing device slots.
// It is read-only: Cloud unlock and existing-device binding must not create a
// canary item merely to authorize credentials they will never replace.
func preflightCloudDeviceAccess(
	ctx context.Context,
	expectedRelayInstanceID string,
	allowCloudPending bool,
	ui SecretStoreUIPolicy,
) error {
	return inspectCloudDeviceAccess(
		ctx,
		expectedRelayInstanceID,
		allowCloudPending,
		false,
		ui,
	)
}

// preflightWritableCloudDeviceAccess additionally proves that a new remote
// pairing credential can be stored. Only remote setup/reconnect uses it before
// OAuth; the pairing path repeats the no-UI proof immediately before consuming
// the owner's one-time code.
func preflightWritableCloudDeviceAccess(
	ctx context.Context,
	expectedRelayInstanceID string,
	allowCloudPending bool,
	ui SecretStoreUIPolicy,
) error {
	if _, err := probeCloudDeviceStorageForSetup(ctx, ui); err != nil {
		return err
	}
	return inspectCloudDeviceAccess(
		ctx,
		expectedRelayInstanceID,
		allowCloudPending,
		false,
		ui,
	)
}

func inspectCloudDeviceAccess(
	ctx context.Context,
	expectedRelayInstanceID string,
	allowCloudPending bool,
	requireCurrentRelayProof bool,
	ui SecretStoreUIPolicy,
) error {
	pending, exists, err := readCloudPendingDeviceForSetup(ctx, ui)
	if err != nil {
		return err
	}
	if exists {
		if !allowCloudPending ||
			pending.Source != pendingDeviceCredentialSourceCloud {
			return newCloudError(
				CloudErrDevicePendingConflict,
				"prepare Cloud device",
				nil,
			)
		}
		if expectedRelayInstanceID != "" &&
			pending.RelayInstanceID != expectedRelayInstanceID {
			return newCloudError(
				CloudErrRelayInstance,
				"match pending Cloud device before authorization",
				nil,
			)
		}
	}
	_, currentExists, err := readCloudDeviceForSetup(ctx, ui)
	if err != nil {
		return err
	}
	if currentExists &&
		requireCurrentRelayProof &&
		expectedRelayInstanceID == "" {
		return &cloudProblem{
			Code:        cloudProblemIdentityMismatch,
			Remediation: cloudRemediationSecurityStop,
			Detail: "an existing device credential cannot be matched to this Relay; " +
				"setup stopped before sign-in or pairing to avoid replacing it. " +
				"Restore local access with `ha-nova setup` and retry, or revoke the old " +
				"NOVA device in Home Assistant and run `ha-nova uninstall --purge` " +
				"before a fresh Cloud-only setup",
			Cause: newCloudError(
				CloudErrRelayInstance,
				"prove existing device credential before remote Cloud setup",
				nil,
			),
		}
	}
	return nil
}
