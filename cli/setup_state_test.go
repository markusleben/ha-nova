package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSetupStatusSummaryShowsAllSignals(t *testing.T) {
	output := &bytes.Buffer{}
	state := setupState{
		RelayOK:  true,
		TokenOK:  true,
		WSOK:     false,
		SkillsOK: true,
	}

	renderSetupStatusSummary(output, state)

	rendered := output.String()
	for _, want := range []string{
		"Checking current setup...",
		"Relay reachable",
		"Authentication valid",
		"WebSocket not connected",
		"Skills installed",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("status summary missing %q:\n%s", want, rendered)
		}
	}
}

func TestSetupStateIsCompleteOnlyWhenAllCoreChecksPass(t *testing.T) {
	state := setupState{
		ConfigOK: true,
		TokenOK:  true,
		RelayOK:  true,
		WSOK:     true,
		SkillsOK: true,
	}
	if !state.IsComplete() {
		t.Fatal("expected complete state")
	}

	state.WSOK = false
	if state.IsComplete() {
		t.Fatal("expected incomplete state when websocket is down")
	}
}

func TestSetupStateSkipSummaryListsCompletedPhases(t *testing.T) {
	state := setupState{
		ConfigOK: true,
		TokenOK:  true,
		RelayOK:  true,
		WSOK:     false,
		SkillsOK: true,
	}

	summary := state.SkipSummary()
	for _, want := range []string{
		"app installation",
		"relay token",
		"connection check",
		"skill installation",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("skip summary missing %q: %s", want, summary)
		}
	}
	if strings.Contains(summary, "access token") {
		t.Fatalf("skip summary should not include access token when WS is not ready: %s", summary)
	}
}

func TestRenderSetupStatusSummaryShowsAuthFailureWhenTokenExistsButRelayFails(t *testing.T) {
	output := &bytes.Buffer{}
	state := setupState{
		TokenOK:  true,
		RelayOK:  false,
		WSOK:     false,
		SkillsOK: false,
	}

	renderSetupStatusSummary(output, state)

	rendered := output.String()
	if !strings.Contains(rendered, "Relay not reachable") {
		t.Fatalf("expected relay failure in summary:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Authentication failed") {
		t.Fatalf("expected auth failure in summary:\n%s", rendered)
	}
}

func TestDetectSetupStateUsesWSPingFallbackForResume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), []byte(`{"plugins":["ha-nova@ha-nova"]}`), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}

	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: 200, Body: []byte(`{"type":"pong"}`)}, nil
	}

	state := loadStateOrDefault(paths)
	got := detectSetupState(paths, cfg, state, "claude")
	if !got.RelayOK {
		t.Fatalf("expected relay to be ok: %+v", got)
	}
	if !got.WSOK {
		t.Fatalf("expected ws fallback to count as ready: %+v", got)
	}
}

func TestClientAppearsInstalledForClaudeIgnoresStaleStateWithoutPluginRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	entry, ok, err := findRegistryClient(paths, "claude")
	if err != nil {
		t.Fatalf("findRegistryClient() error: %v", err)
	}
	if !ok {
		t.Fatal("expected claude registry entry")
	}
	state := installState{InstalledClients: []string{"claude"}}
	if evaluateClientStatus(paths, state, entry).Ready {
		t.Fatal("expected stale Claude state without plugin record to count as not installed")
	}
}

func TestClientAppearsInstalledForClaudeIgnoresBrokenInstallPathRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), []byte(`{
  "plugins": {
    "ha-nova@ha-nova": [
      {
        "installPath": "/definitely/missing"
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}

	entry, ok, err := findRegistryClient(paths, "claude")
	if err != nil {
		t.Fatalf("findRegistryClient() error: %v", err)
	}
	if !ok {
		t.Fatal("expected claude registry entry")
	}
	if evaluateClientStatus(paths, installState{}, entry).Ready {
		t.Fatal("expected broken Claude installPath record to count as not installed")
	}
}

func TestClientAppearsInstalledForClaudeIgnoresBlankInstallPathRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), []byte(`{
  "plugins": {
    "ha-nova@ha-nova": [
      {
        "installPath": ""
      }
    ]
  }
}`), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"), []byte(`{"ha-nova":{"source":"https://github.com/markusleben/ha-nova"}}`), 0o644); err != nil {
		t.Fatalf("write known_marketplaces.json: %v", err)
	}

	entry, ok, err := findRegistryClient(paths, "claude")
	if err != nil {
		t.Fatalf("findRegistryClient() error: %v", err)
	}
	if !ok {
		t.Fatal("expected claude registry entry")
	}
	if evaluateClientStatus(paths, installState{}, entry).Ready {
		t.Fatal("expected blank Claude installPath record to count as not installed")
	}
}

func TestClientAppearsInstalledForClaudeIgnoresUnparseableRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), []byte(`{"plugins":{"ha-nova@ha-nova":`), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}

	entry, ok, err := findRegistryClient(paths, "claude")
	if err != nil {
		t.Fatalf("findRegistryClient() error: %v", err)
	}
	if !ok {
		t.Fatal("expected claude registry entry")
	}
	if evaluateClientStatus(paths, installState{}, entry).Ready {
		t.Fatal("expected unparseable Claude registry to count as not installed")
		t.Fatal("expected unparseable Claude registry to count as not installed")
	}
}

func TestClientAppearsInstalledForClaudeRequiresMarketplaceRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	entry, ok, err := findRegistryClient(paths, "claude")
	if err != nil {
		t.Fatalf("findRegistryClient() error: %v", err)
	}
	if !ok {
		t.Fatal("expected claude registry entry")
	}
	if evaluateClientStatus(paths, installState{}, entry).Ready {
		t.Fatal("expected missing Claude marketplace record to count as not installed")
	}
}

func TestClientAppearsInstalledForClaudeRejectsLegacyFlatMarketplaceRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	writeInstalledClaudePluginFixture(t, home)
	legacyRoot := filepath.Join(home, ".config", "ha-nova", "claude-marketplace")
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"), []byte(fmt.Sprintf(`{"ha-nova":{"source":%q}}`, legacyRoot)), 0o644); err != nil {
		t.Fatalf("write known_marketplaces.json: %v", err)
	}

	entry, ok, err := findRegistryClient(paths, "claude")
	if err != nil {
		t.Fatalf("findRegistryClient() error: %v", err)
	}
	if !ok {
		t.Fatal("expected claude registry entry")
	}
	if evaluateClientStatus(paths, installState{}, entry).Ready {
		t.Fatal("expected legacy flat Claude marketplace root to count as not installed")
	}
}
