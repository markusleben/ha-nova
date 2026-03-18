package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRelayAuthTokenOverrideRoundTrip(t *testing.T) {
	overridePath := filepath.Join(t.TempDir(), ".test-relay-auth-token")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", overridePath)

	if overridden, err := writeRelayAuthTokenOverride("secret-token"); !overridden || err != nil {
		t.Fatalf("write override failed: overridden=%v err=%v", overridden, err)
	}
	token, overridden, err := readRelayAuthTokenOverride()
	if !overridden || err != nil {
		t.Fatalf("read override failed: overridden=%v err=%v", overridden, err)
	}
	if token != "secret-token" {
		t.Fatalf("unexpected token %q", token)
	}
	if overridden, err := deleteRelayAuthTokenOverride(); !overridden || err != nil {
		t.Fatalf("delete override failed: overridden=%v err=%v", overridden, err)
	}
	if _, err := os.Stat(overridePath); !isNotExist(err) {
		t.Fatalf("expected override file deleted, err=%v", err)
	}
}

func TestRelayAuthTokenOverrideRequiresExplicitOptIn(t *testing.T) {
	overridePath := filepath.Join(t.TempDir(), ".test-relay-auth-token")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", overridePath)

	if path := relayAuthTokenTestFile(); path != "" {
		t.Fatalf("expected override disabled without opt-in, got %q", path)
	}
}

func TestRelayAuthTokenProblemMessageDifferentiatesMissingAndUnavailable(t *testing.T) {
	if got := relayAuthTokenProblemMessage(missingRelayAuthTokenError("ha-nova.relay-auth-token")); got != "relay auth token missing; run: ha-nova setup" {
		t.Fatalf("missing-token message = %q", got)
	}
	if got := relayAuthTokenProblemMessage(errors.New("keychain locked")); got != "relay auth token unavailable: keychain locked" {
		t.Fatalf("unavailable-token message = %q", got)
	}
}
