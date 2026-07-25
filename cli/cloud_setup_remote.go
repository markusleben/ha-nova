package main

import (
	"context"
	"fmt"
)

type cloudRemoteSetupRequest struct {
	cloudSetupRequest
	Origin      CloudOrigin
	PairingCode cloudRemotePairingCodeProvider
}

type cloudRemoteSetupCoordinator interface {
	cloudSetupCoordinator
	AddRemoteWithPairing(
		context.Context,
		cloudRemoteSetupRequest,
	) (cloudSetupResult, error)
}

var pairDeviceV2ForCloudSetup = pairDeviceV2
var discoverCloudFromLocalRelayForRemoteSetup = discoverCloudFromLocalRelay
var establishRemoteCloudDeviceForSetup = establishRemoteCloudDevice

func (coordinator productionCloudSetupCoordinator) AddRemoteWithPairing(
	ctx context.Context,
	request cloudRemoteSetupRequest,
) (cloudSetupResult, error) {
	if request.PairingCode == nil ||
		request.Config.ClientInstallID == "" ||
		request.Origin.CanonicalOrigin == "" {
		return cloudSetupResult{}, newCloudError(
			CloudErrInvalidInput,
			"prepare remote Cloud setup",
			nil,
		)
	}
	expectedRelayInstanceID, err := expectedRemoteCloudRelayIdentity(
		ctx,
		request,
	)
	if err != nil {
		return cloudSetupResult{}, err
	}
	if err := preflightRemoteCloudDeviceStateWithContext(
		ctx,
		expectedRelayInstanceID,
	); err != nil {
		return cloudSetupResult{}, err
	}
	result, session, store, err := authorizeAndVerifyCloudForSetup(
		coordinator,
		ctx,
		request.cloudSetupRequest,
		request.Origin,
		expectedRelayInstanceID,
	)
	if err != nil {
		return cloudSetupResult{}, err
	}
	if err := rejectCloudReconnectUserChange(
		request.Config,
		session.User.ID,
	); err != nil {
		return cloudSetupResult{}, err
	}
	if err := reopenRemoteCloudDeviceAccess(
		ctx,
		session.Relay.RelayInstanceID,
		"Home Assistant Cloud sign-in",
	); err != nil {
		return cloudSetupResult{}, err
	}
	credential, err := establishRemoteCloudDeviceForSetup(
		ctx,
		session,
		request,
		expectedRelayInstanceID,
	)
	if err != nil {
		return cloudSetupResult{}, err
	}
	current, err := promoteCloudAuthorization(
		ctx,
		store,
		result.Current.CredentialGeneration,
		session.Envelope,
	)
	if err != nil {
		return cloudSetupResult{}, err
	}
	if parseDeviceCredential(credential) == nil {
		return cloudSetupResult{}, newCloudError(
			CloudErrDeviceRejected,
			"verify remote Cloud device",
			nil,
		)
	}
	result.Current = cloudMetadataFromEnvelope(request.Origin, current)
	result.RelayInstanceID = session.Relay.RelayInstanceID
	return result, nil
}

func establishRemoteCloudDevice(
	ctx context.Context,
	session cloudVerifiedSession,
	request cloudRemoteSetupRequest,
	expectedRelayInstanceID string,
) (string, error) {
	if expectedRelayInstanceID != "" &&
		expectedRelayInstanceID != session.Relay.RelayInstanceID {
		return "", newCloudError(
			CloudErrRelayInstance,
			"verify remote Cloud device reuse",
			nil,
		)
	}
	activationRecovery := request.Config.Cloud != nil &&
		request.Config.Cloud.DeviceActivationStarted
	activationRecoveryID := ""
	if activationRecovery {
		activationRecoveryID =
			request.Config.Cloud.DeviceActivationDeviceID
	}
	if pending, exists, err := readPendingDeviceCredentialRecordWithPolicy(
		ctx,
		SecretStoreForbidUI,
	); err != nil {
		return "", err
	} else if exists {
		if pending.Source != pendingDeviceCredentialSourceCloud {
			return "", newCloudError(
				CloudErrDevicePendingConflict,
				"resume remote Cloud device",
				nil,
			)
		}
		if pending.RelayInstanceID != session.Relay.RelayInstanceID {
			return "", newCloudError(
				CloudErrRelayInstance,
				"match pending Cloud device to Relay",
				nil,
			)
		}
		parsed := parseDeviceCredential(pending.Credential)
		if parsed == nil {
			return "", newCloudError(
				CloudErrDeviceRejected,
				"resume remote Cloud device",
				nil,
			)
		}
		if err := checkpointRemoteCloudDeviceActivation(
			request,
			parsed.deviceID,
		); err != nil {
			return "", err
		}
		activated, err := session.Ingress.ActivateDevice(
			ctx,
			pending.Credential,
			session.Relay.RelayInstanceID,
		)
		if err != nil {
			if !IsCloudErrorCode(err, CloudErrDeviceRejected) {
				return "", err
			}
			// A reached-Relay rejection is definitive. It cannot be a lost
			// successful activation response, because activation is idempotent
			// for the exact credential and binding. Clear only this Cloud-proven
			// pending value before allowing a fresh owner pairing below.
			if err := discardRejectedPendingCloudDeviceCredential(
				ctx,
				request,
			); err != nil {
				return "", err
			}
			activationRecovery = false
			activationRecoveryID = ""
		} else {
			if activated.DeviceID != parsed.deviceID {
				return "", newCloudError(
					CloudErrIdentityMismatch,
					"resume remote Cloud device",
					nil,
				)
			}
			if err := checkpointRemoteCloudDeviceBound(
				request,
				session.Relay.RelayInstanceID,
			); err != nil {
				return "", err
			}
			if err := promotePendingDeviceCredentialWithPolicy(
				ctx,
				SecretStoreForbidUI,
			); err != nil {
				return "", err
			}
			return pending.Credential, nil
		}
	}
	if activationRecovery &&
		request.Config.Cloud.State != cloudStateDeviceBoundOrPaired {
		return "", newCloudError(
			CloudErrSecretNotFound,
			"resume remote Cloud device activation",
			nil,
		)
	}
	if current, exists, err := readDeviceCredentialWithPolicy(
		ctx,
		SecretStoreForbidUI,
	); err != nil {
		return "", err
	} else if exists && expectedRelayInstanceID != "" {
		parsed := parseDeviceCredential(current)
		if parsed == nil {
			return "", newCloudError(
				CloudErrDeviceRejected,
				"reuse remote Cloud device",
				nil,
			)
		}
		if activationRecovery &&
			parsed.deviceID != activationRecoveryID {
			return "", newCloudError(
				CloudErrIdentityMismatch,
				"resume promoted remote Cloud device",
				nil,
			)
		}
		// A device credential is a bearer secret bound to one Relay. Reuse it
		// only when that Relay identity was proven before OAuth from persisted
		// state or authenticated local discovery. A Cloud-only reconnect with
		// no prior Relay identity must create an owner-authorized replacement
		// without disclosing the old secret to the newly authenticated Relay.
		bound, err := session.Ingress.BindDevice(
			ctx,
			current,
			session.Relay.RelayInstanceID,
		)
		if err != nil &&
			!IsCloudErrorCode(err, CloudErrDeviceRejected) {
			return "", err
		}
		if err == nil {
			if bound.DeviceID != parsed.deviceID {
				return "", newCloudError(
					CloudErrIdentityMismatch,
					"reuse remote Cloud device",
					nil,
				)
			}
			if err := checkpointRemoteCloudDeviceBound(
				request,
				session.Relay.RelayInstanceID,
			); err != nil {
				return "", err
			}
			return current, nil
		}
		if activationRecovery {
			if err := clearRemoteCloudDeviceActivation(request); err != nil {
				return "", err
			}
		}
		// A Relay-proven rejection is definitive: the old credential did not
		// execute and cannot be reused. Keep it current while the Owner
		// authorizes a replacement below; pending activation then swaps the
		// credential atomically. Network, ingress, and protocol failures return
		// above and never trigger a second path.
	} else if activationRecovery {
		return "", newCloudError(
			CloudErrSecretNotFound,
			"resume promoted remote Cloud device",
			nil,
		)
	}

	if _, err := probeDeviceCredentialStorageWithPolicy(
		ctx,
		SecretStoreForbidUI,
	); err != nil {
		return "", fmt.Errorf(
			"cannot store the remote device credential before pairing: %w",
			err,
		)
	}
	appURL, err := canonicalCloudAppURL(request.Origin, session.App)
	if err != nil {
		return "", err
	}
	code, err := request.PairingCode(cloudRemotePairingPrompt{AppURL: appURL})
	if err != nil {
		return "", err
	}
	code, err = normalizeRelayPairingCode(code)
	if err != nil {
		return "", &CloudError{
			Code:  CloudErrPairingRejected,
			Op:    "validate Cloud pairing code",
			cause: err,
		}
	}
	// The Owner may take several minutes to retrieve the one-time code in a
	// separate browser session. Re-open storage again before the code is
	// consumed or any pending device credential is written.
	if err := reopenRemoteCloudDeviceAccess(
		ctx,
		session.Relay.RelayInstanceID,
		"owner pairing confirmation",
	); err != nil {
		return "", err
	}
	provisioned, err := pairDeviceV2ForCloudSetup(
		ctx,
		session.Ingress,
		code,
		deviceMetadata{
			Name:            hostLabel(),
			Platform:        defaultPairingClientInfo().platform,
			Client:          defaultPairingClientInfo().client,
			ClientInstallID: request.Config.ClientInstallID,
		},
		session.Relay.RelayInstanceID,
	)
	if err != nil {
		return "", err
	}
	if err := writePendingCloudDeviceCredentialWithPolicy(
		ctx,
		provisioned.Credential,
		session.Relay.RelayInstanceID,
		SecretStoreForbidUI,
	); err != nil {
		return "", fmt.Errorf("store remote device credential: %w", err)
	}
	if err := checkpointRemoteCloudDeviceActivation(
		request,
		provisioned.DeviceID,
	); err != nil {
		return "", err
	}
	activated, err := session.Ingress.ActivateDevice(
		ctx,
		provisioned.Credential,
		session.Relay.RelayInstanceID,
	)
	if err != nil {
		if IsCloudErrorCode(err, CloudErrDeviceRejected) {
			if cleanupErr := discardRejectedPendingCloudDeviceCredential(
				ctx,
				request,
			); cleanupErr != nil {
				return "", cleanupErr
			}
		}
		return "", err
	}
	if activated.DeviceID != provisioned.DeviceID {
		return "", newCloudError(
			CloudErrIdentityMismatch,
			"activate remote Cloud device",
			nil,
		)
	}
	if err := checkpointRemoteCloudDeviceBound(
		request,
		session.Relay.RelayInstanceID,
	); err != nil {
		return "", err
	}
	if err := promotePendingDeviceCredentialWithPolicy(
		ctx,
		SecretStoreForbidUI,
	); err != nil {
		return "", fmt.Errorf("finalize remote device credential: %w", err)
	}
	return provisioned.Credential, nil
}

var _ cloudRemoteSetupCoordinator = productionCloudSetupCoordinator{}
