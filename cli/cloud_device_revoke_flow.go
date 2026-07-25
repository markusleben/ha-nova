package main

import (
	"context"
	"fmt"
)

type cloudDeviceRevocationCheckpointer func(
	cloudDeviceRevocationCheckpoint,
) error

// revokeRemoteOnlyCloudDeviceBeforeOAuth is the destructive-ordering gate for
// Cloud device credentials. Cloud-only profiles revoke their selected active
// device. Local-capable profiles retain their current local pairing, except
// when a Cloud-proven pending credential may already have been activated: that
// exact pending bearer must be revoked before either its slot or OAuth proof is
// removed.
func revokeRemoteOnlyCloudDeviceBeforeOAuth(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
	store OAuthSecretStore,
	report *uninstallReport,
	checkpointRevocation cloudDeviceRevocationCheckpointer,
) (bool, error) {
	if checkpointRevocation == nil {
		return false, newCloudError(
			CloudErrInvalidInput,
			"checkpoint Cloud device revocation",
			nil,
		)
	}
	remoteOnly, err := isRemoteOnlyCloudProfile(cfg)
	if err != nil {
		return false, err
	}
	if cfg.Cloud == nil {
		return false, newCloudError(
			CloudErrSecretCorrupt,
			"checkpoint Cloud device revocation",
			nil,
		)
	}
	if cfg.Cloud.DeviceRevocationCompleted != nil {
		return finishCheckpointedCloudDeviceRevocation(
			ctx,
			cfg,
			profileName,
			remoteOnly,
			report,
		)
	}
	targets, checkpoint, err := planCloudDeviceRevocation(
		ctx,
		cfg,
		profileName,
		remoteOnly,
	)
	if err != nil {
		return false, err
	}
	if checkpoint == nil {
		return false, nil
	}
	for _, target := range targets {
		revokeConfig := target.config
		if revokeConfig.RelayInstanceID == "" &&
			target.credential.relayInstanceID != "" {
			// Activation precedes the durable device-bound checkpoint. If
			// that write failed, pending provenance is the only durable Relay
			// identity available for exact self-revocation.
			revokeConfig.RelayInstanceID =
				target.credential.relayInstanceID
		}
		if err := revokeRemoteCloudDeviceForCLI(
			ctx,
			revokeConfig,
			store,
			target.credential.value,
		); err != nil {
			return false, err
		}
	}
	if err := checkpointRevocation(*checkpoint); err != nil {
		return false, fmt.Errorf(
			"save Cloud device revocation checkpoint for server %q: %w",
			profileName,
			err,
		)
	}
	currentRemoved, err := deleteCheckpointedCloudDeviceCredentials(
		ctx,
		profileName,
		*checkpoint,
	)
	if err != nil {
		return false, err
	}
	reportCloudDeviceRevocation(
		report,
		profileName,
		remoteOnly,
		true,
	)
	return currentRemoved, nil
}

func planCloudDeviceRevocation(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
	remoteOnly bool,
) (
	[]remoteCloudDeviceRevocationTarget,
	*cloudDeviceRevocationCheckpoint,
	error,
) {
	currentService := deviceCredentialServiceForProfile(profileName)
	currentValue, currentExists, err := readCredentialSlotWithPolicy(
		ctx,
		currentService,
		SecretStoreForbidUI,
	)
	if err != nil {
		return nil, nil, err
	}
	currentID := deviceIDOf(currentValue)
	activationCheckpoint := cloudLifecycleMayHaveActivatedPendingDevice(
		cfg.Cloud,
	)
	if !activationCheckpoint {
		if !remoteOnly {
			return nil, nil, nil
		}
		if !currentExists {
			if cfg.Cloud.Current == nil {
				return nil, nil, nil
			}
			return nil, nil, missingCloudDeviceRevocationCredential(
				profileName,
				"current",
			)
		}
		currentConfig, err := currentCloudDeviceRevocationConfig(cfg)
		if err != nil {
			return nil, nil, err
		}
		return []remoteCloudDeviceRevocationTarget{{
				credential: remoteCloudDeviceCredential{
					value:   currentValue,
					service: currentService,
				},
				config: currentConfig,
			}}, &cloudDeviceRevocationCheckpoint{
				CurrentDeviceID: currentID,
			}, nil
	}

	expectedID := cfg.Cloud.DeviceActivationDeviceID
	pending, pendingExists, err := selectPendingRemoteCloudDeviceCredential(
		ctx,
		cfg,
		profileName,
	)
	if err != nil {
		return nil, nil, err
	}
	pendingID := deviceIDOf(pending.value)
	if pendingExists && pendingID != expectedID {
		return nil, nil, cloudDeviceRevocationIdentityMismatch(
			profileName,
			"pending",
		)
	}
	if !pendingExists {
		if cfg.Cloud.State != cloudStateDeviceBoundOrPaired {
			return nil, nil, missingCloudDeviceRevocationCredential(
				profileName,
				"activation-era pending",
			)
		}
		if !currentExists {
			return nil, nil, missingCloudDeviceRevocationCredential(
				profileName,
				"promoted activation-era current",
			)
		}
		if currentID != expectedID {
			return nil, nil, cloudDeviceRevocationIdentityMismatch(
				profileName,
				"promoted current",
			)
		}
		return []remoteCloudDeviceRevocationTarget{{
				credential: remoteCloudDeviceCredential{
					value:   currentValue,
					service: currentService,
				},
				config: cfg,
			}}, &cloudDeviceRevocationCheckpoint{
				CurrentDeviceID: currentID,
			}, nil
	}

	targets := []remoteCloudDeviceRevocationTarget{{
		credential: pending,
		config:     cfg,
	}}
	checkpoint := &cloudDeviceRevocationCheckpoint{
		PendingDeviceID: pendingID,
	}
	if currentExists && (remoteOnly || currentID == expectedID) {
		if currentID == expectedID && currentValue != pending.value {
			return nil, nil, cloudDeviceRevocationIdentityMismatch(
				profileName,
				"duplicate activated",
			)
		}
		checkpoint.CurrentDeviceID = currentID
		if currentID != expectedID {
			currentConfig, err := currentCloudDeviceRevocationConfig(cfg)
			if err != nil {
				return nil, nil, err
			}
			targets = append(targets, remoteCloudDeviceRevocationTarget{
				credential: remoteCloudDeviceCredential{
					value:   currentValue,
					service: currentService,
				},
				config: currentConfig,
			})
		}
	}
	return targets, checkpoint, nil
}

// validateCloudDeviceRevocationPlan runs every deterministic secure-storage
// and identity check used by the destructive revocation flow without writing
// a checkpoint, revoking a device, or deleting a credential.
func validateCloudDeviceRevocationPlan(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
) error {
	remoteOnly, err := isRemoteOnlyCloudProfile(cfg)
	if err != nil {
		return err
	}
	if cfg.Cloud == nil {
		return newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud device revocation plan",
			nil,
		)
	}
	if cfg.Cloud.DeviceRevocationCompleted != nil {
		if err := validateCloudDeviceRevocationCheckpoint(*cfg.Cloud); err != nil {
			return newCloudError(
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
			return err
		}
		return validateCheckpointedPendingCloudDeviceCredential(
			ctx,
			cfg,
			profileName,
			checkpoint.PendingDeviceID,
		)
	}
	_, _, err = planCloudDeviceRevocation(
		ctx,
		cfg,
		profileName,
		remoteOnly,
	)
	return err
}

func reportCloudDeviceRevocation(
	report *uninstallReport,
	profileName string,
	remoteOnly bool,
	revokedRemotely bool,
) {
	if report != nil {
		report.addRemoved(cloudDeviceCredentialLabel(
			profileName,
			remoteOnly,
		))
		if revokedRemotely {
			report.addNote(fmt.Sprintf(
				"Revoked the Cloud device pairing for server %q.",
				profileName,
			))
		}
	}
}

func missingCloudDeviceRevocationCredential(
	profileName string,
	slot string,
) error {
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: fmt.Sprintf(
			"the %s Cloud device credential for server %q is missing; cleanup stopped before OAuth, secure-storage, or configuration deletion. As a Home Assistant Owner, open NOVA from the sidebar, find this computer under Devices, and choose Revoke. Keep this profile checkpoint for recovery",
			slot,
			profileName,
		),
		Cause: newCloudError(
			CloudErrSecretNotFound,
			"load Cloud device credential for verified revocation",
			nil,
		),
	}
}
