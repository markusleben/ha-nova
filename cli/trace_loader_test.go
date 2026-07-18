package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Regression: `ha-nova trace` must route through the paired device transport, not
// read the legacy relay token directly — so it works on a passwordless install
// and fails closed (re-pair guidance) when a paired credential is missing,
// instead of failing on an absent legacy token or using the wrong transport.
func TestRelayWSJSONRoutesThroughPairedTransport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir()) // no device credential stored
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths := runtimePaths{ConfigFile: filepath.Join(home, "config.json")}
	if err := saveConfig(paths, runtimeConfig{
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	_, err := relayWSJSON(paths, map[string]any{"type": "ping"})
	if err == nil {
		t.Fatal("expected trace to fail closed on a paired config with no credential")
	}
	if msg := err.Error(); !strings.Contains(msg, "re-pair") && !strings.Contains(msg, "device credential") {
		t.Fatalf("trace did not route through the paired transport: %v", err)
	}
}
