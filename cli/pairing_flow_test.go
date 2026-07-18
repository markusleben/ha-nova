package main

import (
	"errors"
	"net/http"
	"testing"
)

// Regression: a re-pair whose activation fails must NOT overwrite the working
// install's live endpoint; the new endpoint is only recorded as pending (so a
// crash between activation and promotion can still resume).
func TestRunSecurePairingFailedRePairPreservesLiveEndpoint(t *testing.T) {
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())

	origPair, origActivate := pairDeviceV1ForPairing, activateDeviceV1ForPairing
	const validCred = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	pairDeviceV1ForPairing = func(_ *http.Client, _, _ string, _ deviceMetadata) (*provisionedCredential, error) {
		return &provisionedCredential{DeviceID: "dev-new", Credential: validCred, SpkiPin: "NEW_PIN", SecurePort: 8792}, nil
	}
	activateDeviceV1ForPairing = func(_, _, _ string) error {
		return errors.New("secure port unreachable")
	}
	t.Cleanup(func() {
		pairDeviceV1ForPairing = origPair
		activateDeviceV1ForPairing = origActivate
	})

	cfg := runtimeConfig{ClientInstallID: "inst-existing", RelaySecureBaseURL: "https://old:8792", RelaySpkiPin: "OLD_PIN"}
	if _, err := runSecurePairing("http://relay:8791", "123456", &cfg, func(*runtimeConfig) error { return nil }, defaultPairingClientInfo()); err == nil {
		t.Fatal("expected a pairing error when activation fails")
	}
	if cfg.RelaySecureBaseURL != "https://old:8792" || cfg.RelaySpkiPin != "OLD_PIN" {
		t.Fatalf("failed re-pair overwrote the live endpoint: %q / %q", cfg.RelaySecureBaseURL, cfg.RelaySpkiPin)
	}
	if cfg.PendingSecureBaseURL == "" || cfg.PendingSpkiPin != "NEW_PIN" {
		t.Fatalf("pending endpoint not recorded: %q / %q", cfg.PendingSecureBaseURL, cfg.PendingSpkiPin)
	}
}

// Regression: an IPv6 relay host must stay bracketed when building the secure
// endpoint URL, or activation and later functional calls get an invalid host.
func TestSecureBaseFromBootstrapBracketsIPv6(t *testing.T) {
	got, err := secureBaseFromBootstrap("http://[2001:db8::1]:8791", 8792)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://[2001:db8::1]:8792" {
		t.Fatalf("IPv6 host not bracketed: got %q", got)
	}
}

func TestSecureBaseFromBootstrapIPv4(t *testing.T) {
	got, err := secureBaseFromBootstrap("http://192.168.1.5:8791", 8792)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://192.168.1.5:8792" {
		t.Fatalf("got %q, want https://192.168.1.5:8792", got)
	}
}

func TestSecureBaseFromBootstrapRejectsBadPort(t *testing.T) {
	if _, err := secureBaseFromBootstrap("http://192.168.1.5:8791", 0); err == nil {
		t.Fatal("expected an error for an invalid secure port")
	}
}
