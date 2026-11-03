package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
)

// The automatic route's remembered Link route
// (docs/work/2026-11-03-link-route-timeouts-spec.md).

func rememberLinkRouteInTemp(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache", "link-route.json")
	old := linkRouteMemoryFile
	linkRouteMemoryFile = func() string { return path }
	t.Cleanup(func() { linkRouteMemoryFile = old })
	return path
}

func automaticLinkTestConfig() runtimeConfig {
	return runtimeConfig{
		ProfileID:          "profile-1",
		RelayInstanceID:    "relay-1",
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		Cloud:              readyCloudForTransportTest(),
	}
}

// Local unreachable, then Link: the next automatic selection within two
// minutes goes straight to Link; Link still verifies on every login.
func TestAutomaticRouteRemembersUnreachableLocalRelayBriefly(t *testing.T) {
	path := rememberLinkRouteInTemp(t)
	probes := 0
	local := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		probes++
		return nil, syscall.ECONNREFUSED
	}), relayViaLocal)
	cloudCalls := stubProductionAutomaticResolvers(t, local, nil)
	cfg := automaticLinkTestConfig()

	for round := 1; round <= 2; round++ {
		selected, err := resolveAutomaticRelayTransport(context.Background(), cfg)
		if err != nil || selected.Via != relayViaCloud {
			t.Fatalf("round %d: selection = %+v, err = %v", round, selected, err)
		}
	}
	if probes != 1 || *cloudCalls != 2 {
		t.Fatalf("local preflights = %d (want 1), Link logins = %d (want 2)", probes, *cloudCalls)
	}

	other := cfg
	other.RelayInstanceID = "relay-other" // another App: probed again, never inherits the route
	if _, err := resolveAutomaticRelayTransport(context.Background(), other); err != nil || probes != 2 {
		t.Fatalf("another Relay instance: err = %v, local preflights = %d (want 2)", err, probes)
	}

	if err := os.WriteFile(path, []byte(`{"key":"profile-1 relay-1 https://192.168.1.5:8792 http://192.168.1.5:8791","until":"2099-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAutomaticRelayTransport(context.Background(), cfg); err != nil || probes != 3 {
		t.Fatalf("an end time beyond two minutes: err = %v, local preflights = %d (want 3)", err, probes)
	}
}

func TestAutomaticRouteRemembersNothingAfterASecurityFailure(t *testing.T) {
	path := rememberLinkRouteInTemp(t)
	local := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errPinMismatch
	}), relayViaLocal)
	cloudCalls := stubProductionAutomaticResolvers(t, local, nil)
	if _, err := resolveAutomaticRelayTransport(context.Background(), automaticLinkTestConfig()); err == nil {
		t.Fatal("a pin mismatch fell back")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) || *cloudCalls != 0 {
		t.Fatalf("remembered route after a pin mismatch: stat err = %v, Link logins = %d", err, *cloudCalls)
	}
}

// Link failing on a remembered route forgets it and probes the local Relay
// in the same command: a Relay that is back is used, one still gone leaves
// the Link error.
func TestAutomaticRouteProbesAgainWhenARememberedLinkFails(t *testing.T) {
	for name, localBack := range map[string]bool{"local back": true, "local still gone": false} {
		t.Run(name, func(t *testing.T) {
			path := rememberLinkRouteInTemp(t)
			reachable, probes := false, 0
			local := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
				probes++
				if !reachable {
					return nil, syscall.ECONNREFUSED
				}
				return cloudRuntimeResponse(http.StatusOK, `{"ok":true,"data":{"relay_instance_id":"relay-1"}}`), nil
			}), relayViaLocal)
			stubProductionAutomaticResolvers(t, local, nil)
			cfg := automaticLinkTestConfig()
			if _, err := resolveAutomaticRelayTransport(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
			reachable = localBack
			resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
				return relayTransportSelection{}, newCloudError(CloudErrNetwork, "send Cloud ingress request", nil)
			}
			selected, err := resolveAutomaticRelayTransport(context.Background(), cfg)
			if localBack != (err == nil && selected.Via == relayViaLocal) || !localBack && !IsCloudErrorCode(err, CloudErrNetwork) {
				t.Fatalf("selection = %+v, err = %v", selected, err)
			}
			if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) || probes != 2 {
				t.Fatalf("remembered route after Link failed: stat err = %v, local preflights = %d (want 2)", statErr, probes)
			}
		})
	}
}

// Only a full-length probe that never got a connection is remembered: a
// slow but reachable local Relay keeps its place.
func TestAutomaticRouteRemembersOnlyAProbeThatNeverConnected(t *testing.T) {
	for name, testCase := range map[string]struct {
		ctx   context.Context
		probe func(*http.Request) (*http.Response, error)
	}{
		"probe shortened by --connect-timeout": {
			ctx:   withRelayConnectBudget(context.Background(), 500*time.Millisecond),
			probe: func(*http.Request) (*http.Response, error) { return nil, syscall.ECONNREFUSED },
		},
		"connected, then no answer in time": {
			ctx: context.Background(),
			probe: func(request *http.Request) (*http.Response, error) {
				if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.GotConn != nil {
					trace.GotConn(httptrace.GotConnInfo{})
				}
				return nil, context.DeadlineExceeded
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := rememberLinkRouteInTemp(t)
			cloudCalls := stubProductionAutomaticResolvers(t, cloudRuntimeSelection(cloudRuntimeRoundTripFunc(testCase.probe), relayViaLocal), nil)
			selected, err := resolveAutomaticRelayTransport(testCase.ctx, automaticLinkTestConfig())
			if err != nil || selected.Via != relayViaCloud || *cloudCalls != 1 {
				t.Fatalf("selection = %+v, err = %v, Link logins = %d", selected, err, *cloudCalls)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("remembered Link after a probe that proves nothing: stat err = %v", err)
			}
		})
	}
}

// A command that ran out of its own time keeps the remembered route.
func TestAutomaticRouteKeepsTheRouteWhenTheCallerRanOutOfTime(t *testing.T) {
	path := rememberLinkRouteInTemp(t)
	probes := 0
	local := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		probes++
		return nil, syscall.ECONNREFUSED
	}), relayViaLocal)
	stubProductionAutomaticResolvers(t, local, nil)
	cfg := automaticLinkTestConfig()
	if _, err := resolveAutomaticRelayTransport(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	resolveCloudRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		cancel()
		return relayTransportSelection{}, newCloudError(CloudErrTimeout, "send Cloud ingress request", context.Canceled)
	}
	if _, err := resolveAutomaticRelayTransport(ctx, cfg); err == nil {
		t.Fatal("a Link login that ran out of time succeeded")
	}
	if _, err := os.Stat(path); err != nil || probes != 1 {
		t.Fatalf("route after the caller's deadline: stat err = %v, local preflights = %d (want 1)", err, probes)
	}
}

// While Link is closed for cleanup, a remembered route is ignored and the
// healthy local Relay is used, as before.
func TestAutomaticRouteProbesTheLocalRelayWhileLinkIsClosed(t *testing.T) {
	rememberLinkRouteInTemp(t)
	reachable := false
	local := cloudRuntimeSelection(cloudRuntimeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		if !reachable {
			return nil, syscall.ECONNREFUSED
		}
		return cloudRuntimeResponse(http.StatusOK, `{"ok":true,"data":{"relay_instance_id":"relay-1"}}`), nil
	}), relayViaLocal)
	cloudCalls := stubProductionAutomaticResolvers(t, local, nil)
	cfg := automaticLinkTestConfig()
	if _, err := resolveAutomaticRelayTransport(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	reachable = true
	cfg.Cloud.DeviceRevocationCompleted = &cloudDeviceRevocationCheckpoint{CurrentDeviceID: "revoked-device"}
	selected, err := resolveAutomaticRelayTransport(context.Background(), cfg)
	if err != nil || selected.Via != relayViaLocal || *cloudCalls != 1 {
		t.Fatalf("selection = %+v, err = %v, Link logins = %d (want 1)", selected, err, *cloudCalls)
	}
}

func TestUninstallRemovesTheRememberedLinkRoute(t *testing.T) {
	paths := runtimePaths{CacheDir: filepath.Join(t.TempDir(), "cache")}
	if !slices.Contains(managedCacheArtifactPaths(paths), filepath.Join(paths.CacheDir, linkRouteMemoryName)) {
		t.Fatal("uninstall leaves the remembered Link route behind")
	}
}
