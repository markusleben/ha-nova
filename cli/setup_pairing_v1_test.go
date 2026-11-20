package main

import (
	"bufio"
	"errors"
	"io"
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
	origProbe, origDetailed, origSecure := probePairingV1ForSetup, probePairingV1DetailedForSetup, securePairForSetup
	defer func() {
		probePairingV1ForSetup, probePairingV1DetailedForSetup, securePairForSetup = origProbe, origDetailed, origSecure
	}()
	probePairingV1ForSetup = func(string) bool { return false }
	probePairingV1DetailedForSetup = func(string) (bool, bool) { return false, true }
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
		if !strings.Contains(out.String(), "standalone container") {
			t.Fatalf("store unavailable=%v: missing the token-only guidance:\n%s", storeUnavailable, out.String())
		}
		if storeUnavailable == errors.Is(err, errSetupRelayTokenStep) {
			t.Fatalf("store unavailable=%v: err = %v (token step only when the token can be stored)", storeUnavailable, err)
		}
	}
}

// A Relay that stops offering secure pairing mid-flow is treated the same way.
func TestSetupPairingMidFlowRelayNotV1GoesToItsToken(t *testing.T) {
	origProbe, origSecure := probePairingV1ForSetup, securePairForSetup
	defer func() { probePairingV1ForSetup, securePairForSetup = origProbe, origSecure }()
	probePairingV1ForSetup = func(string) bool { return true }
	securePairForSetup = func(_, _ string, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		return "", errRelayNotV1
	}

	for _, storeUnavailable := range []bool{false, true} {
		var out strings.Builder
		reader := bufio.NewReader(strings.NewReader("\n473921\n"))
		cfg := &runtimeConfig{RelayBaseURL: "http://relay:8791", ClientInstallID: "inst-test"}
		_, err := runSetupPairingFlow(reader, &out, runtimePaths{ConfigDir: t.TempDir()}, cfg, storeUnavailable)
		if storeUnavailable == errors.Is(err, errSetupRelayTokenStep) || err == nil {
			t.Fatalf("store unavailable=%v: err = %v", storeUnavailable, err)
		}
		if !strings.Contains(out.String(), "standalone container") {
			t.Fatalf("store unavailable=%v: missing the token-only guidance:\n%s", storeUnavailable, out.String())
		}
	}
}

// A transient v1 probe failure (relay still starting / momentarily unreachable)
// must NOT be reported as a standalone Relay: a code gets the retry hint, and
// 'manual' stays available at the prompt.
func TestSetupPairingUnreachableRelayIsNotCalledStandalone(t *testing.T) {
	origProbe, origDetailed := probePairingV1ForSetup, probePairingV1DetailedForSetup
	defer func() { probePairingV1ForSetup, probePairingV1DetailedForSetup = origProbe, origDetailed }()
	probePairingV1ForSetup = func(string) bool { return false }
	probePairingV1DetailedForSetup = func(string) (bool, bool) { return false, false }

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
	origProbe, origDetailed, origSecure := probePairingV1ForSetup, probePairingV1DetailedForSetup, securePairForSetup
	defer func() {
		probePairingV1ForSetup, probePairingV1DetailedForSetup, securePairForSetup = origProbe, origDetailed, origSecure
	}()
	probePairingV1ForSetup = func(string) bool { return false }                      // transient miss...
	probePairingV1DetailedForSetup = func(string) (bool, bool) { return true, true } // ...v1 is up on the retry
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
