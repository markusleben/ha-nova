package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
)

// Headless Linux + standalone relay (client matrix): no Secret Service, relay
// token only. Codex/OpenCode wiring must work through the service token file.
func setupHeadlessSetupTest(t *testing.T) (runtimePaths, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	withClientRuntimeAvailability(t, map[string]bool{"codex": true, "hermes": true})
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "")
	previousPreflight := relayAuthTokenPlatformSetupPreflight
	relayAuthTokenPlatformSetupPreflight = func() error {
		return desktopKeyringSessionUnavailableError("no session bus")
	}
	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	originalHealth := fetchRelayHealthForSetup
	originalWSPing := probeRelayWSPingForSetup
	originalReadinessHealth := fetchRelayHealthForReadiness
	originalReadinessWSPing := probeRelayWSPingForReadiness
	t.Cleanup(func() {
		haServer.Close()
		relayAuthTokenPlatformSetupPreflight = previousPreflight
		fetchRelayHealthForSetup = originalHealth
		probeRelayWSPingForSetup = originalWSPing
		fetchRelayHealthForReadiness = originalReadinessHealth
		probeRelayWSPingForReadiness = originalReadinessWSPing
	})
	health := func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	ping := func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":null}`)}, nil
	}
	fetchRelayHealthForSetup = health
	fetchRelayHealthForReadiness = health
	probeRelayWSPingForSetup = ping
	probeRelayWSPingForReadiness = ping
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	return paths, haServer.URL
}

func runHeadlessSetup(t *testing.T, paths runtimePaths, haURL string, args ...string) {
	t.Helper()
	base := []string{
		"--host", normalizeHostInput(haURL),
		"--ha-url", haURL,
		"--relay-url", "http://relay.test:8791",
		"--non-interactive",
	}
	exit, output := captureCommandOutput(t, func() int {
		return runSetup(paths, append(base, args...))
	})
	if exit != 0 {
		t.Fatalf("setup %v exit = %d:\n%s", args, exit, output)
	}
}

func requireServiceToken(t *testing.T, paths runtimePaths, want string) {
	t.Helper()
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RelayTokenFile != defaultRelayAuthTokenFile(paths) {
		t.Fatalf("RelayTokenFile = %q, want the service token file", cfg.RelayTokenFile)
	}
	token, err := readRelayAuthTokenFile(cfg.RelayTokenFile)
	if err != nil || token != want {
		t.Fatalf("service token = %q, %v; want %q", token, err, want)
	}
}

func TestHeadlessServiceSetupAcceptsCodex(t *testing.T) {
	paths, haURL := setupHeadlessSetupTest(t)
	runHeadlessSetup(t, paths, haURL, "--service", "--relay-token", "tok", "codex")
	requireServiceToken(t, paths, "tok")
}

func TestHeadlessDesktopSetupKeepsConfiguredServiceTokenFile(t *testing.T) {
	paths, haURL := setupHeadlessSetupTest(t)
	runHeadlessSetup(t, paths, haURL, "--service", "--relay-token", "tok", "hermes")
	exit, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{"--non-interactive", "codex"})
	})
	if exit != 0 {
		t.Fatalf("setup codex exit = %d:\n%s", exit, output)
	}
	if !strings.Contains(output, "Keeping the service token file") {
		t.Fatalf("missing keep note:\n%s", output)
	}
	requireServiceToken(t, paths, "tok")
	if _, err := os.Stat(defaultRelayAuthTokenFile(paths)); err != nil {
		t.Fatalf("service token file removed: %v", err)
	}
}

func TestHeadlessDesktopSetupPointsToServiceMode(t *testing.T) {
	paths, haURL := setupHeadlessSetupTest(t)
	exit, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"--host", normalizeHostInput(haURL), "--ha-url", haURL,
			"--relay-url", "http://relay.test:8791", "--relay-token", "tok",
			"--non-interactive", "codex",
		})
	})
	if exit != 1 || !strings.Contains(output, "ha-nova setup --service codex") {
		t.Fatalf("exit = %d, want the service-mode hint:\n%s", exit, output)
	}
}
