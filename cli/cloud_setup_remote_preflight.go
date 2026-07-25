package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
)

type cloudRemotePairingPrompt struct {
	AppURL string
}

type cloudRemotePairingCodeProvider func(
	cloudRemotePairingPrompt,
) (string, error)

func promptRemoteOwnerPairingCode(
	reader *bufio.Reader,
	out io.Writer,
	prompt cloudRemotePairingPrompt,
) (string, error) {
	if prompt.AppURL == "" {
		return "", newCloudError(
			CloudErrInvalidInput,
			"prepare remote owner pairing",
			nil,
		)
	}
	renderSetupParagraph(
		out,
		"OAuth sign-in is complete. Device pairing now requires a Home Assistant Owner.",
		"Open a separate private/incognito window or a different browser profile. Sign in to the same Home Assistant Cloud instance as an Owner, open NOVA from the sidebar, and choose “Connect a device”.",
		"Keep the OAuth browser session separate. A standard Home Assistant account is preferred for OAuth, but an Owner OAuth account is also supported.",
	)
	// Do not auto-open the capability URL in the OAuth browser session. That
	// session may be signed in as a standard user and would receive a 403. The
	// user navigates through NOVA's sidebar in the explicitly separate Owner
	// session, so the capability URL also never enters terminal output.
	return promptWizardLineFromReader(
		reader,
		out,
		"Six-digit code from the Owner session",
		"",
	)
}

// OAuth may keep the user in a browser for several minutes, during which the
// desktop keyring can legitimately relock. Re-authorize the selected device
// slots after sign-in and before pairing consumes an owner code or mutates a
// device credential. All operational reads and writes remain no-UI.
func reopenRemoteCloudDeviceAccess(
	ctx context.Context,
	relayInstanceID string,
	after string,
) error {
	if err := preflightWritableCloudDeviceAccess(
		ctx,
		relayInstanceID,
		true,
		SecretStoreAllowUI,
	); err != nil {
		return fmt.Errorf(
			"re-open device secure storage after %s: %w",
			after,
			err,
		)
	}
	return nil
}

func preflightRemoteCloudDeviceState(
	expectedRelayInstanceID string,
) error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return preflightRemoteCloudDeviceStateWithContext(
		ctx,
		expectedRelayInstanceID,
	)
}

func preflightRemoteCloudDeviceStateWithContext(
	ctx context.Context,
	expectedRelayInstanceID string,
) error {
	return inspectCloudDeviceAccess(
		ctx,
		expectedRelayInstanceID,
		true,
		true,
		SecretStoreForbidUI,
	)
}
