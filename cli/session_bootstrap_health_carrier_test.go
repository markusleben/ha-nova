package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func healthCarrierFixture(t *testing.T) runtimePaths {
	t.Helper()
	paths := setupHealableInstall(t)
	codexRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	if _, err := installTreeClient(filepath.Dir(codexRoot), filepath.Join(paths.InstallRoot, "skills"), false); err != nil {
		t.Fatalf("seed copied Codex tree: %v", err)
	}
	if err := os.Remove(filepath.Join(codexRoot, "ha-nova", "session-bootstrap.md")); err != nil {
		t.Fatalf("remove bootstrap: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{
		Version: "0.6.1",
		HTMLURL: "https://example.invalid/releases/v0.6.1",
	}, 0o644); err != nil {
		t.Fatalf("write current update cache: %v", err)
	}
	if err := saveState(paths, installState{
		SchemaVersion: stateSchemaVersion,
		Version:       "0.6.1",
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			fmt.Fprint(w, `{"ok":true,"data":{"status":"ok","version":"0.6.1"}}`)
		case "/core":
			w.Write(statesEnvelope(t, nil))
		case "/ws":
			w.Write(registryEnvelope(t, nil))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(relay.Close)
	if err := saveConfig(paths, runtimeConfig{RelayBaseURL: relay.URL}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(paths.ConfigDir, ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_NO_CENSUS", "1")
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("write relay token: %v", err)
	}
	return paths
}

func TestSuccessfulHealthFinishesMigratedFirstUseCarrier(t *testing.T) {
	paths := healthCarrierFixture(t)
	if exit := runRelayCommand(paths, []string{"health"}); exit != 0 {
		t.Fatalf("health exit = %d", exit)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if !markerHasVersion(marker, "0.6.1") {
		t.Fatal("successful health did not finalize migrated first-use carrier")
	}
}

func TestShortHealthProbeLeavesMigratedCarrierPending(t *testing.T) {
	paths := healthCarrierFixture(t)
	if exit := runRelayCommand(paths, []string{"health", "--connect-timeout", "1", "--max-time", "2"}); exit != 0 {
		t.Fatalf("health exit = %d", exit)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if !markerHasCarrierPending(marker, "0.6.1") {
		t.Fatal("short hook-style health probe consumed the migrated carrier")
	}
}
