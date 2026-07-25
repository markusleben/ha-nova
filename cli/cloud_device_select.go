package main

import (
	"context"
	"errors"
)

func selectPendingRemoteCloudDeviceCredential(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
) (remoteCloudDeviceCredential, bool, error) {
	service := deviceCredentialPendingServiceForProfile(profileName)
	raw, err := secretGetWithPolicy(
		ctx,
		service,
		SecretStoreForbidUI,
	)
	if errors.Is(err, errSecretNotFound) {
		return remoteCloudDeviceCredential{}, false, nil
	}
	if err != nil {
		return remoteCloudDeviceCredential{}, false, err
	}
	pending, decodeErr := decodePendingDeviceCredentialRecord(raw)
	if decodeErr != nil ||
		pending.Source != pendingDeviceCredentialSourceCloud ||
		(cfg.RelayInstanceID != "" &&
			pending.RelayInstanceID != cfg.RelayInstanceID) {
		return remoteCloudDeviceCredential{}, false, newCloudError(
			CloudErrSecretCorrupt,
			"validate bound pending Cloud device provenance",
			decodeErr,
		)
	}
	return remoteCloudDeviceCredential{
		value:           pending.Credential,
		service:         service,
		relayInstanceID: pending.RelayInstanceID,
	}, true, nil
}
