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

func TestUninstallTokenLabelMatchesPlatform(t *testing.T) {
	if uninstallTokenLineLabel() == "" {
		t.Fatalf("expected %s uninstall to expose a token label", runtime.GOOS)
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
		return runUninstall(paths, []string{"--yes", "--purge"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	for _, want := range []string{
		"Removed: " + paths.InstallRoot,
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
		return runUninstall(paths, []string{"--yes", "--purge"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	for _, want := range []string{
		"HA NOVA Uninstall",
		"Standard remove:",
		uninstallRuntimeLineLabel(paths, installSourceBundle),
		"Managed local state and cache",
		"Full purge also removes:",
		"Home Assistant connection config",
		uninstallTokenLineLabel(),
		"Note: The NOVA Relay app is still running in Home Assistant.",
		"To remove it: Settings > Apps > NOVA Relay > Uninstall",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected uninstall output %q:\n%s", want, output)
		}
	}
}

func TestUninstallRuntimeLineLabelForWingetUsesChannelCopy(t *testing.T) {
	if got := uninstallRuntimeLineLabel(runtimePaths{}, installSourceWinget); got != "Installed CLI runtime (winget-managed package)" {
		t.Fatalf("unexpected winget runtime label: %q", got)
	}
}

func TestUninstallWindowsBundleNoteMentionsWait(t *testing.T) {
	got := uninstallWindowsBundleNote()
	if !strings.Contains(got, "Please wait a moment") {
		t.Fatalf("expected Windows bundle note to mention wait guidance, got %q", got)
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
		return runUninstall(paths, []string{"--yes", "--purge"})
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

func TestRunUninstallFailsLoudOnWindowsInstallChannelConflict(t *testing.T) {
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
		filepath.Dir(paths.PublicBinary),
		paths.ConfigDir,
		filepath.Dir(paths.UpdateCacheFile),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("bundle"), 0o755); err != nil {
		t.Fatalf("write bundle binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, "bundle.json"), []byte(`{"version":"0.3.0"}`), 0o644); err != nil {
		t.Fatalf("write bundle metadata: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("shim"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}
	if err := os.WriteFile(paths.UpdateCacheFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	cfg := runtimeConfig{
		HAHost: "192.168.1.5",
		HAURL:  "http://192.168.1.5:8123",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	state := installState{
		SchemaVersion: stateSchemaVersion,
		InstallSource: installSourceBundle,
	}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}
	wingetLink := windowsWingetLinkPath(home)
	if err := os.MkdirAll(filepath.Dir(wingetLink), 0o755); err != nil {
		t.Fatalf("mkdir winget link dir: %v", err)
	}
	if err := os.WriteFile(wingetLink, []byte("winget"), 0o755); err != nil {
		t.Fatalf("write winget link: %v", err)
	}

	originalPlatform := channelChecksUseWindowsPlatform
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
	}()
	channelChecksUseWindowsPlatform = func() bool { return true }

	cases := [][]string{
		{"--yes"},
		{"--yes", "--purge"},
	}
	for _, args := range cases {
		exitCode, output := captureCommandOutput(t, func() int {
			return runUninstall(paths, args)
		})
		if exitCode != 1 {
			t.Fatalf("runUninstall(%v) exit = %d, want 1\n%s", args, exitCode, output)
		}
		if !strings.Contains(output, "Windows install channel conflict") {
			t.Fatalf("expected conflict error for %v, got:\n%s", args, output)
		}
		if !strings.Contains(output, "Keep only one Windows install channel before running 'ha-nova uninstall'.") {
			t.Fatalf("expected uninstall guidance for %v, got:\n%s", args, output)
		}
		if _, err := os.Stat(paths.ConfigFile); err != nil {
			t.Fatalf("expected config to remain untouched for %v, got %v", args, err)
		}
		if _, err := os.Stat(paths.UpdateCacheFile); err != nil {
			t.Fatalf("expected update cache to remain untouched for %v, got %v", args, err)
		}
		if _, err := os.Stat(paths.StateFile); err != nil {
			t.Fatalf("expected state to remain untouched for %v, got %v", args, err)
		}
		if _, err := os.Stat(wingetLink); err != nil {
			t.Fatalf("expected winget link to remain untouched for %v, got %v", args, err)
		}
	}
}

func TestRunUninstallLeavesBundleRuntimeWhenLocalCleanupFails(t *testing.T) {
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
	runtimePath := filepath.Join(paths.InstallRoot, publicBinaryName())
	if err := os.WriteFile(runtimePath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write install binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, "bundle.json"), []byte(`{"version":"0.3.1"}`), 0o644); err != nil {
		t.Fatalf("write bundle metadata: %v", err)
	}
	if err := os.WriteFile(paths.UpdateCacheFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("shim"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
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
		return runUninstall(paths, []string{"--yes", "--purge"})
	})
	if exitCode != 1 {
		t.Fatalf("runUninstall() exit = %d, want 1\n%s", exitCode, output)
	}
	if _, err := os.Stat(runtimePath); err != nil {
		t.Fatalf("expected runtime to remain after failed local cleanup, got %v", err)
	}
}

func TestApplyUninstallTokenPolicySkipsHeadlessLinuxSecretServiceDeleteFailure(t *testing.T) {
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
		return relayAuthTokenReadError("ha-nova.relay-auth-token", errors.New("The name org.freedesktop.secrets was not provided by any .service files"))
	}

	report := &uninstallReport{}
	if err := applyUninstallTokenPolicy(report); err != nil {
		t.Fatalf("expected headless Secret Service delete failure to be tolerated, got %v", err)
	}
	if len(report.removed) != 0 {
		t.Fatalf("did not expect removals to be reported: %+v", report.removed)
	}
	if len(report.notes) != 1 || !strings.Contains(report.notes[0], "secure storage is unavailable") {
		t.Fatalf("expected secure-storage note, got %+v", report.notes)
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

func TestRunUninstallSkipsHeadlessLinuxSecretServiceDeleteFailureAfterRuntimeRemoval(t *testing.T) {
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
		return relayAuthTokenReadError("ha-nova.relay-auth-token", errors.New("The name org.freedesktop.secrets was not provided by any .service files"))
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes", "--purge"})
	})
	if exitCode != 0 {
		t.Fatalf("runUninstall() exit = %d\n%s", exitCode, output)
	}
	for _, removedPath := range []string{
		paths.InstallRoot,
		filepath.Dir(paths.UpdateCacheFile),
		paths.PublicBinary,
	} {
		if _, err := os.Stat(removedPath); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed despite token cleanup skip; err=%v", removedPath, err)
		}
	}
	if !strings.Contains(output, "Relay auth token cleanup skipped after runtime removal") {
		t.Fatalf("expected token cleanup skip note in output:\n%s", output)
	}
	if !strings.Contains(output, "Done. Removed") || !strings.Contains(output, "HA NOVA removed") {
		t.Fatalf("expected success summary after cleanup skip:\n%s", output)
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
		return runUninstall(paths, []string{"--yes", "--purge"})
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

func TestRunUninstallStandardRemoveKeepsConfigAndTokenForReinstall(t *testing.T) {
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
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"ha_host":"homeassistant.local","relay_base_url":"http://homeassistant.local:8791"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.StateFile, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
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
	if _, err := os.Stat(paths.ConfigFile); err != nil {
		t.Fatalf("expected config file to survive standard uninstall, got %v", err)
	}
	if _, err := os.Stat(paths.StateFile); !os.IsNotExist(err) {
		t.Fatalf("expected state file removed, got %v", err)
	}
	if _, err := os.Stat(paths.UpdateCacheFile); !os.IsNotExist(err) {
		t.Fatalf("expected update cache removed, got %v", err)
	}
	if token, err := readRelayAuthToken(); err != nil || token != "test-relay-token" {
		t.Fatalf("expected relay token to survive standard uninstall, got token=%q err=%v", token, err)
	}
	if !strings.Contains(output, "Kept Home Assistant connection config and stored relay token") {
		t.Fatalf("expected standard uninstall retention note:\n%s", output)
	}
}
