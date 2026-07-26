package main

import (
	"context"
	"runtime"
)

type productionCloudSetupCoordinator struct{}

var revokeAndVerifyCloudAuthorizationForCLI = revokeAndVerifyCloudAuthorization
var discoverCloudFromLocalRelayForSetup = discoverCloudFromLocalRelay
var readCloudDeviceCredentialForSetup = readDeviceCredentialWithPolicy
var authorizeAndVerifyCloudForSetup = func(
	coordinator productionCloudSetupCoordinator,
	ctx context.Context,
	request cloudSetupRequest,
	origin CloudOrigin,
	expectedRelayInstanceID string,
) (cloudSetupResult, cloudVerifiedSession, OAuthSecretStore, error) {
	return coordinator.authorizeAndVerify(
		ctx,
		request,
		origin,
		expectedRelayInstanceID,
	)
}

var cloudOAuthFlowForSetup = func() *OAuthLoopbackFlow {
	return NewOAuthLoopbackFlow()
}

var exchangeCloudAuthorizationCodeForSetup = func(
	ctx context.Context,
	oauth *HAOAuthClient,
	authorization OAuthAuthorizationCode,
) (HAOAuthToken, error) {
	return oauth.ExchangeAuthorizationCode(ctx, authorization)
}

var openCloudOAuthBrowserForSetup OAuthBrowserOpener = func(
	_ context.Context,
	target string,
) error {
	return openBrowserForSetup(target)
}

var cloudInteractivePromptSessionForSetup = func() bool {
	return uiInputSupportsTTY() &&
		stdoutIsInteractiveTTY() &&
		platformNativeSecretPromptAvailable()
}

func (productionCloudSetupCoordinator) Available() bool {
	if !cloudRemoteFeatureAvailable() {
		return false
	}
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		return true
	default:
		return false
	}
}

func (productionCloudSetupCoordinator) Preflight(
	ctx context.Context,
	profileID string,
) error {
	if !cloudInteractivePromptSessionForSetup() {
		return newCloudError(
			CloudErrUnsupportedPlatform,
			"prepare Cloud authorization",
			nil,
		)
	}
	store, exists := cloudSecretStoreForOperation(ctx, profileID)
	if !exists {
		var err error
		store, err = newCloudSecretStoreForCLI(profileID)
		if err != nil {
			return err
		}
	}
	return PreflightOAuthSecretStore(ctx, store, SecretStoreAllowUI)
}

func (coordinator productionCloudSetupCoordinator) AddAwayWithExistingDevice(
	ctx context.Context,
	request cloudSetupRequest,
) (cloudSetupResult, error) {
	discovery, err := discoverCloudFromLocalRelayForSetup(ctx, request.Config)
	if err != nil {
		return cloudSetupResult{}, err
	}
	if request.Config.RelayInstanceID != "" &&
		request.Config.RelayInstanceID != discovery.RelayInstanceID {
		return cloudSetupResult{}, newCloudError(
			CloudErrRelayInstance,
			"match local NOVA Relay identity",
			nil,
		)
	}
	credential, exists, err := readCloudDeviceCredentialForSetup(
		ctx,
		SecretStoreForbidUI,
	)
	if err != nil {
		return cloudSetupResult{}, err
	}
	if !exists {
		return cloudSetupResult{}, newCloudError(
			CloudErrDeviceRejected,
			"load local device credential",
			nil,
		)
	}
	result, session, store, err := authorizeAndVerifyCloudForSetup(
		coordinator,
		ctx,
		request,
		discovery.Origin,
		discovery.RelayInstanceID,
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
	bound, err := session.Ingress.BindDevice(
		ctx,
		credential,
		discovery.RelayInstanceID,
	)
	if err != nil {
		return cloudSetupResult{}, err
	}
	parsed := parseDeviceCredential(credential)
	if parsed == nil || bound.DeviceID != parsed.deviceID {
		return cloudSetupResult{}, newCloudError(
			CloudErrDeviceRejected,
			"verify bound Cloud device",
			nil,
		)
	}
	if err := request.AdvancePendingLifecycle(cloudStateDeviceBoundOrPaired); err != nil {
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
	result.Current = cloudMetadataFromEnvelope(discovery.Origin, current)
	return result, nil
}

func (productionCloudSetupCoordinator) RetirePrevious(
	ctx context.Context,
	profileID string,
) error {
	store, exists := cloudSecretStoreForOperation(ctx, profileID)
	if !exists {
		var err error
		store, err = newCloudSecretStoreForCLI(profileID)
		if err != nil {
			return err
		}
	}
	retiring, exists, err := store.LoadRetiring(ctx, SecretStoreForbidUI)
	if err != nil || !exists {
		return err
	}
	return store.RevokeRetiring(
		ctx,
		retiring.Generation,
		SecretStoreForbidUI,
		revokeAndVerifyCloudAuthorizationForCLI,
	)
}

func revokeAndVerifyCloudAuthorization(
	ctx context.Context,
	envelope OAuthSecretEnvelope,
) error {
	oauth, err := NewHAOAuthClient(
		envelope.CanonicalOrigin,
		cloudHTTPClientForCLI,
	)
	if err != nil {
		return err
	}
	return revokeAndVerifyCloudAuthorizationWithClient(
		ctx,
		oauth,
		envelope.RefreshToken,
		envelope.ClientID,
	)
}

func revokeAndVerifyCloudAuthorizationWithClient(
	ctx context.Context,
	oauth *HAOAuthClient,
	refreshToken, clientID string,
) error {
	if oauth == nil {
		return newCloudError(
			CloudErrInvalidInput,
			"verify OAuth revocation",
			nil,
		)
	}
	if err := oauth.Revoke(ctx, refreshToken); err != nil {
		return err
	}
	_, err := oauth.Refresh(ctx, refreshToken, clientID)
	if IsCloudErrorCode(err, CloudErrOAuthInvalidGrant) {
		return nil
	}
	if err == nil {
		return newCloudError(
			CloudErrOAuthProtocol,
			"verify OAuth revocation",
			nil,
		)
	}
	return err
}

func init() {
	cloudCoordinatorForSetup = productionCloudSetupCoordinator{}
}
