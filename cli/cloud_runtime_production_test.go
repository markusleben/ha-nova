package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

type cloudRuntimeRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn cloudRuntimeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func cloudRuntimeSelection(transport http.RoundTripper, via relayVia) relayTransportSelection {
	return relayTransportSelection{
		BaseURL:    "https://relay.invalid",
		Client:     &http.Client{Transport: transport},
		Credential: validCredential(30),
		Via:        via,
	}
}

func stubProductionAutomaticResolvers(
	t *testing.T,
	local relayTransportSelection,
	localErr error,
) *int {
	t.Helper()
	oldLocal := resolveLocalRelayTransportForCLI
	oldCloud := resolveCloudRelayTransportForCLI
	cloudCalls := 0
	resolveLocalRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		return local, localErr
	}
	resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		cloudCalls++
		return relayTransportSelection{
			BaseURL:    "https://cloud.invalid",
			Client:     &http.Client{},
			Credential: validCredential(31),
			Via:        relayViaCloud,
		}, nil
	}
	t.Cleanup(func() {
		resolveLocalRelayTransportForCLI = oldLocal
		resolveCloudRelayTransportForCLI = oldCloud
	})
	return &cloudCalls
}

func TestProductionAutomaticRouteUsesLocalAfterAuthenticatedIdentityPreflight(t *testing.T) {
	const relayID = "relay-expected"
	requests := 0
	local := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Method != http.MethodGet || request.URL.Path != "/health" {
			t.Fatalf("preflight request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer "+localCredentialForRuntimeTest() {
			t.Fatalf("preflight Authorization = %q", request.Header.Get("Authorization"))
		}
		return cloudRuntimeResponse(http.StatusOK, `{"ok":true,"data":{"relay_instance_id":"`+relayID+`"}}`), nil
	}), relayViaLocal)
	local.Credential = localCredentialForRuntimeTest()
	cloudCalls := stubProductionAutomaticResolvers(t, local, nil)

	selected, err := resolveAutomaticRelayTransport(context.Background(), runtimeConfig{RelayInstanceID: relayID})
	if err != nil {
		t.Fatalf("resolveAutomaticRelayTransport: %v", err)
	}
	if selected.Via != relayViaLocal || requests != 1 || *cloudCalls != 0 {
		t.Fatalf("selection=%+v local requests=%d cloud calls=%d", selected, requests, *cloudCalls)
	}
}

func TestProductionAutomaticRouteFallsBackOnlyForConnectionFailures(t *testing.T) {
	for name, connectionErr := range map[string]error{
		"deadline":            context.DeadlineExceeded,
		"dns":                 &net.DNSError{Err: "no such host", Name: "relay.local"},
		"connection refused":  syscall.ECONNREFUSED,
		"host unreachable":    syscall.EHOSTUNREACH,
		"network unreachable": syscall.ENETUNREACH,
		"connection reset":    syscall.ECONNRESET,
	} {
		t.Run(name, func(t *testing.T) {
			local := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, connectionErr
			}), relayViaLocal)
			cloudCalls := stubProductionAutomaticResolvers(t, local, nil)

			selected, err := resolveAutomaticRelayTransport(
				context.Background(),
				runtimeConfig{
					RelayInstanceID: "relay-1",
					Cloud:           readyCloudForTransportTest(),
				},
			)
			if err != nil {
				t.Fatalf("resolveAutomaticRelayTransport: %v", err)
			}
			if selected.Via != relayViaCloud || *cloudCalls != 1 {
				t.Fatalf("selection=%+v cloud calls=%d", selected, *cloudCalls)
			}
		})
	}
}

func TestProductionAutomaticRouteNeverFallsBackForSecurityOrProtocolFailures(t *testing.T) {
	certificate := &x509.Certificate{DNSNames: []string{"different.invalid"}}
	errorCases := map[string]error{
		"pin mismatch":        errPinMismatch,
		"unknown authority":   x509.UnknownAuthorityError{Cert: certificate},
		"hostname mismatch":   x509.HostnameError{Certificate: certificate, Host: "relay.invalid"},
		"tls record":          tls.RecordHeaderError{},
		"context canceled":    context.Canceled,
		"generic I/O failure": errors.New("unexpected local transport failure"),
	}
	for name, transportErr := range errorCases {
		t.Run(name, func(t *testing.T) {
			local := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, transportErr
			}), relayViaLocal)
			cloudCalls := stubProductionAutomaticResolvers(t, local, nil)

			_, err := resolveAutomaticRelayTransport(
				context.Background(),
				runtimeConfig{
					RelayInstanceID: "relay-1",
					RelayBaseURL:    "http://cabin.local:8791",
				},
			)
			if err == nil {
				t.Fatal("security/protocol failure was accepted")
			}
			if *cloudCalls != 0 {
				t.Fatalf("Cloud fallback called %d time(s) for %v", *cloudCalls, transportErr)
			}
		})
	}
}

func TestProductionAutomaticRouteNeverFallsBackForHTTPOrEnvelopeFailures(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"unauthorized":      {status: http.StatusUnauthorized, body: `{}`},
		"forbidden":         {status: http.StatusForbidden, body: `{}`},
		"redirect":          {status: http.StatusTemporaryRedirect, body: ``},
		"server status":     {status: http.StatusServiceUnavailable, body: `{}`},
		"malformed JSON":    {status: http.StatusOK, body: `{`},
		"trailing JSON":     {status: http.StatusOK, body: `{"ok":true,"data":{"relay_instance_id":"relay-1"}} {}`},
		"unknown field":     {status: http.StatusOK, body: `{"ok":true,"data":{"relay_instance_id":"relay-1"},"extra":true}`},
		"negative envelope": {status: http.StatusOK, body: `{"ok":false,"data":{"relay_instance_id":"relay-1"}}`},
		"identity mismatch": {status: http.StatusOK, body: `{"ok":true,"data":{"relay_instance_id":"relay-other"}}`},
		"missing identity":  {status: http.StatusOK, body: `{"ok":true,"data":{}}`},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			resetServerProfileSelection(t)
			setActiveServerProfile("cabin")
			local := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return cloudRuntimeResponse(testCase.status, testCase.body), nil
			}), relayViaLocal)
			cloudCalls := stubProductionAutomaticResolvers(t, local, nil)

			_, err := resolveAutomaticRelayTransport(
				context.Background(),
				runtimeConfig{
					RelayInstanceID: "relay-1",
					RelayBaseURL:    "http://cabin.local:8791",
				},
			)
			if err == nil {
				t.Fatal("HTTP/envelope failure was accepted")
			}
			if (testCase.status == http.StatusUnauthorized ||
				testCase.status == http.StatusForbidden) &&
				!strings.Contains(
					err.Error(),
					`ha-nova pair --server cabin --relay-url "http://cabin.local:8791"`,
				) {
				t.Fatalf(
					"named-profile recovery error = %v",
					err,
				)
			}
			if *cloudCalls != 0 {
				t.Fatalf("Cloud fallback called %d time(s)", *cloudCalls)
			}
		})
	}
}

func TestProductionAutomaticRouteDoesNotMaskLocalResolverFailure(t *testing.T) {
	localErr := errors.New("local credential unavailable")
	cloudCalls := stubProductionAutomaticResolvers(t, relayTransportSelection{}, localErr)

	_, err := resolveAutomaticRelayTransport(context.Background(), runtimeConfig{RelayInstanceID: "relay-1"})
	if !errors.Is(err, localErr) {
		t.Fatalf("error = %v", err)
	}
	if *cloudCalls != 0 {
		t.Fatalf("Cloud fallback called %d time(s)", *cloudCalls)
	}
}

func TestRelayRoutePolicyNeverCrossesStrictTransportBoundary(t *testing.T) {
	oldLocal := resolveLocalRelayTransportForCLI
	oldCloud := resolveCloudRelayTransportForCLI
	oldAutomatic := resolveAutomaticRelayTransportForCLI
	t.Cleanup(func() {
		resolveLocalRelayTransportForCLI = oldLocal
		resolveCloudRelayTransportForCLI = oldCloud
		resolveAutomaticRelayTransportForCLI = oldAutomatic
	})

	sentinel := errors.New("selected resolver stopped")
	for name, testCase := range map[string]struct {
		policy      routePolicy
		override    relayVia
		overrideSet bool
		want        relayVia
	}{
		"local policy":            {policy: routePolicyLocal, want: relayViaLocal},
		"cloud policy":            {policy: routePolicyCloud, want: relayViaCloud},
		"automatic policy":        {policy: routePolicyAutomatic, want: "automatic"},
		"explicit local override": {policy: routePolicyCloud, override: relayViaLocal, overrideSet: true, want: relayViaLocal},
		"explicit cloud override": {policy: routePolicyLocal, override: relayViaCloud, overrideSet: true, want: relayViaCloud},
	} {
		t.Run(name, func(t *testing.T) {
			calls := map[relayVia]int{}
			resolveLocalRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
				calls[relayViaLocal]++
				return relayTransportSelection{}, sentinel
			}
			resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
				calls[relayViaCloud]++
				return relayTransportSelection{}, sentinel
			}
			resolveAutomaticRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
				calls["automatic"]++
				return relayTransportSelection{}, sentinel
			}
			cfg := runtimeConfig{
				RoutePolicy: testCase.policy,
				Cloud:       readyCloudForTransportTest(),
			}
			_, err := selectRelayTransport(context.Background(), cfg, testCase.override, testCase.overrideSet)
			if !errors.Is(err, sentinel) {
				t.Fatalf("error = %v", err)
			}
			if calls[testCase.want] != 1 ||
				calls[relayViaLocal]+calls[relayViaCloud]+calls["automatic"] != 1 {
				t.Fatalf("resolver calls = %v, want only %q", calls, testCase.want)
			}
		})
	}
}

func TestPureLocalNetworkFailureClassificationIsNarrow(t *testing.T) {
	for name, testCase := range map[string]struct {
		err  error
		want bool
	}{
		"wrapped refused": {
			err:  &url.Error{Op: "Get", URL: "https://relay.invalid", Err: syscall.ECONNREFUSED},
			want: true,
		},
		"wrapped DNS": {
			err:  &url.Error{Op: "Get", URL: "https://relay.invalid", Err: &net.DNSError{Err: "not found"}},
			want: true,
		},
		"pin":      {err: errPinMismatch, want: false},
		"canceled": {err: context.Canceled, want: false},
		"nil":      {err: nil, want: false},
		"cloud status": {
			err:  newCloudHTTPError(CloudErrUnauthorized, "preflight local Relay", http.StatusUnauthorized, false),
			want: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := isPureLocalNetworkFailure(testCase.err); got != testCase.want {
				t.Fatalf("isPureLocalNetworkFailure(%T) = %v, want %v", testCase.err, got, testCase.want)
			}
		})
	}
}

func TestCloudConfigSecretBindingClassifiesIdentityAndGenerationMismatch(t *testing.T) {
	const generation = "0123456789abcdef0123456789abcdef"
	cfg := runtimeConfig{ProfileID: "profile-1", RelayInstanceID: "relay-1"}
	metadata := cloudMetadataForTest(generation)
	envelope := OAuthSecretEnvelope{
		ProfileID:       cfg.ProfileID,
		CanonicalOrigin: metadata.CanonicalOrigin,
		ClientID:        metadata.OAuthClientID,
		Generation:      metadata.CredentialGeneration,
		HAUserID:        metadata.HAUserID,
		RelayInstanceID: cfg.RelayInstanceID,
	}
	if err := matchCloudConfigToSecret(cfg, metadata, envelope); err != nil {
		t.Fatalf("valid binding: %v", err)
	}

	for name, mutation := range map[string]struct {
		change func(*runtimeConfig, *cloudConnectionMetadata, *OAuthSecretEnvelope)
		code   CloudErrorCode
	}{
		"profile": {
			change: func(_ *runtimeConfig, _ *cloudConnectionMetadata, secret *OAuthSecretEnvelope) {
				secret.ProfileID = "profile-other"
			},
			code: CloudErrIdentityMismatch,
		},
		"canonical origin": {
			change: func(_ *runtimeConfig, _ *cloudConnectionMetadata, secret *OAuthSecretEnvelope) {
				secret.CanonicalOrigin = "https://other.ui.nabu.casa"
			},
			code: CloudErrIdentityMismatch,
		},
		"Home Assistant user": {
			change: func(_ *runtimeConfig, _ *cloudConnectionMetadata, secret *OAuthSecretEnvelope) {
				secret.HAUserID = "user-other"
			},
			code: CloudErrIdentityMismatch,
		},
		"Relay instance": {
			change: func(_ *runtimeConfig, _ *cloudConnectionMetadata, secret *OAuthSecretEnvelope) {
				secret.RelayInstanceID = "relay-other"
			},
			code: CloudErrRelayInstance,
		},
		"generation": {
			change: func(_ *runtimeConfig, _ *cloudConnectionMetadata, secret *OAuthSecretEnvelope) {
				secret.Generation = strings.Repeat("f", 32)
			},
			code: CloudErrSecretConflict,
		},
		"OAuth client": {
			change: func(_ *runtimeConfig, _ *cloudConnectionMetadata, secret *OAuthSecretEnvelope) {
				secret.ClientID = "http://127.0.0.1:49153/ha-nova"
			},
			code: CloudErrSecretConflict,
		},
	} {
		t.Run(name, func(t *testing.T) {
			testCfg, testMetadata, testEnvelope := cfg, metadata, envelope
			mutation.change(&testCfg, &testMetadata, &testEnvelope)
			err := matchCloudConfigToSecret(testCfg, testMetadata, testEnvelope)
			if !IsCloudErrorCode(err, mutation.code) {
				t.Fatalf("error = %v, want %s", err, mutation.code)
			}
		})
	}
}

func TestCloudRuntimeRejectsUnsafeContextBeforeSecureStorage(t *testing.T) {
	metadata := cloudMetadataForTest(
		"0123456789abcdef0123456789abcdef",
	)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	originalContext := cloudRuntimeSecureStorageContextAvailable
	originalStore := newCloudSecretStoreForCLI
	cloudRuntimeSecureStorageContextAvailable = func() bool { return false }
	storeCalls := 0
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) {
		storeCalls++
		return nil, errors.New("must not access native storage")
	}
	t.Cleanup(func() {
		cloudRuntimeSecureStorageContextAvailable = originalContext
		newCloudSecretStoreForCLI = originalStore
	})

	_, err := resolveCloudRelayTransport(context.Background(), cfg)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
		t.Fatalf("unsafe runtime error = %v", err)
	}
	if storeCalls != 0 {
		t.Fatalf("native store calls = %d", storeCalls)
	}
}

func cloudRuntimeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func localCredentialForRuntimeTest() string {
	return validCredential(30)
}
