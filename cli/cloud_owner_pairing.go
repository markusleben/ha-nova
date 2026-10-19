package main

import (
	"context"
	"errors"
)

const (
	invalidOwnerPairingCodeMessage  = "That code is not six digits."
	rejectedOwnerPairingCodeMessage = "That code expired or was rejected."
	olderAppOwnerPairingNote        = "Your NOVA App still needs a code. Update the NOVA App in Home Assistant to connect without one."
)

func provisionRemoteCloudDeviceWithOwnerCode(
	ctx context.Context,
	session cloudVerifiedSession,
	request cloudRemoteSetupRequest,
	expectedRelayInstanceID string,
	appURL string,
) (*cloudProvisionedCredential, error) {
	installID, err := pairingClientInstallID(
		request.Config.ClientInstallID,
		activeServerProfile(),
	)
	if err != nil {
		return nil, newCloudError(
			CloudErrInvalidInput,
			"prepare owner pairing",
			err,
		)
	}
	metadata := deviceMetadata{
		Name:            hostLabel(),
		Platform:        defaultPairingClientInfo().platform,
		Client:          defaultPairingClientInfo().client,
		ClientInstallID: installID,
	}
	// The Link sign-in user is the owner (auth/current_user over Link; the
	// Relay re-checks with Home Assistant before arming): no code needed.
	ownerNote := ""
	if session.User.IsOwner {
		provisioned, err := pairDeviceV2LinkOwnerForCloudSetup(
			ctx,
			session.Ingress,
			metadata,
			expectedRelayInstanceID,
		)
		if err == nil || !linkOwnerPairingNeedsCode(err) {
			return provisioned, err
		}
		if errors.Is(err, errSignInRelayUnsupported) {
			ownerNote = olderAppOwnerPairingNote
		}
	}
	retryReason := ""
	for {
		code, err := request.PairingCode(cloudRemotePairingPrompt{
			AppURL:      appURL,
			RetryReason: retryReason,
			OwnerNote:   ownerNote,
		})
		if err != nil {
			return nil, err
		}
		code, err = normalizeRelayPairingCode(code)
		if err != nil {
			retryReason = invalidOwnerPairingCodeMessage
			continue
		}

		// The Owner may take several minutes to retrieve the one-time code in a
		// separate browser session. Re-open storage before every attempt, prior
		// to consuming the code or writing a pending device credential.
		if err := reopenRemoteCloudDeviceAccess(
			ctx,
			expectedRelayInstanceID,
			"owner pairing confirmation",
		); err != nil {
			return nil, err
		}
		provisioned, err := pairDeviceV2ForCloudSetup(
			ctx,
			session.Ingress,
			code,
			metadata,
			expectedRelayInstanceID,
		)
		if err == nil {
			return provisioned, nil
		}
		if IsCloudErrorCode(err, CloudErrPairingInactive) ||
			IsCloudErrorCode(err, CloudErrPairingRejected) {
			retryReason = rejectedOwnerPairingCodeMessage
			continue
		}
		return nil, err
	}
}

// linkOwnerPairingNeedsCode: the Relay definitively refused the no-code path
// before any device existed, so the owner code is the safe way on. An older
// App (no /pair/v2/arm/link), a Relay-side owner refusal, an unavailable slot,
// or a slot the NOVA page replaced meanwhile. Ambiguous failures stop instead.
func linkOwnerPairingNeedsCode(err error) bool {
	return errors.Is(err, errSignInRelayUnsupported) ||
		errors.Is(err, errSignInNotOwner) ||
		errors.Is(err, errSignInRelayNotReady) ||
		IsCloudErrorCode(err, CloudErrPairingInactive) ||
		IsCloudErrorCode(err, CloudErrPairingRejected)
}
