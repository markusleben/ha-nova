package main

import (
	"bufio"
	"context"
	"io"
)

func renderSetupCompletionOutcome(
	out io.Writer,
	selectedClients []string,
	cloudSetupIncomplete bool,
) int {
	return renderSetupCompletionOutcomeWithCloudPause(
		out,
		selectedClients,
		cloudSetupIncomplete,
		false,
	)
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

func runInteractiveCloudOnlySetup(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	cfg runtimeConfig,
	state *installState,
	target string,
	selectedClients, skippedClients []string,
	lifecycleMarker ...[]byte,
) int {
	exit, back := runInteractiveCloudOnlySetupForWizard(
		reader,
		out,
		paths,
		cfg,
		state,
		target,
		selectedClients,
		skippedClients,
		lifecycleMarker...,
	)
	if back {
		renderSetupCancelledNote(out)
	}
	return exit
}

func finalizeDeviceCredentialRetirement(previous runtimeConfig) error {
	if _, err := revokeDeviceCredentialsForRetirement(previous); err != nil {
		return err
	}
	return deleteDeviceCredentialsForRetirement()
}

func withCloudSecretAccessSession(
	ctx context.Context,
	profileID string,
	store OAuthSecretStore,
) (context.Context, OAuthSecretStore) {
	sessionStore := memoizeOAuthSecretStore(store)
	return installCloudSecretAccessSession(ctx, profileID, sessionStore),
		sessionStore
}

func memoizeOAuthSecretStore(store OAuthSecretStore) OAuthSecretStore {
	sessionStore, activate := stageOAuthSecretStoreMemoization(store)
	activate()
	return sessionStore
}

func writePendingCloudDeviceCredential(
	credential, relayInstanceID string,
) error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return writePendingCloudDeviceCredentialWithPolicy(
		ctx,
		credential,
		relayInstanceID,
		SecretStoreForbidUI,
	)
}

func cloudOriginFromCanonical(raw string) (CloudOrigin, error) {
	parsed, err := ParseCanonicalNabuOrigin(raw)
	if err != nil {
		return CloudOrigin{}, err
	}
	return CloudOrigin{
		InputOrigin:     parsed.String(),
		InputHost:       parsed.Hostname(),
		CanonicalOrigin: parsed.String(),
		CanonicalHost:   parsed.Hostname(),
	}, nil
}
