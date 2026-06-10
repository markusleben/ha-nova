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
	withClientRuntimeAvailability(t, map[string]bool{"gemini": true})
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
			"gemini",
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
	if _, err := os.Stat(filepath.Join(home, ".gemini", "skills", "ha-nova", "SKILL.md")); !isNotExist(err) {
		t.Fatalf("expected gemini skills not to be installed on failed readiness, err=%v", err)
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
	withClientRuntimeAvailability(t, map[string]bool{"gemini": true})
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
			"gemini",
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
	withClientRuntimeAvailability(t, map[string]bool{"gemini": true})
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
			"gemini",
		})
	})
	if exitCode == 0 {
		t.Fatalf("expected service setup to fail for client without registry capability:\n%s", output)
	}
	if !strings.Contains(output, "Gemini CLI does not support service credentials") {
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
