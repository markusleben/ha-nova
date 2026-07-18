package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunSetupNonInteractiveVerifiesBeforeInstallingClients(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	originalHealth := fetchRelayHealthForSetup
	originalWSPing := probeRelayWSPingForSetup
	defer func() {
		fetchRelayHealthForSetup = originalHealth
		probeRelayWSPingForSetup = originalWSPing
	}()
	fetchRelayHealthForSetup = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false}}`), nil
	}
	probeRelayWSPingForSetup = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusBadGateway, Body: []byte(`{"ok":false,"error":{"message":"upstream still offline"}}`)}, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"antigravity",
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", "http://relay.test:8791",
			"--relay-token", "test-relay-token",
			"--non-interactive",
		})
	})
	if exitCode == 0 {
		t.Fatalf("expected non-interactive setup to fail when readiness is not confirmed:\n%s", output)
	}
	if !strings.Contains(output, "Setup incomplete") {
		t.Fatalf("expected incomplete setup banner:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "config", "skills", "ha-nova", "SKILL.md")); !isNotExist(err) {
		t.Fatalf("expected Antigravity skills not to be installed on failed readiness, err=%v", err)
	}
	if _, err := os.Stat(paths.ConfigFile); !isNotExist(err) {
		t.Fatalf("expected failed non-interactive setup to roll config back, err=%v", err)
	}
	if _, err := os.Stat(paths.StateFile); !isNotExist(err) {
		t.Fatalf("expected failed non-interactive setup to roll state back, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token")); !isNotExist(err) {
		t.Fatalf("expected failed non-interactive setup to roll token back, err=%v", err)
	}
}

func TestRunSetupNonInteractiveSkipsClipboardAndBrowserSideEffects(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	originalHealth := fetchRelayHealthForSetup
	originalWSPing := probeRelayWSPingForSetup
	originalReadinessHealth := fetchRelayHealthForReadiness
	originalReadinessWSPing := probeRelayWSPingForReadiness
	originalClipboard := copyToClipboardForSetup
	originalBrowser := openBrowserForSetup
	defer func() {
		fetchRelayHealthForSetup = originalHealth
		probeRelayWSPingForSetup = originalWSPing
		fetchRelayHealthForReadiness = originalReadinessHealth
		probeRelayWSPingForReadiness = originalReadinessWSPing
		copyToClipboardForSetup = originalClipboard
		openBrowserForSetup = originalBrowser
	}()

	clipboardCalls := 0
	browserCalls := 0
	copyToClipboardForSetup = func(string) error {
		clipboardCalls++
		return nil
	}
	openBrowserForSetup = func(string) error {
		browserCalls++
		return nil
	}
	fetchRelayHealthForSetup = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForSetup = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":null}`)}, nil
	}
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":null}`)}, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"antigravity",
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", "http://relay.test:8791",
			"--relay-token", "test-relay-token",
			"--non-interactive",
		})
	})
	if exitCode != 0 {
		t.Fatalf("expected non-interactive setup to succeed:\n%s", output)
	}
	if clipboardCalls != 0 {
		t.Fatalf("expected non-interactive setup not to touch clipboard, got %d call(s)", clipboardCalls)
	}
	if browserCalls != 0 {
		t.Fatalf("expected non-interactive setup not to launch browser, got %d call(s)", browserCalls)
	}
}

func TestRunSetupNonInteractiveServiceModeWritesRelayTokenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	originalHealth := fetchRelayHealthForSetup
	originalWSPing := probeRelayWSPingForSetup
	originalReadinessHealth := fetchRelayHealthForReadiness
	originalReadinessWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForSetup = originalHealth
		probeRelayWSPingForSetup = originalWSPing
		fetchRelayHealthForReadiness = originalReadinessHealth
		probeRelayWSPingForReadiness = originalReadinessWSPing
	}()
	fetchRelayHealthForSetup = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForSetup = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":null}`)}, nil
	}
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":null}`)}, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"--service",
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", "http://relay.test:8791",
			"--relay-token", "test-relay-token",
			"--non-interactive",
			"hermes",
		})
	})
	if exitCode != 0 {
		t.Fatalf("expected service setup to succeed:\n%s", output)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.RelayTokenFile != defaultRelayAuthTokenFile(paths) {
		t.Fatalf("RelayTokenFile = %q, want %q", cfg.RelayTokenFile, defaultRelayAuthTokenFile(paths))
	}
	token, err := readRelayAuthTokenFile(cfg.RelayTokenFile)
	if err != nil {
		t.Fatalf("readRelayAuthTokenFile() error = %v", err)
	}
	if token != "test-relay-token" {
		t.Fatalf("token = %q, want test-relay-token", token)
	}
}

func TestRunSetupNonInteractiveServiceModeRequiresRegistryCapability(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"--service",
			"--host", "203.0.113.1",
			"--relay-token", "test-relay-token",
			"--non-interactive",
			"antigravity",
		})
	})
	if exitCode == 0 {
		t.Fatalf("expected service setup to fail for client without registry capability:\n%s", output)
	}
	if !strings.Contains(output, "Google Antigravity does not support service credentials") {
		t.Fatalf("expected registry capability error:\n%s", output)
	}
	if _, err := os.Stat(defaultRelayAuthTokenFile(paths)); !isNotExist(err) {
		t.Fatalf("expected service token file not to be written, err=%v", err)
	}
}

func TestRunSetupNonInteractiveServiceModeRequiresSpecificClient(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"--service",
			"--host", "203.0.113.1",
			"--relay-token", "test-relay-token",
			"--non-interactive",
			"all",
		})
	})
	if exitCode == 0 {
		t.Fatalf("expected service setup to fail for all target:\n%s", output)
	}
	if !strings.Contains(output, "service credentials require a specific client; use: ha-nova setup --service <client>") {
		t.Fatalf("expected explicit service target error:\n%s", output)
	}
	if _, err := os.Stat(defaultRelayAuthTokenFile(paths)); !isNotExist(err) {
		t.Fatalf("expected service token file not to be written, err=%v", err)
	}
}

func TestRunSetupNonInteractiveRollsBackWhenInitialStateSaveFails(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	originalSaveState := saveStateForSetup
	defer func() {
		saveStateForSetup = originalSaveState
	}()
	saveCalls := 0
	saveStateForSetup = func(paths runtimePaths, state installState) error {
		saveCalls++
		if saveCalls == 1 {
			return errors.New("disk full")
		}
		return originalSaveState(paths, state)
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"claude",
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", "http://relay.test:8791",
			"--relay-token", "test-relay-token",
			"--non-interactive",
		})
	})
	if exitCode != 1 {
		t.Fatalf("expected non-interactive setup to fail on state save error:\n%s", output)
	}
	if !strings.Contains(output, "cannot save state: disk full") {
		t.Fatalf("expected explicit state-save error:\n%s", output)
	}
	if _, err := os.Stat(paths.ConfigFile); !isNotExist(err) {
		t.Fatalf("expected config rollback on state save failure, err=%v", err)
	}
	if _, err := os.Stat(paths.StateFile); !isNotExist(err) {
		t.Fatalf("expected state rollback on state save failure, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token")); !isNotExist(err) {
		t.Fatalf("expected token rollback on state save failure, err=%v", err)
	}
}

func TestRunSetupNonInteractiveFailsOnLinuxKeyringPreflightBeforeWrite(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	originalPreflight := relayAuthTokenSetupPreflightForSetup
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	defer func() {
		relayAuthTokenSetupPreflightForSetup = originalPreflight
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
	}()
	relayAuthTokenSetupPreflightForSetup = func() error {
		return desktopKeyringSetupRequiredError("no default Secret Service collection configured")
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"claude",
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", "http://relay.test:8791",
			"--relay-token", "test-relay-token",
			"--non-interactive",
		})
	})
	if exitCode != 1 {
		t.Fatalf("expected non-interactive setup to fail on keyring preflight:\n%s", output)
	}
	if !strings.Contains(output, "cannot save relay token: secure storage is present but not initialized on this Linux machine") {
		t.Fatalf("expected actionable keyring preflight error:\n%s", output)
	}
	if !strings.Contains(output, "Recovery: run `ha-nova setup` interactively to set up local secure storage on this Linux machine.") {
		t.Fatalf("expected interactive recovery hint:\n%s", output)
	}
	if _, err := os.Stat(paths.ConfigFile); !isNotExist(err) {
		t.Fatalf("expected config not to be written after preflight failure, err=%v", err)
	}
	if _, err := os.Stat(paths.StateFile); !isNotExist(err) {
		t.Fatalf("expected state not to be written after preflight failure, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token")); !isNotExist(err) {
		t.Fatalf("expected token file not to be written after preflight failure, err=%v", err)
	}
}

func TestRunSetupNonInteractiveDesktopModeMigratesAwayFromServiceTokenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	keyringFile := filepath.Join(home, ".test-relay-auth-token")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", keyringFile)

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	originalHealth := fetchRelayHealthForSetup
	originalWSPing := probeRelayWSPingForSetup
	originalReadinessHealth := fetchRelayHealthForReadiness
	originalReadinessWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForSetup = originalHealth
		probeRelayWSPingForSetup = originalWSPing
		fetchRelayHealthForReadiness = originalReadinessHealth
		probeRelayWSPingForReadiness = originalReadinessWSPing
	}()
	fetchRelayHealthForSetup = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForSetup = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":null}`)}, nil
	}
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":null}`)}, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	// Previous service-mode install: config references the token file.
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	tokenFilePath := defaultRelayAuthTokenFile(paths)
	if err := writeRelayAuthTokenFile(tokenFilePath, "service-token"); err != nil {
		t.Fatalf("write service token file: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "ha.test", HAURL: "http://ha.test:8123", RelayBaseURL: "http://relay.test:8791", RelayTokenFile: "relay-token"}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	// Documented desktop path: setup without --service and without a token
	// flag must migrate the credential back to the OS keyring.
	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", "http://relay.test:8791",
			"--non-interactive",
			"hermes",
		})
	})
	if exitCode != 0 {
		t.Fatalf("expected desktop setup to succeed:\n%s", output)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.RelayTokenFile != "" {
		t.Fatalf("RelayTokenFile = %q, want empty (desktop mode)", cfg.RelayTokenFile)
	}
	if _, err := os.Stat(tokenFilePath); !isNotExist(err) {
		t.Fatalf("expected former service token file to be removed, err=%v", err)
	}
	migrated, err := os.ReadFile(keyringFile)
	if err != nil {
		t.Fatalf("read keyring file: %v", err)
	}
	if strings.TrimSpace(string(migrated)) != "service-token" {
		t.Fatalf("migrated token = %q, want service-token", strings.TrimSpace(string(migrated)))
	}
}

func TestRunSetupNonInteractiveDesktopModeMigratesFromIncompleteServiceConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	keyringFile := filepath.Join(home, ".test-relay-auth-token")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", keyringFile)

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	originalHealth := fetchRelayHealthForSetup
	originalWSPing := probeRelayWSPingForSetup
	originalReadinessHealth := fetchRelayHealthForReadiness
	originalReadinessWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForSetup = originalHealth
		probeRelayWSPingForSetup = originalWSPing
		fetchRelayHealthForReadiness = originalReadinessHealth
		probeRelayWSPingForReadiness = originalReadinessWSPing
	}()
	fetchRelayHealthForSetup = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForSetup = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":null}`)}, nil
	}
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":null}`)}, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	// Incomplete former service-mode config: relay_base_url missing, so
	// loadConfig fails — credential routing must still be honored.
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	tokenFilePath := defaultRelayAuthTokenFile(paths)
	if err := writeRelayAuthTokenFile(tokenFilePath, "service-token"); err != nil {
		t.Fatalf("write service token file: %v", err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"relay_token_file":"relay-token"}`), 0o600); err != nil {
		t.Fatalf("write incomplete config: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", "http://relay.test:8791",
			"--non-interactive",
			"hermes",
		})
	})
	if exitCode != 0 {
		t.Fatalf("expected desktop setup to succeed:\n%s", output)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.RelayTokenFile != "" {
		t.Fatalf("RelayTokenFile = %q, want empty (desktop mode)", cfg.RelayTokenFile)
	}
	if _, err := os.Stat(tokenFilePath); !isNotExist(err) {
		t.Fatalf("expected former service token file to be removed, err=%v", err)
	}
	migrated, err := os.ReadFile(keyringFile)
	if err != nil {
		t.Fatalf("read keyring file: %v", err)
	}
	if strings.TrimSpace(string(migrated)) != "service-token" {
		t.Fatalf("migrated token = %q, want service-token", strings.TrimSpace(string(migrated)))
	}
}

// F1 (round 3) regression: headless recovery with an explicit token must
// retire a leftover device pairing, or the trailing doctor run (and every
// skill call) resolves the dead pairing first and fails.
func TestRunSetupNonInteractiveWithTokenRetiresDeadPairing(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	originalHealth := fetchRelayHealthForSetup
	defer func() { fetchRelayHealthForSetup = originalHealth }()
	fetchRelayHealthForSetup = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	// The command tails into doctor, which probes with its own hooks.
	originalReadinessHealth := fetchRelayHealthForReadiness
	originalReadinessWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalReadinessHealth
		probeRelayWSPingForReadiness = originalReadinessWSPing
	}()
	fetchRelayHealthForReadiness = func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForReadiness = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK}, nil
	}
	originalRetireRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error { return nil }
	defer func() { revokeSelfDeviceV1ForRetire = originalRetireRevoke }()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	// A dead pairing from an earlier life of this install.
	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:             normalizeHostInput(haServer.URL),
		HAURL:              haServer.URL,
		RelayBaseURL:       "http://relay.test:8791",
		RelaySecureBaseURL: "https://192.168.1.5:18792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"antigravity",
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", "http://relay.test:8791",
			"--relay-token", "test-relay-token",
			"--non-interactive",
		})
	})
	if exitCode != 0 {
		t.Fatalf("non-interactive setup exit = %d, want 0:\n%s", exitCode, output)
	}
	if _, ok, _ := readDeviceCredential(); ok {
		t.Fatal("expected the dead device credential to be retired")
	}
	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.RelaySecureBaseURL != "" || saved.RelaySpkiPin != "" {
		t.Fatalf("expected secure fields cleared, got %q/%q", saved.RelaySecureBaseURL, saved.RelaySpkiPin)
	}
}

// Round-4 regression: a routine non-interactive re-sync that silently reuses
// the STORED token must never retire a (possibly live) device pairing — only
// an explicit --relay-token expresses that intent.
func TestRunSetupNonInteractiveStoredTokenKeepsLivePairing(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	originalHealth := fetchRelayHealthForSetup
	defer func() { fetchRelayHealthForSetup = originalHealth }()
	fetchRelayHealthForSetup = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	originalReadinessHealth := fetchRelayHealthForReadiness
	originalReadinessWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalReadinessHealth
		probeRelayWSPingForReadiness = originalReadinessWSPing
	}()
	fetchRelayHealthForReadiness = func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForReadiness = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK}, nil
	}
	revokeCalls := 0
	originalRetireRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error { revokeCalls++; return nil }
	defer func() { revokeSelfDeviceV1ForRetire = originalRetireRevoke }()

	// The trailing doctor resolves the device transport; point it at a local
	// healthy endpoint instead of the config's (unreachable) secure URL.
	secure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer secure.Close()
	originalTransport := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return secure.URL, httpClient, "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", true, nil
	}
	defer func() { relayFunctionalTransportForDoctor = originalTransport }()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	// A legacy-then-paired install: stored token AND a live pairing.
	if err := writeRelayAuthToken("stored-legacy-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:             normalizeHostInput(haServer.URL),
		HAURL:              haServer.URL,
		RelayBaseURL:       "http://relay.test:8791",
		RelaySecureBaseURL: "https://192.168.1.5:18792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"antigravity",
			"--host", normalizeHostInput(haServer.URL),
			"--ha-url", haServer.URL,
			"--relay-url", "http://relay.test:8791",
			"--non-interactive",
		})
	})
	if exitCode != 0 {
		t.Fatalf("non-interactive setup exit = %d, want 0:\n%s", exitCode, output)
	}
	if revokeCalls != 0 {
		t.Fatalf("stored-token re-sync revoked the live pairing (%d call(s))", revokeCalls)
	}
	if _, ok, _ := readDeviceCredential(); !ok {
		t.Fatal("stored-token re-sync must keep the device credential")
	}
	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.RelaySecureBaseURL == "" || saved.RelaySpkiPin == "" {
		t.Fatal("stored-token re-sync must keep the secure endpoint fields")
	}
}

// Regression: a passwordless-paired install has no legacy token; noninteractive
// setup must honor the device credential and route to the device path instead of
// failing with "missing relay auth token". verifyDeviceHealth is stubbed false to
// isolate the routing without a live relay.
func TestRunSetupNonInteractivePairedInstallHonorsDeviceCredential(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	const validCred = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := writeDeviceCredential(validCred); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:             "192.168.1.5",
		HAURL:              "http://192.168.1.5:8123",
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	origVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) bool { return false }
	defer func() { verifyDeviceHealth = origVerify }()

	_, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{"antigravity", "--non-interactive"})
	})
	if strings.Contains(output, "missing relay auth token") {
		t.Fatalf("paired install wrongly required a legacy token:\n%s", output)
	}
	if !strings.Contains(output, "could not be verified") {
		t.Fatalf("expected the device-path verify message:\n%s", output)
	}
}

func TestRunSetupNonInteractivePairedInstallHonorsDeviceCredentialWithoutKeyring(t *testing.T) {
	// Headless parity: a paired install on a box with no Secret Service must still
	// sync clients over the device transport, not fail on the legacy-token keyring
	// preflight (the paired-device check runs BEFORE that failure).
	withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	origPreflight := relayAuthTokenSetupPreflightForSetup
	relayAuthTokenSetupPreflightForSetup = func() error {
		return desktopKeyringSessionUnavailableError("no session bus in this shell")
	}
	origVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) bool { return false }
	t.Cleanup(func() {
		relayAuthTokenSetupPreflightForSetup = origPreflight
		verifyDeviceHealth = origVerify
	})

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	const validCred = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := writeDeviceCredential(validCred); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:             "192.168.1.5",
		HAURL:              "http://192.168.1.5:8123",
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	_, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{"antigravity", "--non-interactive"})
	})
	if strings.Contains(output, "missing relay auth token") ||
		strings.Contains(output, "secure storage unavailable") && strings.Contains(output, "save relay token") {
		t.Fatalf("headless paired install wrongly hit the legacy-token keyring gate:\n%s", output)
	}
	if !strings.Contains(output, "could not be verified") {
		t.Fatalf("expected the device-path verify message (paired check must precede the token preflight):\n%s", output)
	}
}
