package main

import (
	"context"
	"fmt"
)

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
	relayAlreadyRemoved bool,
) (bool, error) {
	remoteOnly, err := isRemoteOnlyCloudProfile(cfg)
	if err != nil {
		return false, err
	}
	var selected remoteCloudDeviceCredential
	var exists bool
	activationCheckpoint := cloudLifecycleMayHaveActivatedPendingDevice(cfg.Cloud)
	if activationCheckpoint {
		selected, exists, err = selectPendingRemoteCloudDeviceCredential(
			ctx,
			cfg,
			profileName,
		)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, missingCloudDeviceRevocationCredential(
				profileName,
				"activation-era pending",
			)
		}
	} else if remoteOnly {
		selected, exists, err = selectRemoteCloudDeviceCredential(
			ctx,
			cfg,
			profileName,
		)
	}
	if err != nil {
		return false, err
	}
	if !exists {
		if remoteOnly && cfg.Cloud != nil && cfg.Cloud.Current != nil {
			return false, missingCloudDeviceRevocationCredential(
				profileName,
				"current",
			)
		}
		return false, nil
	}
	currentService := deviceCredentialServiceForProfile(profileName)
	targets := []remoteCloudDeviceRevocationTarget{{
		credential: selected,
		config:     cfg,
	}}
	if !relayAlreadyRemoved &&
		selected.service == currentService {
		currentConfig, err := currentCloudDeviceRevocationConfig(cfg)
		if err != nil {
			return false, err
		}
		targets[0].config = currentConfig
	}
	if !relayAlreadyRemoved &&
		selected.service != currentService &&
		cloudDeviceActivationUncertain(cfg.Cloud) {
		current, currentExists, err := readCredentialSlotWithPolicy(
			ctx,
			currentService,
			SecretStoreForbidUI,
		)
		if err != nil {
			return false, err
		}
		if currentExists {
			currentConfig, err := currentCloudDeviceRevocationConfig(cfg)
			if err != nil {
				return false, err
			}
			targets = append(targets, remoteCloudDeviceRevocationTarget{
				credential: remoteCloudDeviceCredential{
					value:   current,
					service: currentService,
				},
				config: currentConfig,
			})
		}
	}
	if !relayAlreadyRemoved {
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
	}
	services := []string{selected.service}
	if selected.service != currentService {
		// Do not delete either slot until every potentially live credential has
		// a verified revocation outcome. This preserves exact retry material.
		services = append([]string{currentService}, services...)
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
	if report != nil {
		report.addRemoved(cloudDeviceCredentialLabel(
			profileName,
			remoteOnly,
		))
		if !relayAlreadyRemoved {
			report.addNote(fmt.Sprintf(
				"Revoked the Cloud device pairing for server %q.",
				profileName,
			))
		}
	}
	return true, nil
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
