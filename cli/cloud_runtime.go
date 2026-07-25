package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const automaticLocalPreflightTimeout = 2 * time.Second

var newCloudSecretStoreForCLI = func(profileID string) (OAuthSecretStore, error) {
	return NewSystemOAuthSecretStore(profileID)
}

var cloudHTTPClientForCLI = cloudNoRedirectHTTPClient(nil, 2*time.Minute)
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
	if cfg.Cloud == nil || cfg.Cloud.Current == nil {
		return relayTransportSelection{}, cloudNotConfiguredProblem()
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
	token, err := oauth.Refresh(ctx, envelope.RefreshToken, envelope.ClientID)
	if err != nil {
		return relayTransportSelection{}, err
	}
	verified, err := verifyCloudAccess(
		ctx,
		envelope,
		token.AccessToken,
		origin,
		cloudHTTPClientForCLI,
	)
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
	if err := automaticLocalPreflight(ctx, cfg, local); err == nil {
		return local, nil
	} else if !isPureLocalNetworkFailure(err) {
		return relayTransportSelection{}, &localRelayPreflightError{cause: err}
	}
	return resolveCloudRelayTransportForCLI(ctx, cfg)
}

func automaticLocalPreflight(
	parent context.Context,
	cfg runtimeConfig,
	local relayTransportSelection,
) error {
	ctx, cancel := context.WithTimeout(parent, automaticLocalPreflightTimeout)
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
	client := cloudNoRedirectHTTPClient(local.Client, automaticLocalPreflightTimeout)
	client.Timeout = automaticLocalPreflightTimeout
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
