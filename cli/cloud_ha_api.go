package main

import (
	"context"
	"regexp"
	"strings"
	"time"
)

const haNOVAIngressUIEntry = "/home"

type HACurrentUser struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsOwner bool   `json:"is_owner"`
	IsAdmin bool   `json:"is_admin"`
}

type HARefreshTokenMetadata struct {
	ID         string     `json:"id"`
	ClientID   string     `json:"client_id"`
	IsCurrent  bool       `json:"is_current"`
	Type       string     `json:"type"`
	CreatedAt  *time.Time `json:"created_at"`
	ExpiresAt  *time.Time `json:"expire_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type HACloudCertificate struct {
	CommonName       string   `json:"common_name"`
	ExpireDate       string   `json:"expire_date"`
	Fingerprint      string   `json:"fingerprint"`
	AlternativeNames []string `json:"alternative_names"`
}

type HACloudStatus struct {
	LoggedIn                bool                `json:"logged_in"`
	ActiveSubscription      bool                `json:"active_subscription"`
	RemoteConnected         bool                `json:"remote_connected"`
	RemoteDomain            string              `json:"remote_domain"`
	RemoteCertificateStatus string              `json:"remote_certificate_status"`
	RemoteCertificate       *HACloudCertificate `json:"remote_certificate"`
	Prefs                   struct {
		RemoteEnabled bool `json:"remote_enabled"`
	} `json:"prefs"`
}

type HAAddonInfo struct {
	Slug         string `json:"slug"`
	State        string `json:"state"`
	Version      string `json:"version"`
	Ingress      bool   `json:"ingress"`
	IngressEntry string `json:"ingress_entry"`
	IngressURL   string `json:"ingress_url"`
}

type HAIngressSession struct {
	Session string `json:"session"`
}

var supervisorIngressEntryPattern = regexp.MustCompile(`^/api/hassio_ingress/[A-Za-z0-9_-]{32,256}$`)
var supervisorIngressSessionPattern = regexp.MustCompile(`^[a-f0-9]{128}$`)

func (c *HAWebSocketClient) CurrentUser(ctx context.Context) (HACurrentUser, error) {
	var user HACurrentUser
	if err := c.Call(ctx, "auth/current_user", nil, &user); err != nil {
		return HACurrentUser{}, err
	}
	if !validIdentifier(user.ID, 256) ||
		(user.Name != "" && !validIdentifier(user.Name, 1024)) {
		return HACurrentUser{}, newCloudError(CloudErrHAProtocol, "validate Home Assistant current user", nil)
	}
	return user, nil
}

func (c *HAWebSocketClient) RefreshTokens(ctx context.Context) ([]HARefreshTokenMetadata, error) {
	var tokens []HARefreshTokenMetadata
	if err := c.Call(ctx, "auth/refresh_tokens", nil, &tokens); err != nil {
		return nil, err
	}
	if len(tokens) > 10000 {
		return nil, newCloudError(CloudErrHAProtocol, "validate Home Assistant refresh tokens", nil)
	}
	return tokens, nil
}

func VerifyCurrentOAuthRefreshToken(
	tokens []HARefreshTokenMetadata,
	expectedClientID string,
	now time.Time,
) (HARefreshTokenMetadata, error) {
	if err := ValidateOAuthLoopbackClientID(expectedClientID); err != nil {
		return HARefreshTokenMetadata{}, err
	}
	var current *HARefreshTokenMetadata
	for index := range tokens {
		if !tokens[index].IsCurrent {
			continue
		}
		if current != nil {
			return HARefreshTokenMetadata{}, newCloudError(CloudErrHAProtocol, "verify current OAuth refresh token", nil)
		}
		current = &tokens[index]
	}
	if current == nil || current.ClientID != expectedClientID ||
		current.Type != "normal" || !validIdentifier(current.ID, 256) ||
		current.ExpiresAt == nil || !current.ExpiresAt.After(now.UTC()) {
		return HARefreshTokenMetadata{}, newCloudError(CloudErrOAuthInvalidGrant, "verify current OAuth refresh token", nil)
	}
	return *current, nil
}

func (c *HAWebSocketClient) CloudStatus(ctx context.Context) (HACloudStatus, error) {
	var status HACloudStatus
	if err := c.Call(ctx, "cloud/status", nil, &status); err != nil {
		return HACloudStatus{}, err
	}
	return status, nil
}

func (status HACloudStatus) ValidateForOrigin(origin CloudOrigin) error {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(status.RemoteDomain), "."))
	if !status.LoggedIn {
		return newCloudError(
			CloudErrUnauthorized,
			"validate Home Assistant Cloud sign-in",
			nil,
		)
	}
	if !status.ActiveSubscription {
		return newCloudError(
			CloudErrSubscriptionInactive,
			"validate Home Assistant Cloud subscription",
			nil,
		)
	}
	if !status.Prefs.RemoteEnabled ||
		!status.RemoteConnected || status.RemoteCertificateStatus != "ready" ||
		domain != origin.CanonicalHost || status.RemoteCertificate == nil {
		return newCloudError(CloudErrCloudNotReady, "validate Home Assistant Cloud status", nil)
	}
	names := make(map[string]struct{}, len(status.RemoteCertificate.AlternativeNames)+1)
	names[strings.ToLower(strings.TrimSuffix(status.RemoteCertificate.CommonName, "."))] = struct{}{}
	for _, name := range status.RemoteCertificate.AlternativeNames {
		names[strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))] = struct{}{}
	}
	if _, ok := names[origin.CanonicalHost]; !ok {
		return newCloudError(CloudErrCloudNotReady, "validate Home Assistant Cloud certificate", nil)
	}
	if origin.CustomDomain {
		if _, ok := names[origin.InputHost]; !ok {
			return newCloudError(CloudErrCloudNotReady, "validate custom Home Assistant Cloud certificate", nil)
		}
	}
	return nil
}

func (c *HAWebSocketClient) NOVAAppInfo(ctx context.Context) (HAAddonInfo, error) {
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		return HAAddonInfo{}, newCloudError(
			CloudErrInvalidInput,
			"select HA NOVA App",
			err,
		)
	}
	var info HAAddonInfo
	if err := c.Call(ctx, "supervisor/api", map[string]any{
		"endpoint": "/addons/" + appSlug + "/info",
		"method":   "get",
	}, &info); err != nil {
		return HAAddonInfo{}, err
	}
	if _, err := info.MachineIngressRoot(); err != nil {
		return HAAddonInfo{}, err
	}
	return info, nil
}

func (info HAAddonInfo) MachineIngressRoot() (string, error) {
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		return "", newCloudError(
			CloudErrInvalidInput,
			"select HA NOVA App",
			err,
		)
	}
	if info.Slug != appSlug || info.State != "started" ||
		!info.Ingress || !validIdentifier(info.Version, 128) ||
		!supervisorIngressEntryPattern.MatchString(info.IngressEntry) ||
		info.IngressURL != info.IngressEntry+haNOVAIngressUIEntry {
		return "", newCloudError(CloudErrAppNotReady, "validate HA NOVA App ingress", nil)
	}
	return info.IngressEntry, nil
}

func (c *HAWebSocketClient) CreateIngressSession(ctx context.Context) (HAIngressSession, error) {
	var session HAIngressSession
	if err := c.Call(ctx, "supervisor/api", map[string]any{
		"endpoint": "/ingress/session",
		"method":   "post",
	}, &session); err != nil {
		return HAIngressSession{}, err
	}
	if !supervisorIngressSessionPattern.MatchString(session.Session) {
		return HAIngressSession{}, newCloudError(CloudErrHAProtocol, "validate Supervisor ingress session", nil)
	}
	return session, nil
}
