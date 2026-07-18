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
	token, err := runSetupPairingFlow(reader, io.Discard, runtimePaths{}, cfg)
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
	token, err := runSetupPairingFlow(reader, io.Discard, runtimePaths{}, cfg)
	if err != nil {
		t.Fatalf("legacy exchange should succeed: %v", err)
	}
	if token != "legacy-token-for-473921" {
		t.Fatalf("unexpected legacy token %q", token)
	}
}
