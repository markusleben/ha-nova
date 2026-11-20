package main

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	cfg := &runtimeConfig{
		RelayBaseURL:    "http://relay:8791",
		ClientInstallID: "inst-test",
	}
	token, err := runSetupPairingFlow(reader, io.Discard, runtimePaths{ConfigDir: t.TempDir()}, cfg, false)
	if !errors.Is(err, errSetupDevicePaired) {
		t.Fatalf("expected errSetupDevicePaired, got token=%q err=%v", token, err)
	}
	if token != "" || !paired {
		t.Fatalf("device pairing should store no relay token; token=%q paired=%v", token, paired)
	}
}

// A reachable Relay without secure pairing is a standalone container: setup
// sends the person to its token, before any code is asked for or spent.
func TestSetupPairingStandaloneRelayGoesToItsToken(t *testing.T) {
	origProbe, origAnswer, origSecure := probePairingV1ForSetup, probeRelayPairingForSetup, securePairForSetup
	defer func() {
		probePairingV1ForSetup, probeRelayPairingForSetup, securePairForSetup = origProbe, origAnswer, origSecure
	}()
	probePairingV1ForSetup = func(string) bool { return false }
	probeRelayPairingForSetup = func(string) relayPairingAnswer { return relayAnswerStandalone }
	securePairForSetup = func(_, _ string, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		t.Fatal("a standalone Relay must not be paired with a code")
		return "", nil
	}

	for _, storeUnavailable := range []bool{false, true} {
		var out strings.Builder
		reader := bufio.NewReader(strings.NewReader("\n473921\n"))
		cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791", ClientInstallID: "inst-test"}
		_, err := runSetupPairingFlow(reader, &out, runtimePaths{ConfigDir: t.TempDir()}, cfg, storeUnavailable)
		if strings.Contains(out.String(), "Six-digit code") {
			t.Fatalf("store unavailable=%v: asked for a code:\n%s", storeUnavailable, out.String())
		}
		if storeUnavailable == errors.Is(err, errSetupStandaloneToken) {
			t.Fatalf("store unavailable=%v: err = %v (token step only when the token can be stored)", storeUnavailable, err)
		}
		if storeUnavailable && !strings.Contains(out.String(), "standalone container") {
			t.Fatalf("missing the token-only guidance:\n%s", out.String())
		}
	}
}

// A standalone container's token gate met mid-flow goes to its token; a Relay
// that drops secure pairing mid-flow gets the address hint, never a token ask.
func TestSetupPairingMidFlowAnswersRouteByRelay(t *testing.T) {
	origProbe, origSecure := probePairingV1ForSetup, securePairForSetup
	defer func() { probePairingV1ForSetup, securePairForSetup = origProbe, origSecure }()
	probePairingV1ForSetup = func(string) bool { return true }

	for _, tc := range []struct {
		pairErr error
		want    error
	}{{errRelayStandalone, errSetupStandaloneToken}, {errRelayNotV1, nil}} {
		securePairForSetup = func(_, _ string, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
			return "", tc.pairErr
		}
		var out strings.Builder
		reader := bufio.NewReader(strings.NewReader("\n473921\n"))
		cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791", ClientInstallID: "inst-test"}
		_, err := runSetupPairingFlow(reader, &out, runtimePaths{ConfigDir: t.TempDir()}, cfg, false)
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Fatalf("%v: err = %v, want %v", tc.pairErr, err, tc.want)
		}
		if tc.want == nil && (err == nil || errors.Is(err, errSetupStandaloneToken) || !strings.Contains(out.String(), "not as a NOVA Relay")) {
			t.Fatalf("%v: err = %v, want the address hint:\n%s", tc.pairErr, err, out.String())
		}
	}
}

// Only the standalone container's own token gate earns a token ask; a proxy's
// error, a redirect target, or another service at the address gets the
// address hint.
func TestSetupPairingOtherAnswerIsNotAskedForAToken(t *testing.T) {
	origProbe, origAnswer := probePairingV1ForSetup, probeRelayPairingForSetup
	defer func() { probePairingV1ForSetup, probeRelayPairingForSetup = origProbe, origAnswer }()
	probePairingV1ForSetup = func(string) bool { return false }
	probeRelayPairingForSetup = func(string) relayPairingAnswer { return relayAnswerOther }

	var out strings.Builder
	cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791", ClientInstallID: "inst-test"}
	_, err := runSetupPairingFlow(bufio.NewReader(strings.NewReader("\n473921\n")), &out, runtimePaths{ConfigDir: t.TempDir()}, cfg, false)
	if err == nil || errors.Is(err, errSetupStandaloneToken) || errors.Is(err, errSetupRelayTokenStep) {
		t.Fatalf("err = %v, want the address hint", err)
	}
	if !strings.Contains(out.String(), "not as a NOVA Relay") || strings.Contains(out.String(), "Six-digit code") {
		t.Fatalf("want the address hint before any code:\n%s", out.String())
	}
}

func TestProbeRelayPairingTellsTheStandaloneGateApart(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   relayPairingAnswer
	}{
		{"app", http.StatusOK, `{"ok":true,"data":{"relay_version":"0.7.0","protocol_version":"v1","available":true}}`, relayAnswerSecure},
		{"standalone", http.StatusUnauthorized, `{"ok":false,"error":{"code":"UNAUTHORIZED","message":"Missing authorization header"}}`, relayAnswerStandalone},
		{"proxy 502", http.StatusBadGateway, "Bad Gateway", relayAnswerOther},
		{"other 401", http.StatusUnauthorized, "401: Unauthorized", relayAnswerOther},
		{"not found", http.StatusNotFound, "", relayAnswerOther},
		{"other 200", http.StatusOK, "<html></html>", relayAnswerOther},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		if got := probeRelayPairing(srv.URL); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
		srv.Close()
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	if got := probeRelayPairing(dead.URL); got != relayAnswerNone {
		t.Errorf("closed server: got %v, want relayAnswerNone", got)
	}
}

// A transient v1 probe failure (relay still starting / momentarily unreachable)
// must NOT be reported as a standalone Relay: a code gets the retry hint, and
// 'manual' stays available at the prompt.
func TestSetupPairingUnreachableRelayIsNotCalledStandalone(t *testing.T) {
	origProbe, origAnswer := probePairingV1ForSetup, probeRelayPairingForSetup
	defer func() { probePairingV1ForSetup, probeRelayPairingForSetup = origProbe, origAnswer }()
	probePairingV1ForSetup = func(string) bool { return false }
	probeRelayPairingForSetup = func(string) relayPairingAnswer { return relayAnswerNone }

	var out strings.Builder
	reader := bufio.NewReader(strings.NewReader("\n473921\n"))
	cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791", ClientInstallID: "inst-test"}
	_, err := runSetupPairingFlow(reader, &out, runtimePaths{ConfigDir: t.TempDir()}, cfg, false)
	if err == nil || errors.Is(err, errSetupRelayTokenStep) || strings.Contains(out.String(), "standalone container") {
		t.Fatalf("an unreachable Relay must get the retry hint, got %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Could not reach the NOVA App") {
		t.Fatalf("missing the retry hint:\n%s", out.String())
	}
}

// If the first v1 probe fails transiently but the second (detailed) probe finds
// v1 support (relay finished starting), setup proceeds with secure pairing.
func TestSetupPairingRecoversWhenSecondV1ProbeSucceeds(t *testing.T) {
	origProbe, origAnswer, origSecure := probePairingV1ForSetup, probeRelayPairingForSetup, securePairForSetup
	defer func() {
		probePairingV1ForSetup, probeRelayPairingForSetup, securePairForSetup = origProbe, origAnswer, origSecure
	}()
	probePairingV1ForSetup = func(string) bool { return false }                              // transient miss...
	probeRelayPairingForSetup = func(string) relayPairingAnswer { return relayAnswerSecure } // ...v1 is up on the retry
	securePaired := false
	securePairForSetup = func(_, _ string, cfg *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		securePaired = true
		return "device-id", nil
	}

	reader := bufio.NewReader(strings.NewReader("\n473921\n"))
	cfg := &runtimeConfig{
		RelayBaseURL:    "http://relay:8791",
		ClientInstallID: "inst-test",
	}
	_, err := runSetupPairingFlow(reader, io.Discard, runtimePaths{ConfigDir: t.TempDir()}, cfg, true)
	if !errors.Is(err, errSetupDevicePaired) || !securePaired {
		t.Fatalf("expected secure pairing to proceed after the v1 probe recovered, got %v (paired=%v)", err, securePaired)
	}
}
