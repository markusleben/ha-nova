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

	user, err := socket.CurrentUser(ctx)
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	if envelope.HAUserID != "" && envelope.HAUserID != user.ID {
		return cloudVerifiedSession{}, newCloudError(
			CloudErrIdentityMismatch,
			"verify Home Assistant Cloud user",
			nil,
		)
	}
	tokens, err := socket.RefreshTokens(ctx)
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	refreshMetadata, err := VerifyCurrentOAuthRefreshToken(
		tokens,
		envelope.ClientID,
		time.Now(),
	)
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	if envelope.RefreshTokenID != "" &&
		envelope.RefreshTokenID != refreshMetadata.ID {
		return cloudVerifiedSession{}, newCloudError(
			CloudErrOAuthInvalidGrant,
			"verify Home Assistant Cloud refresh token identity",
			nil,
		)
	}
	status, err := socket.CloudStatus(ctx)
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	if err := status.ValidateForOrigin(origin); err != nil {
		return cloudVerifiedSession{}, err
	}
	app, err := socket.NOVAAppInfo(ctx)
	if err != nil {
		return cloudVerifiedSession{}, err
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
			"validate saved Home Assistant Cloud origin",
			nil,
		)
	}
	return origin, nil
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
