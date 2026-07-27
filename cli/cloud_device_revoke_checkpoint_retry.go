package main

import (
	"context"
	"errors"
	"fmt"
)

func finishCheckpointedCloudDeviceRevocation(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
	remoteOnly bool,
	report *uninstallReport,
) (bool, error) {
	if err := validateCloudDeviceRevocationCheckpoint(*cfg.Cloud); err != nil {
		return false, newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud device revocation checkpoint",
			err,
		)
	}
	checkpoint := *cfg.Cloud.DeviceRevocationCompleted
	if err := validateCheckpointedCloudDeviceCredential(
		ctx,
		deviceCredentialServiceForProfile(profileName),
		checkpoint.CurrentDeviceID,
	); err != nil {
		return false, err
	}
	if err := validateCheckpointedPendingCloudDeviceCredential(
		ctx,
		cfg,
		profileName,
		checkpoint.PendingDeviceID,
	); err != nil {
		return false, err
	}
	currentRemoved, err := deleteCheckpointedCloudDeviceCredentials(
		ctx,
		profileName,
		checkpoint,
	)
	if err != nil {
		return false, err
	}
	reportCloudDeviceRevocation(
		report,
		profileName,
		remoteOnly,
		false,
	)
	return currentRemoved, nil
}

func validateCheckpointedCloudDeviceCredential(
	ctx context.Context,
	service string,
	expectedID string,
) error {
	if expectedID == "" {
		return nil
	}
	value, exists, err := readCredentialSlotWithPolicy(
		ctx,
		service,
		SecretStoreForbidUI,
	)
	if err != nil || !exists {
		return err
	}
	if deviceIDOf(value) != expectedID {
		return newCloudError(
			CloudErrIdentityMismatch,
			"validate checkpointed Cloud device credential",
			nil,
		)
	}
	return nil
}

func validateCheckpointedPendingCloudDeviceCredential(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
	expectedID string,
) error {
	if expectedID == "" {
		return nil
	}
	pending, exists, err := selectPendingRemoteCloudDeviceCredential(
		ctx,
		cfg,
		profileName,
	)
	if err != nil || !exists {
		return err
	}
	if deviceIDOf(pending.value) != expectedID {
		return newCloudError(
			CloudErrIdentityMismatch,
			"validate checkpointed pending Cloud device credential",
			nil,
		)
	}
	return nil
}

func deleteCheckpointedCloudDeviceCredentials(
	ctx context.Context,
	profileName string,
	checkpoint cloudDeviceRevocationCheckpoint,
) (bool, error) {
	services := make([]string, 0, 2)
	if checkpoint.CurrentDeviceID != "" {
		services = append(
			services,
			deviceCredentialServiceForProfile(profileName),
		)
	}
	if checkpoint.PendingDeviceID != "" {
		services = append(
			services,
			deviceCredentialPendingServiceForProfile(profileName),
		)
	}
	for _, service := range services {
		if err := secretDeleteWithPolicy(
			ctx,
			service,
			SecretStoreForbidUI,
		); err != nil {
			return false, fmt.Errorf(
				"remove revoked Cloud device credential for server %q: %w",
				profileName,
				err,
			)
		}
	}
	return checkpoint.CurrentDeviceID != "", nil
}

func cloudDeviceRevocationIdentityMismatch(
	profileName string,
	slot string,
) error {
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: fmt.Sprintf(
			"the %s Cloud device credential for server %q does not match its durable activation checkpoint; cleanup stopped before OAuth, secure-storage, or configuration deletion",
			slot,
			profileName,
		),
		Cause: newCloudError(
			CloudErrIdentityMismatch,
			"validate Cloud device credential for verified revocation",
			errors.New("device id mismatch"),
		),
	}
}
