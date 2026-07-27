package main

import "context"

const (
	invalidOwnerPairingCodeMessage  = "That code is not six digits."
	rejectedOwnerPairingCodeMessage = "That code expired or was rejected."
)

func provisionRemoteCloudDeviceWithOwnerCode(
	ctx context.Context,
	session cloudVerifiedSession,
	request cloudRemoteSetupRequest,
	expectedRelayInstanceID string,
	appURL string,
) (*cloudProvisionedCredential, error) {
	retryReason := ""
	for {
		code, err := request.PairingCode(cloudRemotePairingPrompt{
			AppURL:      appURL,
			RetryReason: retryReason,
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
			deviceMetadata{
				Name:            hostLabel(),
				Platform:        defaultPairingClientInfo().platform,
				Client:          defaultPairingClientInfo().client,
				ClientInstallID: request.Config.ClientInstallID,
			},
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
