package main

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The wizard prefers the secure v1 flow: on success it returns errSetupDevicePaired
// (no relay token to persist) after storing the device credential.
func TestSetupPairingPrefersSecureV1(t *testing.T) {
	origProbe, origPair := probePairingV1ForSetup, securePairForSetup
	defer func() { probePairingV1ForSetup, securePairForSetup = origProbe, origPair }()

	probePairingV1ForSetup = func(string) bool { return true }
	paired := false
	securePairForSetup = func(bootstrapURL, code string, cfg *runtimeConfig, save func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		if code != "473921" {
			t.Fatalf("unexpected code %q", code)
		}
		paired = true
		cfg.RelaySecureBaseURL = "https://relay:8792"
		cfg.RelaySpkiPin = "pin"
		return "device-abc", nil
	}

	reader := bufio.NewReader(strings.NewReader("\n473921\n"))
	cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791"}
	token, err := runSetupPairingFlow(reader, io.Discard, runtimePaths{}, cfg, false)
	if !errors.Is(err, errSetupDevicePaired) {
		t.Fatalf("expected errSetupDevicePaired, got token=%q err=%v", token, err)
	}
	if token != "" || !paired {
		t.Fatalf("device pairing should store no relay token; token=%q paired=%v", token, paired)
	}
}

// Without v1 support the wizard falls back to the legacy code exchange.
func TestSetupPairingFallsBackToLegacyExchange(t *testing.T) {
	origProbe, origExchange := probePairingV1ForSetup, exchangeRelayPairingCodeForSetup
	defer func() { probePairingV1ForSetup, exchangeRelayPairingCodeForSetup = origProbe, origExchange }()

	probePairingV1ForSetup = func(string) bool { return false }
	exchangeRelayPairingCodeForSetup = func(_ *http.Client, _, code string) (string, error) {
		return "legacy-token-for-" + code, nil
	}

	reader := bufio.NewReader(strings.NewReader("\n473921\n"))
	cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791"}
	token, err := runSetupPairingFlow(reader, io.Discard, runtimePaths{}, cfg, false)
	if err != nil {
		t.Fatalf("legacy exchange should succeed: %v", err)
	}
	if token != "legacy-token-for-473921" {
		t.Fatalf("unexpected legacy token %q", token)
	}
}

// A relay that advertises v1 but then rejects it mid-flow must not fall through to
// the legacy /pair exchange on a box with no legacy token store — that would spend
// the one-time code and only fail later at token persistence.
func TestSetupPairingMidFlowRelayNotV1FailsSafeWithoutTokenStore(t *testing.T) {
	origProbe, origSecure, origExchange := probePairingV1ForSetup, securePairForSetup, exchangeRelayPairingCodeForSetup
	defer func() {
		probePairingV1ForSetup, securePairForSetup, exchangeRelayPairingCodeForSetup = origProbe, origSecure, origExchange
	}()
	probePairingV1ForSetup = func(string) bool { return true } // v1 at first
	securePairForSetup = func(_, _ string, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		return "", errRelayNotV1 // relay turns out pre-v1 mid-flow
	}
	exchangeCalled := false
	exchangeRelayPairingCodeForSetup = func(_ *http.Client, _, _ string) (string, error) {
		exchangeCalled = true
		return "legacy-token", nil
	}

	reader := bufio.NewReader(strings.NewReader("\n473921\n"))
	cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791"}
	_, err := runSetupPairingFlow(reader, io.Discard, runtimePaths{}, cfg, true) // legacyTokenStoreUnavailable
	if err == nil {
		t.Fatal("expected a fail-safe error on mid-flow errRelayNotV1 with no legacy token store")
	}
	if exchangeCalled {
		t.Fatal("legacy /pair exchange was reached — a one-time code could be consumed with no store to persist the token")
	}
}

// A transient v1 probe failure (relay still starting / momentarily unreachable)
// must NOT be reported as a too-old relay, and must not spend a code — the user
// should be told to retry once the relay is up.
func TestSetupPairingTransientV1ProbeFailureDoesNotDeclareOldRelay(t *testing.T) {
	origProbe, origDetailed, origExchange := probePairingV1ForSetup, probePairingV1DetailedForSetup, exchangeRelayPairingCodeForSetup
	defer func() {
		probePairingV1ForSetup, probePairingV1DetailedForSetup, exchangeRelayPairingCodeForSetup = origProbe, origDetailed, origExchange
	}()
	probePairingV1ForSetup = func(string) bool { return false }                          // probe returns false...
	probePairingV1DetailedForSetup = func(string) (bool, bool) { return false, false } // ...and the relay is unreachable
	exchangeCalled := false
	exchangeRelayPairingCodeForSetup = func(_ *http.Client, _, _ string) (string, error) {
		exchangeCalled = true
		return "legacy-token", nil
	}

	reader := bufio.NewReader(strings.NewReader("\n473921\n"))
	cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791"}
	_, err := runSetupPairingFlow(reader, io.Discard, runtimePaths{}, cfg, true)
	if err == nil {
		t.Fatal("expected an error when the relay is unreachable")
	}
	if strings.Contains(err.Error(), "predates secure pairing") {
		t.Fatalf("a transient probe failure must not be reported as a too-old relay: %v", err)
	}
	if exchangeCalled {
		t.Fatal("legacy /pair exchange reached despite an unreachable relay (a code could be consumed)")
	}
}

// If the first v1 probe fails transiently but the second (detailed) probe finds
// v1 support (relay finished starting), setup proceeds with secure pairing rather
// than declaring the relay too old.
func TestSetupPairingRecoversWhenSecondV1ProbeSucceeds(t *testing.T) {
	origProbe, origDetailed, origSecure, origExchange := probePairingV1ForSetup, probePairingV1DetailedForSetup, securePairForSetup, exchangeRelayPairingCodeForSetup
	defer func() {
		probePairingV1ForSetup, probePairingV1DetailedForSetup, securePairForSetup, exchangeRelayPairingCodeForSetup = origProbe, origDetailed, origSecure, origExchange
	}()
	probePairingV1ForSetup = func(string) bool { return false }                        // transient miss...
	probePairingV1DetailedForSetup = func(string) (bool, bool) { return true, true } // ...v1 is up on the retry
	securePaired := false
	securePairForSetup = func(_, _ string, cfg *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		securePaired = true
		return "device-id", nil
	}
	exchangeCalled := false
	exchangeRelayPairingCodeForSetup = func(_ *http.Client, _, _ string) (string, error) {
		exchangeCalled = true
		return "legacy-token", nil
	}

	reader := bufio.NewReader(strings.NewReader("\n473921\n"))
	cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791"}
	_, err := runSetupPairingFlow(reader, io.Discard, runtimePaths{}, cfg, true)
	if !errors.Is(err, errSetupDevicePaired) {
		t.Fatalf("expected secure pairing to proceed after the v1 probe recovered, got %v", err)
	}
	if !securePaired || exchangeCalled {
		t.Fatalf("expected secure pairing, not the legacy exchange; securePaired=%v exchangeCalled=%v", securePaired, exchangeCalled)
	}
}
