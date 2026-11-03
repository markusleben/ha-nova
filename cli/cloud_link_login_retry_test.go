package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

// R1: one Link login that failed on the network or on a server error goes
// once more; an authentication failure never does
// (docs/work/2026-11-03-link-route-timeouts-spec.md).
func TestLinkLoginRetriesOnceOnlyAfterANetworkOrServerFailure(t *testing.T) {
	shortLinkRetryPause(t)
	for name, testCase := range map[string]struct {
		first      func() (*http.Response, error)
		wantTokens int32
		wantCode   CloudErrorCode
	}{
		"network failure, then success": {
			first:      func() (*http.Response, error) { return nil, syscall.ECONNRESET },
			wantTokens: 2,
		},
		"server error, then success": {
			first: func() (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("busy"))}, nil
			},
			wantTokens: 2,
		},
		"rejected grant is final": {
			first: func() (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`)),
				}, nil
			},
			wantTokens: 1, wantCode: CloudErrOAuthInvalidGrant,
		},
	} {
		t.Run(name, func(t *testing.T) {
			setupServerCommandTest(t, `{"schema_version":1}`)
			store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())
			pending, err := store.CreatePending(context.Background(), productionCloudTestEnvelope(), SecretStoreAllowUI)
			if err != nil {
				t.Fatal(err)
			}
			current, err := store.PromotePending(context.Background(), pending.Generation, SecretStoreAllowUI)
			if err != nil {
				t.Fatal(err)
			}
			origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
			if err != nil {
				t.Fatal(err)
			}
			metadata := cloudMetadataFromEnvelope(origin, current)
			cfg := runtimeConfig{
				ProfileID:       "profile-1",
				RelayInstanceID: "relay-1",
				RoutePolicy:     routePolicyCloud,
				Cloud:           &cloudLifecycleMetadata{State: cloudStateReady, Current: &metadata},
			}
			if err := writeDeviceCredential(validCredential(72)); err != nil {
				t.Fatal(err)
			}
			server := newProductionCloudProtocolServer(t)
			defer server.Close()
			mapped := productionCloudMappedClient(t, server).Transport
			var tokens atomic.Int32
			oldStore, oldHTTP, oldContext := newCloudSecretStoreForCLI, cloudHTTPClientForCLI, cloudRuntimeSecureStorageContextAvailable
			newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) { return store, nil }
			cloudHTTPClientForCLI = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/auth/token" && tokens.Add(1) == 1 {
					return testCase.first()
				}
				return mapped.RoundTrip(request)
			})}
			cloudRuntimeSecureStorageContextAvailable = func() bool { return true }
			t.Cleanup(func() {
				newCloudSecretStoreForCLI, cloudHTTPClientForCLI, cloudRuntimeSecureStorageContextAvailable = oldStore, oldHTTP, oldContext
			})

			selected, err := resolveCloudRelayTransport(context.Background(), cfg)
			if tokens.Load() != testCase.wantTokens {
				t.Fatalf("OAuth refreshes = %d, want %d (err = %v)", tokens.Load(), testCase.wantTokens, err)
			}
			if testCase.wantCode != "" {
				if !IsCloudErrorCode(err, testCase.wantCode) {
					t.Fatalf("error = %v, want %s", err, testCase.wantCode)
				}
				return
			}
			if err != nil || selected.Via != relayViaCloud {
				t.Fatalf("selection = %+v, err = %v", selected, err)
			}
		})
	}
}
