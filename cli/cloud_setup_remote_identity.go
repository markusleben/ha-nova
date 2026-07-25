package main

import "context"

func checkpointRemoteCloudDeviceActivation(
	request cloudRemoteSetupRequest,
) error {
	if request.CheckpointDeviceActivation == nil {
		return newCloudError(
			CloudErrInvalidInput,
			"checkpoint remote Cloud device activation",
			nil,
		)
	}
	return request.CheckpointDeviceActivation()
}

func clearRemoteCloudDeviceActivation(
	request cloudRemoteSetupRequest,
) error {
	if request.ClearDeviceActivation == nil {
		return newCloudError(
			CloudErrInvalidInput,
			"clear remote Cloud device activation",
			nil,
		)
	}
	return request.ClearDeviceActivation()
}

func checkpointRemoteCloudDeviceBound(
	request cloudRemoteSetupRequest,
	relayInstanceID string,
) error {
	if request.CheckpointDeviceBinding == nil {
		return newCloudError(
			CloudErrInvalidInput,
			"checkpoint remote Cloud device",
			nil,
		)
	}
	return request.CheckpointDeviceBinding(relayInstanceID)
}

func expectedRemoteCloudRelayIdentity(
	ctx context.Context,
	request cloudRemoteSetupRequest,
) (string, error) {
	expected := request.Config.RelayInstanceID
	hasLocalURL := request.Config.RelaySecureBaseURL != ""
	hasLocalPin := request.Config.RelaySpkiPin != ""
	if expected != "" || (!hasLocalURL && !hasLocalPin) {
		return expected, nil
	}
	if !hasLocalURL || !hasLocalPin {
		return "", newCloudError(
			CloudErrRelayInstance,
			"prove local NOVA Relay identity before Cloud authorization",
			nil,
		)
	}
	discovery, err := discoverCloudFromLocalRelayForRemoteSetup(
		ctx,
		request.Config,
	)
	if err != nil {
		return "", err
	}
	if discovery.Origin.CanonicalOrigin != request.Origin.CanonicalOrigin {
		return "", newCloudError(
			CloudErrIdentityMismatch,
			"match local and requested Home Assistant Cloud origins",
			nil,
		)
	}
	if !validIdentifier(discovery.RelayInstanceID, 256) {
		return "", newCloudError(
			CloudErrRelayInstance,
			"prove local NOVA Relay identity before Cloud authorization",
			nil,
		)
	}
	return discovery.RelayInstanceID, nil
}

func canonicalCloudAppURL(origin CloudOrigin, app HAAddonInfo) (string, error) {
	canonical, err := ParseCanonicalNabuOrigin(origin.CanonicalOrigin)
	if err != nil {
		return "", err
	}
	if _, err := app.MachineIngressRoot(); err != nil {
		return "", err
	}
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		return "", err
	}
	return canonical.String() + "/app/" + appSlug, nil
}
