package main

import (
	"context"
	"net/http"
	"time"
)

type cloudVerifiedSession struct {
	Envelope OAuthSecretEnvelope
	User     HACurrentUser
	Status   HACloudStatus
	App      HAAddonInfo
	Relay    CloudRelayInfo
	Ingress  *CloudIngressClient
}

func verifyCloudAccess(
	ctx context.Context,
	envelope OAuthSecretEnvelope,
	accessToken string,
	origin CloudOrigin,
	client *http.Client,
) (cloudVerifiedSession, error) {
	if origin.CanonicalOrigin != envelope.CanonicalOrigin {
		return cloudVerifiedSession{}, newCloudError(
			CloudErrIdentityMismatch,
			"verify Cloud authorization origin",
			nil,
		)
	}
	socket, err := DialHAWebSocket(
		ctx,
		envelope.CanonicalOrigin,
		accessToken,
		HAWebSocketClientOptions{HTTPClient: client},
	)
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	defer socket.Close()

	// The four reads share one round trip and are checked in this order, each
	// as soon as it and every earlier one have answered, all before the
	// Ingress session is created.
	var user HACurrentUser
	var tokens []HARefreshTokenMetadata
	var refreshMetadata HARefreshTokenMetadata
	var status HACloudStatus
	var app HAAddonInfo
	calls := []haCall{
		{"auth/current_user", nil, &user, func(err error) error {
			if user, err = checkCurrentUser(user, err); err == nil && envelope.HAUserID != "" && envelope.HAUserID != user.ID {
				return newCloudError(CloudErrIdentityMismatch, "verify Home Assistant Link user", nil)
			}
			return err
		}},
		{"auth/refresh_tokens", nil, &tokens, func(err error) error {
			if tokens, err = checkRefreshTokens(tokens, err); err != nil {
				return err
			}
			if refreshMetadata, err = VerifyCurrentOAuthRefreshToken(tokens, envelope.ClientID, time.Now()); err != nil {
				return err
			}
			if envelope.RefreshTokenID != "" && envelope.RefreshTokenID != refreshMetadata.ID {
				return newCloudError(CloudErrOAuthInvalidGrant, "verify Home Assistant Link refresh token identity", nil)
			}
			return nil
		}},
		{"cloud/status", nil, &status, func(err error) error {
			if err != nil {
				return err
			}
			return status.ValidateForOrigin(origin)
		}},
	}
	appCall, appErr := novaAppInfoCall(&app)
	if appErr == nil {
		appCall.check = func(err error) error {
			app, err = checkNOVAAppInfo(app, err)
			return err
		}
		calls = append(calls, appCall)
	}
	for _, err := range socket.CallAll(ctx, calls) {
		if err != nil {
			return cloudVerifiedSession{}, err
		}
	}
	if appErr != nil {
		return cloudVerifiedSession{}, appErr
	}
	ingressRoot, err := app.MachineIngressRoot()
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	ingressSession, err := socket.CreateIngressSession(ctx)
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	ingress, err := NewCloudIngressClient(
		envelope.CanonicalOrigin,
		ingressRoot,
		ingressSession.Session,
		client,
	)
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	relay, err := ingress.RelayInfo(ctx)
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	if envelope.RelayInstanceID != "" &&
		envelope.RelayInstanceID != relay.RelayInstanceID {
		return cloudVerifiedSession{}, newCloudError(
			CloudErrRelayInstance,
			"verify NOVA Relay instance identity",
			nil,
		)
	}

	envelope.HAUserID = user.ID
	envelope.RefreshTokenID = refreshMetadata.ID
	envelope.RefreshTokenExpiresAt = refreshMetadata.ExpiresAt
	envelope.RelayInstanceID = relay.RelayInstanceID
	return cloudVerifiedSession{
		Envelope: envelope,
		User:     user,
		Status:   status,
		App:      app,
		Relay:    relay,
		Ingress:  ingress,
	}, nil
}

func cloudOriginFromMetadata(metadata cloudConnectionMetadata) (CloudOrigin, error) {
	canonical, err := ParseCanonicalNabuOrigin(metadata.CanonicalOrigin)
	if err != nil {
		return CloudOrigin{}, err
	}
	input, err := parseStrictCloudOrigin(metadata.Origin)
	if err != nil {
		return CloudOrigin{}, err
	}
	origin := CloudOrigin{
		InputOrigin:     input.String(),
		InputHost:       input.Hostname(),
		CanonicalOrigin: canonical.String(),
		CanonicalHost:   canonical.Hostname(),
		CustomDomain:    input.Hostname() != canonical.Hostname(),
	}
	if !origin.CustomDomain && origin.InputOrigin != origin.CanonicalOrigin {
		return CloudOrigin{}, newCloudError(
			CloudErrInvalidInput,
			"validate saved Home Assistant Link origin",
			nil,
		)
	}
	return origin, nil
}
