package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDiscoverCloudFromLocalRelayFindsOriginAndInstanceWithoutInput(t *testing.T) {
	const (
		credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
		domain     = "example123.ui.nabu.casa"
		instanceID = "relay-instance-123"
	)
	relay := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+credential {
			t.Fatal("local discovery did not authenticate with the device credential")
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/health":
			_, _ = response.Write([]byte(`{"ok":true,"data":{"relay_instance_id":"` + instanceID + `"}}`))
		case "/ws":
			_, _ = response.Write([]byte(`{
				"ok":true,
				"data":{
					"logged_in":true,
					"active_subscription":true,
					"remote_connected":true,
					"remote_domain":"` + domain + `",
					"remote_certificate_status":"ready",
					"remote_certificate":{
						"common_name":"` + domain + `",
						"alternative_names":["` + domain + `"]
					},
					"prefs":{"remote_enabled":true}
				}
			}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer relay.Close()

	restore := replaceLocalCloudDiscoveryTransport(t, relay.URL, relay.Client(), credential)
	defer restore()

	discovery, err := discoverCloudFromLocalRelay(context.Background(), runtimeConfig{})
	if err != nil {
		t.Fatalf("discoverCloudFromLocalRelay() error = %v", err)
	}
	if discovery.Origin.CanonicalOrigin != "https://"+domain {
		t.Fatalf("canonical origin = %q", discovery.Origin.CanonicalOrigin)
	}
	if discovery.RelayInstanceID != instanceID {
		t.Fatalf("relay instance id = %q", discovery.RelayInstanceID)
	}
}

func TestDiscoverCloudFromLocalRelayRejectsRedirectWithoutFollowing(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer target.Close()
	relay := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", target.URL)
		response.WriteHeader(http.StatusFound)
	}))
	defer relay.Close()

	restore := replaceLocalCloudDiscoveryTransport(t, relay.URL, relay.Client(), "device-secret")
	defer restore()

	_, err := discoverCloudFromLocalRelay(context.Background(), runtimeConfig{})
	if !IsCloudErrorCode(err, CloudErrRedirectRejected) {
		t.Fatalf("error = %v, want REDIRECT_REJECTED", err)
	}
	if redirected.Load() {
		t.Fatal("credential-bearing discovery redirect was followed")
	}
}

func TestDiscoverCloudFromLocalRelayRequiresRelayInstanceIdentity(t *testing.T) {
	relay := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"ok":true,"data":{"status":"ok"}}`))
	}))
	defer relay.Close()

	restore := replaceLocalCloudDiscoveryTransport(t, relay.URL, relay.Client(), "device-secret")
	defer restore()

	_, err := discoverCloudFromLocalRelay(context.Background(), runtimeConfig{})
	if !IsCloudErrorCode(err, CloudErrAppNotReady) {
		t.Fatalf("error = %v, want APP_NOT_READY", err)
	}
}

func replaceLocalCloudDiscoveryTransport(
	t *testing.T,
	baseURL string,
	client *http.Client,
	credential string,
) func() {
	t.Helper()
	previous := resolveLocalRelayTransportForCLI
	resolveLocalRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		return relayTransportSelection{
			BaseURL:    baseURL,
			Client:     client,
			Credential: credential,
			DeviceMode: true,
			Via:        relayViaLocal,
		}, nil
	}
	return func() {
		resolveLocalRelayTransportForCLI = previous
	}
}
