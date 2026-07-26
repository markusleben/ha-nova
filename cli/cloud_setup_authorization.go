package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (productionCloudSetupCoordinator) authorizeAndVerify(
	ctx context.Context,
	request cloudSetupRequest,
	origin CloudOrigin,
	expectedRelayInstanceID string,
) (cloudSetupResult, cloudVerifiedSession, OAuthSecretStore, error) {
	if request.Config.ProfileID == "" {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, newCloudError(
			CloudErrInvalidInput,
			"prepare Cloud profile",
			nil,
		)
	}
	store, exists := cloudSecretStoreForOperation(
		ctx,
		request.Config.ProfileID,
	)
	if !exists {
		var err error
		store, err = newCloudSecretStoreForCLI(request.Config.ProfileID)
		if err != nil {
			return cloudSetupResult{}, cloudVerifiedSession{}, nil, err
		}
	}
	envelope, alreadyCurrent, err := resumableCloudEnvelope(
		ctx,
		store,
		request.Config,
		origin,
		SecretStoreForbidUI,
	)
	if err != nil {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, err
	}
	accessToken, envelope, err := authorizeOrRefreshCloud(
		ctx,
		request,
		origin,
		store,
		envelope,
	)
	if err != nil {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, err
	}
	pendingMetadata := cloudMetadataFromEnvelope(origin, envelope)
	if err := request.PersistPendingMetadata(pendingMetadata); err != nil {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, err
	}
	if err := request.AdvancePendingLifecycle(cloudStateTokenStored); err != nil {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, err
	}
	session, err := verifyCloudAccess(
		ctx,
		envelope,
		accessToken,
		origin,
		cloudHTTPClientForCLI,
	)
	if err != nil {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, err
	}
	if !session.Relay.RemoteEnabled() {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, newCloudError(
			CloudErrAppNotReady,
			"verify Cloud Remote is enabled on NOVA Relay",
			nil,
		)
	}
	if expectedRelayInstanceID != "" &&
		session.Relay.RelayInstanceID != expectedRelayInstanceID {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, newCloudError(
			CloudErrRelayInstance,
			"match local and Cloud NOVA Relay identities",
			nil,
		)
	}
	if alreadyCurrent {
		err = store.UpdateCurrent(
			ctx,
			session.Envelope,
			envelope.Generation,
			SecretStoreForbidUI,
		)
	} else {
		err = store.UpdatePending(
			ctx,
			session.Envelope,
			envelope.Generation,
			SecretStoreForbidUI,
		)
	}
	if err != nil {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, err
	}
	session.Envelope = normalizedVerifiedEnvelope(session.Envelope, alreadyCurrent)
	verifiedMetadata := cloudMetadataFromEnvelope(origin, session.Envelope)
	if err := request.PersistPendingMetadata(verifiedMetadata); err != nil {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, err
	}
	if err := request.AdvancePendingLifecycle(cloudStateCloudVerified); err != nil {
		return cloudSetupResult{}, cloudVerifiedSession{}, nil, err
	}
	return cloudSetupResult{
		Current:         verifiedMetadata,
		RelayInstanceID: session.Relay.RelayInstanceID,
	}, session, store, nil
}

func authorizeOrRefreshCloud(
	ctx context.Context,
	request cloudSetupRequest,
	origin CloudOrigin,
	store OAuthSecretStore,
	envelope OAuthSecretEnvelope,
) (string, OAuthSecretEnvelope, error) {
	if envelope.Generation != "" {
		oauth, err := NewHAOAuthClient(
			envelope.CanonicalOrigin,
			cloudHTTPClientForCLI,
		)
		if err != nil {
			return "", OAuthSecretEnvelope{}, err
		}
		token, err := oauth.Refresh(
			ctx,
			envelope.RefreshToken,
			envelope.ClientID,
		)
		if err != nil {
			if IsCloudErrorCode(err, CloudErrOAuthInvalidGrant) &&
				resumableAuthorizationNeedsCleanup(
					request.Config,
					envelope.Generation,
				) {
				if cleanupErr := cleanupAmbiguousPendingAuthorization(
					ctx,
					store,
					envelope.Generation,
					envelope.RefreshToken,
					envelope.ClientID,
					request.ClearPendingAuthorization,
				); cleanupErr != nil {
					return "", OAuthSecretEnvelope{}, &cloudProblem{
						Code:        cloudProblemAuthorization,
						Remediation: cloudRemediationSecurityStop,
						Detail: "the revoked pending authorization could not be " +
							"cleared safely; keep its HA NOVA profile and retry " +
							"from an interactive desktop session",
						Cause: errors.Join(err, cleanupErr),
					}
				}
			}
			return "", OAuthSecretEnvelope{}, err
		}
		return token.AccessToken, envelope, nil
	}
	if err := verifyCloudOriginForOAuth(ctx, origin); err != nil {
		return "", OAuthSecretEnvelope{}, err
	}
	if request.PauseOAuthAuthorization == nil ||
		request.ResumeOAuthAuthorization == nil {
		return "", OAuthSecretEnvelope{}, newCloudError(
			CloudErrInvalidInput,
			"prepare OAuth mutation-lock handoff",
			nil,
		)
	}

	generation := ""
	if request.Config.Cloud != nil &&
		request.Config.Cloud.State == cloudStateAuthorizing &&
		request.Config.Cloud.Pending != nil {
		generation = request.Config.Cloud.Pending.CredentialGeneration
	}
	if generation == "" {
		var err error
		generation, err = NewOAuthSecretGeneration()
		if err != nil {
			return "", OAuthSecretEnvelope{}, err
		}
	}
	flow := cloudOAuthFlowForSetup()
	previousHook := flow.BeforeBrowser
	mutationPaused := false
	flow.BeforeBrowser = func(
		hookCtx context.Context,
		preparation OAuthLoopbackPreparation,
	) error {
		metadata := cloudConnectionMetadata{
			Origin:               origin.InputOrigin,
			CanonicalOrigin:      preparation.CanonicalOrigin,
			OAuthClientID:        preparation.ClientID,
			CredentialGeneration: generation,
		}
		if err := request.PersistPendingMetadata(metadata); err != nil {
			return err
		}
		if previousHook != nil {
			if err := previousHook(hookCtx, preparation); err != nil {
				return err
			}
		}
		if err := request.PauseOAuthAuthorization(); err != nil {
			return err
		}
		mutationPaused = true
		return nil
	}
	authorization, authorizationErr := flow.Authorize(
		ctx,
		origin.CanonicalOrigin,
		openCloudOAuthBrowserForSetup,
	)
	var resumeErr error
	if mutationPaused {
		resumeErr = request.ResumeOAuthAuthorization()
	}
	if resumeErr != nil {
		stateErr := fmt.Errorf(
			"server configuration changed while waiting for Home Assistant authorization; the authorization code was not exchanged: %w",
			resumeErr,
		)
		if authorizationErr != nil {
			return "", OAuthSecretEnvelope{}, errors.Join(
				authorizationErr,
				stateErr,
			)
		}
		return "", OAuthSecretEnvelope{}, stateErr
	}
	if authorizationErr != nil {
		return "", OAuthSecretEnvelope{}, authorizationErr
	}
	if err := PreflightOAuthSecretStore(
		ctx,
		store,
		SecretStoreAllowUI,
	); err != nil {
		return "", OAuthSecretEnvelope{}, fmt.Errorf(
			"re-open OAuth secure storage after browser authorization: %w",
			err,
		)
	}
	oauth, err := NewHAOAuthClient(
		origin.CanonicalOrigin,
		cloudHTTPClientForCLI,
	)
	if err != nil {
		return "", OAuthSecretEnvelope{}, err
	}
	token, err := exchangeCloudAuthorizationCodeForSetup(
		ctx,
		oauth,
		authorization,
	)
	if err != nil {
		return "", OAuthSecretEnvelope{}, err
	}
	envelope, err = store.CreatePending(
		ctx,
		OAuthSecretEnvelope{
			Generation:      generation,
			CanonicalOrigin: origin.CanonicalOrigin,
			ClientID:        authorization.ClientID,
			RefreshToken:    token.RefreshToken,
		},
		SecretStoreForbidUI,
	)
	if err != nil {
		return "", OAuthSecretEnvelope{}, cleanupUnstoredCloudAuthorization(
			ctx,
			oauth,
			store,
			generation,
			request.ClearPendingAuthorization,
			token.RefreshToken,
			authorization.ClientID,
			err,
		)
	}
	return token.AccessToken, envelope, nil
}

func cleanupUnstoredCloudAuthorization(
	parent context.Context,
	oauth *HAOAuthClient,
	store OAuthSecretStore,
	generation string,
	clearPendingMetadata func(string) error,
	refreshToken, clientID string,
	storeErr error,
) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(parent),
		15*time.Second,
	)
	defer cancel()
	cleanupErr := revokeAndVerifyCloudAuthorizationWithClient(
		cleanupCtx,
		oauth,
		refreshToken,
		clientID,
	)
	if cleanupErr == nil &&
		IsCloudErrorCode(storeErr, CloudErrSecretOutcomeUnknown) {
		cleanupErr = cleanupAmbiguousPendingAuthorization(
			cleanupCtx,
			store,
			generation,
			refreshToken,
			clientID,
			clearPendingMetadata,
		)
	}
	if cleanupErr == nil {
		return storeErr
	}
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: "the new authorization could not be verified as revoked; " +
			"revoke its HA NOVA session in Home Assistant before retrying",
		Cause: errors.Join(storeErr, cleanupErr),
	}
}

func cleanupAmbiguousPendingAuthorization(
	ctx context.Context,
	store OAuthSecretStore,
	generation string,
	refreshToken string,
	clientID string,
	clearPendingMetadata func(string) error,
) error {
	if store == nil || generation == "" ||
		refreshToken == "" || clientID == "" ||
		clearPendingMetadata == nil {
		return errors.New(
			"pending Cloud authorization cleanup is unavailable",
		)
	}
	exact, err := exactOAuthCleanupStoreFor(store)
	if err != nil {
		return err
	}
	if err := exact.DeletePendingGrantExact(
		ctx,
		generation,
		refreshToken,
		clientID,
		SecretStoreForbidUI,
	); err != nil {
		return err
	}
	return clearPendingMetadata(generation)
}

func resumableAuthorizationNeedsCleanup(
	cfg runtimeConfig,
	generation string,
) bool {
	return generation != "" &&
		cfg.Cloud != nil &&
		cfg.Cloud.State == cloudStateAuthorizing &&
		cfg.Cloud.Pending != nil &&
		cfg.Cloud.Pending.CredentialGeneration == generation
}
