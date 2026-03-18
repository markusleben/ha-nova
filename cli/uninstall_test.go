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

func TestShouldDeleteRelayAuthTokenOnUninstallFollowsPlatformPolicy(t *testing.T) {
	got := shouldDeleteRelayAuthTokenOnUninstall()
	if !got {
		t.Fatalf("expected %s uninstall to delete the stored relay token", runtime.GOOS)
	}
}

func TestRunUninstallReportsConcreteRemovalsAndTokenPolicy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	for _, path := range []string{
		paths.InstallRoot,
		paths.ConfigDir,
		filepath.Dir(paths.UpdateCacheFile),
		filepath.Dir(paths.PublicBinary),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write install binary: %v", err)
	}
	if err := os.WriteFile(paths.UpdateCacheFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("shim"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	for _, want := range []string{
		"Removed: " + paths.InstallRoot,
		"Removed: " + paths.ConfigDir,
		"Removed: " + filepath.Dir(paths.UpdateCacheFile),
		"Removed: " + paths.PublicBinary,
		"HA NOVA removed",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("uninstall output missing %q:\n%s", want, output)
		}
	}

	if !strings.Contains(output, "Removed: relay auth token") {
		t.Fatalf("expected token removal output:\n%s", output)
	}
}

func TestRunUninstallShowsPreflightAndRelayStillRunningNote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-relay-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	for _, path := range []string{
		paths.InstallRoot,
		paths.ConfigDir,
		filepath.Dir(paths.UpdateCacheFile),
		filepath.Dir(paths.PublicBinary),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write install binary: %v", err)
	}
	if err := os.WriteFile(paths.UpdateCacheFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("shim"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}
	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: relayServer.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	for _, want := range []string{
		"HA NOVA Uninstall",
		"This will remove:",
		"Local install (~/.local/share/ha-nova)",
		uninstallCLILineLabel(),
		"Managed config files (~/.config/ha-nova/)",
		"Managed cache files (~/.cache/ha-nova/)",
		uninstallTokenLineLabel(),
		"Note: The NOVA Relay app is still running in Home Assistant.",
		"To remove it: Settings > Apps > NOVA Relay > Uninstall",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected uninstall output %q:\n%s", want, output)
		}
	}
}

func TestUninstallCLILineLabelForWindowsUsesInstalledBinaryPath(t *testing.T) {
	if got := uninstallCLILineLabelForOS("windows"); got != "Installed CLI binary (~/.local/share/ha-nova/ha-nova.exe)" {
		t.Fatalf("unexpected Windows uninstall CLI label: %q", got)
	}
}

func TestPreflightNoteLinesIncludeSecureStoreWarningWhenTokenLookupFails(t *testing.T) {
	notes := preflightNoteLines(uninstallPreflight{tokenUnavailable: "relay auth token unavailable: keychain locked"})
	if len(notes) != 1 || notes[0] != "relay auth token unavailable: keychain locked" {
		t.Fatalf("unexpected preflight notes: %#v", notes)
	}
}

func TestRunUninstallNoopDoesNotClaimRemoval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Nothing to remove — HA NOVA was not installed.") {
		t.Fatalf("expected noop uninstall output:\n%s", output)
	}
	if strings.Contains(output, "HA NOVA removed") {
		t.Fatalf("did not expect final removal claim for noop uninstall:\n%s", output)
	}
}

func TestApplyUninstallTokenPolicyFailsLoudWhenDeleteFails(t *testing.T) {
	originalRead := readRelayAuthTokenForUninstall
	originalDelete := deleteRelayAuthTokenForUninstall
	defer func() {
		readRelayAuthTokenForUninstall = originalRead
		deleteRelayAuthTokenForUninstall = originalDelete
	}()

	readRelayAuthTokenForUninstall = func() (string, error) {
		return "test-relay-token", nil
	}
	deleteRelayAuthTokenForUninstall = func() error {
		return errors.New("secure store unavailable")
	}

	report := &uninstallReport{}
	err := applyUninstallTokenPolicy(report)
	if err == nil || !strings.Contains(err.Error(), "secure store unavailable") {
		t.Fatalf("expected token deletion failure, got %v", err)
	}
	if len(report.removed) != 0 {
		t.Fatalf("did not expect token removal to be reported on failure: %+v", report.removed)
	}
}

func TestApplyUninstallTokenPolicySkipsHeadlessLinuxSecretServiceReadFailure(t *testing.T) {
	originalRead := readRelayAuthTokenForUninstall
	originalDelete := deleteRelayAuthTokenForUninstall
	defer func() {
		readRelayAuthTokenForUninstall = originalRead
		deleteRelayAuthTokenForUninstall = originalDelete
	}()

	readRelayAuthTokenForUninstall = func() (string, error) {
		return "", relayAuthTokenReadError("ha-nova.relay-auth-token", errors.New("The name org.freedesktop.secrets was not provided by any .service files"))
	}
	deleteRelayAuthTokenForUninstall = func() error {
		t.Fatal("did not expect token delete when keyring is unavailable")
		return nil
	}

	report := &uninstallReport{}
	if err := applyUninstallTokenPolicy(report); err != nil {
		t.Fatalf("expected headless Secret Service read failure to be tolerated, got %v", err)
	}
	if len(report.removed) != 0 {
		t.Fatalf("did not expect removals to be reported: %+v", report.removed)
	}
	if len(report.notes) != 1 || !strings.Contains(report.notes[0], "secure storage is unavailable") {
		t.Fatalf("expected secure-storage note, got %+v", report.notes)
	}
}

func TestApplyUninstallTokenPolicyFailsLoudWhenReadFailsForOtherReasons(t *testing.T) {
	originalRead := readRelayAuthTokenForUninstall
	originalDelete := deleteRelayAuthTokenForUninstall
	defer func() {
		readRelayAuthTokenForUninstall = originalRead
		deleteRelayAuthTokenForUninstall = originalDelete
	}()

	readRelayAuthTokenForUninstall = func() (string, error) {
		return "", relayAuthTokenReadError("ha-nova.relay-auth-token", errors.New("Secret Service backend locked"))
	}
	deleteRelayAuthTokenForUninstall = func() error {
		t.Fatal("did not expect token delete when read failed")
		return nil
	}

	report := &uninstallReport{}
	err := applyUninstallTokenPolicy(report)
	if err == nil || !strings.Contains(err.Error(), "Secret Service backend locked") {
		t.Fatalf("expected generic keyring read failure, got %v", err)
	}
	if len(report.removed) != 0 {
		t.Fatalf("did not expect removals to be reported: %+v", report.removed)
	}
	if len(report.notes) != 0 {
		t.Fatalf("did not expect notes on hard failure: %+v", report.notes)
	}
}

func TestRunUninstallContinuesRemovingFilesWhenTokenDeleteFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	for _, path := range []string{
		paths.InstallRoot,
		paths.ConfigDir,
		filepath.Dir(paths.UpdateCacheFile),
		filepath.Dir(paths.PublicBinary),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write install binary: %v", err)
	}
	if err := os.WriteFile(paths.UpdateCacheFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("shim"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	originalRead := readRelayAuthTokenForUninstall
	originalDelete := deleteRelayAuthTokenForUninstall
	defer func() {
		readRelayAuthTokenForUninstall = originalRead
		deleteRelayAuthTokenForUninstall = originalDelete
	}()
	readRelayAuthTokenForUninstall = func() (string, error) {
		return "test-relay-token", nil
	}
	deleteRelayAuthTokenForUninstall = func() error {
		return errors.New("secure store unavailable")
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 1 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	for _, removedPath := range []string{
		paths.InstallRoot,
		paths.ConfigDir,
		filepath.Dir(paths.UpdateCacheFile),
		paths.PublicBinary,
	} {
		if _, err := os.Stat(removedPath); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed despite token failure; err=%v", removedPath, err)
		}
	}
	if !strings.Contains(output, "failed to remove relay auth token") {
		t.Fatalf("expected final token failure in output:\n%s", output)
	}
	if strings.Contains(output, "Done. Removed") || strings.Contains(output, "HA NOVA removed") {
		t.Fatalf("did not expect success summary on token-delete failure:\n%s", output)
	}
}

func TestRunUninstallPreservesUnknownConfigAndCacheFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	for _, path := range []string{
		paths.InstallRoot,
		paths.ConfigDir,
		filepath.Dir(paths.UpdateCacheFile),
		filepath.Dir(paths.PublicBinary),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write install binary: %v", err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"ha_host":"homeassistant.local"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.UpdateCacheFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("shim"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}
	customConfig := filepath.Join(paths.ConfigDir, "custom.txt")
	if err := os.WriteFile(customConfig, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write custom config file: %v", err)
	}
	customCache := filepath.Join(paths.CacheDir, "custom-cache.txt")
	if err := os.WriteFile(customCache, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write custom cache file: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	if _, err := os.Stat(customConfig); err != nil {
		t.Fatalf("expected custom config file to survive uninstall, got %v", err)
	}
	if _, err := os.Stat(customCache); err != nil {
		t.Fatalf("expected custom cache file to survive uninstall, got %v", err)
	}
	if _, err := os.Stat(paths.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("expected managed config file removed, got %v", err)
	}
	if _, err := os.Stat(paths.UpdateCacheFile); !os.IsNotExist(err) {
		t.Fatalf("expected managed cache file removed, got %v", err)
	}
	if strings.Contains(output, "\n[ha-nova] Removed: "+paths.ConfigDir+"\n") || strings.Contains(output, "\n[ha-nova] Removed: "+paths.CacheDir+"\n") {
		t.Fatalf("did not expect config/cache dir removal when custom files remain:\n%s", output)
	}
}
