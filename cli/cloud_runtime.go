package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const automaticLocalPreflightTimeout = 2 * time.Second

var newCloudSecretStoreForCLI = func(profileID string) (OAuthSecretStore, error) {
	return NewSystemOAuthSecretStore(profileID)
}

var cloudHTTPClientForCLI = cloudNoRedirectHTTPClient(&http.Client{Transport: newLinkHTTPTransport()}, 2*time.Minute)
var cloudRuntimeSecureStorageContextAvailable = func() bool {
	return platformNativeSecretPromptContextAvailable()
}

func resolveCloudRelayTransport(
	ctx context.Context,
	cfg runtimeConfig,
) (relayTransportSelection, error) {
	if err := requireCloudRemoteFeature(); err != nil {
		return relayTransportSelection{}, err
	}
	if err := requireFunctionalCloudAccess(cfg); err != nil {
		return relayTransportSelection{}, err
	}
	metadata := *cfg.Cloud.Current
	if err := validateCurrentCloudConnectionMetadata(metadata); err != nil {
		return relayTransportSelection{}, newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud configuration",
			err,
		)
	}
	if cfg.ProfileID == "" || cfg.RelayInstanceID == "" {
		return relayTransportSelection{}, newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud profile identity",
			nil,
		)
	}
	if !cloudRuntimeSecureStorageContextAvailable() {
		return relayTransportSelection{}, newCloudError(
			CloudErrSecretUIForbidden,
			"access Cloud authorization from this runtime context",
			nil,
		)
	}
	store, exists := cloudSecretStoreForOperation(ctx, cfg.ProfileID)
	if !exists {
		var err error
		store, err = newCloudSecretStoreForCLI(cfg.ProfileID)
		if err != nil {
			return relayTransportSelection{}, err
		}
	}
	envelope, exists, err := store.LoadCurrent(ctx, SecretStoreForbidUI)
	if err != nil {
		return relayTransportSelection{}, err
	}
	if !exists {
		return relayTransportSelection{}, newCloudError(
			CloudErrSecretNotFound,
			"load Cloud authorization",
			nil,
		)
	}
	if err := matchCloudConfigToSecret(cfg, metadata, envelope); err != nil {
		return relayTransportSelection{}, err
	}
	origin, err := cloudOriginFromMetadata(metadata)
	if err != nil {
		return relayTransportSelection{}, err
	}
	oauth, err := NewHAOAuthClient(envelope.CanonicalOrigin, cloudHTTPClientForCLI)
	if err != nil {
		return relayTransportSelection{}, err
	}
	verified, err := loginToLink(ctx, oauth, envelope, origin)
	if err != nil && linkLoginRetryable(err) && linkRetryWait(ctx, linkLoginRetryMinLeft) {
		verified, err = loginToLink(ctx, oauth, envelope, origin) // R1: nothing functional was sent yet
	}
	if err != nil {
		return relayTransportSelection{}, err
	}
	if !verified.Relay.RemoteEnabled() {
		return relayTransportSelection{}, newCloudError(
			CloudErrAppNotReady,
			"verify Cloud Remote is enabled on NOVA Relay",
			nil,
		)
	}
	if verified.Relay.RelayInstanceID != cfg.RelayInstanceID ||
		verified.User.ID != metadata.HAUserID {
		return relayTransportSelection{}, newCloudError(
			CloudErrRelayInstance,
			"verify saved Cloud identity",
			nil,
		)
	}
	// Ordinary Relay/status/doctor calls stay read-only in native storage.
	// Updating verified metadata here would race a separately locked reconnect:
	// a stale reader could overwrite the newly promoted current generation after
	// its non-atomic load/compare. Setup and reconnect own all slot mutations.
	credential, exists, err := readDeviceCredentialWithPolicy(
		ctx,
		SecretStoreForbidUI,
	)
	if err != nil {
		return relayTransportSelection{}, err
	}
	if !exists {
		return relayTransportSelection{}, newCloudError(
			CloudErrDeviceRejected,
			"load Cloud device credential",
			nil,
		)
	}
	return newCloudRelayTransport(verified.Ingress, credential)
}

// loginToLink is one Link login within linkLoginAttemptTimeout: the OAuth
// refresh, then the WebSocket checks and the Ingress session.
func loginToLink(
	ctx context.Context,
	oauth *HAOAuthClient,
	envelope OAuthSecretEnvelope,
	origin CloudOrigin,
) (cloudVerifiedSession, error) {
	ctx, cancel := context.WithTimeout(ctx, linkLoginAttemptTimeout)
	defer cancel()
	token, err := oauth.Refresh(ctx, envelope.RefreshToken, envelope.ClientID)
	if err != nil {
		return cloudVerifiedSession{}, err
	}
	return verifyCloudAccess(ctx, envelope, token.AccessToken, origin, cloudHTTPClientForCLI)
}

func matchCloudConfigToSecret(
	cfg runtimeConfig,
	metadata cloudConnectionMetadata,
	envelope OAuthSecretEnvelope,
) error {
	if envelope.ProfileID != cfg.ProfileID ||
		envelope.CanonicalOrigin != metadata.CanonicalOrigin ||
		envelope.HAUserID != metadata.HAUserID {
		return newCloudError(
			CloudErrIdentityMismatch,
			"match Cloud authorization to profile",
			nil,
		)
	}
	if envelope.RelayInstanceID != cfg.RelayInstanceID {
		return newCloudError(
			CloudErrRelayInstance,
			"match Cloud authorization to Relay",
			nil,
		)
	}
	if envelope.Generation != metadata.CredentialGeneration ||
		envelope.ClientID != metadata.OAuthClientID {
		return newCloudError(
			CloudErrSecretConflict,
			"match Cloud authorization to profile",
			nil,
		)
	}
	return nil
}

func resolveAutomaticRelayTransport(
	ctx context.Context,
	cfg runtimeConfig,
) (relayTransportSelection, error) {
	local, err := resolveLocalRelayTransportForCLI(ctx, cfg)
	if err != nil {
		return relayTransportSelection{}, err
	}
	// A route remembered moments ago skips only the local preflight; Link
	// still verifies the Relay instance and the user on every login. While
	// Link is closed (cleanup), the local Relay is probed as before.
	remembered := linkRouteRemembered(cfg) && requireFunctionalCloudAccess(cfg) == nil
	unreachable := false
	if !remembered {
		useLocal, neverConnected, err := probeAutomaticLocalRelay(ctx, cfg, local)
		if err != nil {
			return relayTransportSelection{}, err
		}
		if useLocal {
			return local, nil
		}
		unreachable = neverConnected
	}
	if err := requireFunctionalCloudAccess(cfg); err != nil {
		return relayTransportSelection{}, err
	}
	selected, err := resolveCloudRelayTransportForCLI(ctx, cfg)
	if err == nil && unreachable {
		rememberLinkRoute(cfg)
	}
	if err != nil && remembered && ctx.Err() == nil {
		// Link failed on a remembered route: forget it and probe the local
		// Relay now, before any functional request exists.
		forgetLinkRoute()
		if useLocal, _, probeErr := probeAutomaticLocalRelay(ctx, cfg, local); probeErr != nil {
			return relayTransportSelection{}, probeErr
		} else if useLocal {
			return local, nil
		}
	}
	return selected, err
}

// probeAutomaticLocalRelay runs the authenticated local preflight. useLocal:
// the local Relay answered as this instance. unreachable: a full-length probe
// never obtained a connection, so Link may be remembered; a shortened probe,
// or one that connected and then timed out, proves nothing. err: a failure
// that never falls back (pin, TLS, authentication, protocol).
func probeAutomaticLocalRelay(ctx context.Context, cfg runtimeConfig, local relayTransportSelection) (useLocal, unreachable bool, err error) {
	var connected atomic.Bool
	probeCtx := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
	})
	if err := automaticLocalPreflight(probeCtx, cfg, local); err == nil {
		return true, false, nil
	} else if !isPureLocalNetworkFailure(err) {
		return false, false, &localRelayPreflightError{
			cause:    err,
			profile:  activeServerProfile(),
			relayURL: cfg.RelayBaseURL,
		}
	}
	fullProbe := relayConnectBudget(ctx, automaticLocalPreflightTimeout) == automaticLocalPreflightTimeout
	return false, fullProbe && !connected.Load(), nil
}

func automaticLocalPreflight(
	parent context.Context,
	cfg runtimeConfig,
	local relayTransportSelection,
) error {
	timeout := relayConnectBudget(parent, automaticLocalPreflightTimeout)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(local.BaseURL, "/")+"/health",
		nil,
	)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+local.Credential)
	request.Header.Set("Accept", "application/json")
	client := cloudNoRedirectHTTPClient(local.Client, timeout)
	client.Timeout = timeout
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if isHTTPRedirect(response.StatusCode) {
		return newCloudHTTPError(
			CloudErrRedirectRejected,
			"preflight local Relay",
			response.StatusCode,
			false,
		)
	}
	if response.StatusCode != http.StatusOK {
		code := CloudErrHAProtocol
		if response.StatusCode == http.StatusUnauthorized {
			code = CloudErrUnauthorized
		} else if response.StatusCode == http.StatusForbidden {
			code = CloudErrForbidden
		}
		return newCloudHTTPError(code, "preflight local Relay", response.StatusCode, false)
	}
	body, err := readCloudResponse(
		response.Body,
		cloudLocalDiscoveryMaxBytes,
		"read local Relay preflight",
	)
	if err != nil {
		return err
	}
	return validateCloudRelayHealthIdentity(body, cfg.RelayInstanceID)
}

func isPureLocalNetworkFailure(err error) bool {
	if err == nil || errors.Is(err, errPinMismatch) {
		return false
	}
	var certificateError x509.CertificateInvalidError
	var hostnameError x509.HostnameError
	var unknownAuthority x509.UnknownAuthorityError
	var verificationError *tls.CertificateVerificationError
	var recordHeaderError tls.RecordHeaderError
	if errors.As(err, &certificateError) ||
		errors.As(err, &hostnameError) ||
		errors.As(err, &unknownAuthority) ||
		errors.As(err, &verificationError) ||
		errors.As(err, &recordHeaderError) {
		return false
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return isPureLocalNetworkFailure(urlError.Err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return true
	}
	for _, code := range []error{
		syscall.ECONNREFUSED,
		syscall.EHOSTUNREACH,
		syscall.ENETUNREACH,
		syscall.ETIMEDOUT,
		syscall.ECONNRESET,
	} {
		if errors.Is(err, code) {
			return true
		}
	}
	return false
}

func init() {
	resolveCloudRelayTransportForCLI = resolveCloudRelayTransport
	resolveAutomaticRelayTransportForCLI = resolveAutomaticRelayTransport
}
