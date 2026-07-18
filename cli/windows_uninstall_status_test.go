package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func enableWindowsUninstallStatusChecks(t *testing.T) {
	t.Helper()

	originalEnabled := windowsUninstallStatusChecksEnabled
	originalNow := windowsUninstallStatusNow
	originalAlive := windowsUninstallStatusProcessAlive
	t.Cleanup(func() {
		windowsUninstallStatusChecksEnabled = originalEnabled
		windowsUninstallStatusNow = originalNow
		windowsUninstallStatusProcessAlive = originalAlive
	})

	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	windowsUninstallStatusChecksEnabled = func() bool { return true }
	windowsUninstallStatusNow = func() time.Time { return now }
	windowsUninstallStatusProcessAlive = func(pid int) bool { return pid == 4242 }
}

func TestInspectWindowsUninstallStatusTreatsSuccessMarkerAsTransient(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	root := t.TempDir()
	paths := runtimePaths{UninstallStatusFile: filepath.Join(root, "ha-nova", "uninstall-status.json")}
	if err := writeWindowsUninstallStatus(paths, windowsUninstallStatus{
		Status: windowsUninstallStatusSuccess,
		Mode:   string(uninstallModeStandard),
	}); err != nil {
		t.Fatalf("writeWindowsUninstallStatus() error: %v", err)
	}

	inspection := inspectWindowsUninstallStatus(paths)
	if inspection.Kind != windowsUninstallStatusKindNone {
		t.Fatalf("inspectWindowsUninstallStatus() kind = %q, want none", inspection.Kind)
	}
	if _, err := os.Stat(paths.UninstallStatusFile); !isNotExist(err) {
		t.Fatalf("expected success marker to be removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Dir(paths.UninstallStatusFile)); !isNotExist(err) {
		t.Fatalf("expected empty local data dir to be removed, got err=%v", err)
	}
}

func TestInspectWindowsUninstallStatusTreatsStaleRunningAsInterrupted(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	root := t.TempDir()
	paths := runtimePaths{
		InstallRoot:         filepath.Join(root, "Programs", "ha-nova"),
		PublicBinary:        filepath.Join(root, "Programs", "ha-nova", publicBinaryName()),
		UninstallStatusFile: filepath.Join(root, "uninstall-status.json"),
	}
	if err := os.MkdirAll(paths.InstallRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	if err := writeWindowsUninstallStatus(paths, windowsUninstallStatus{
		Status:        windowsUninstallStatusRunning,
		Mode:          string(uninstallModePurge),
		HelperPID:     7,
		StartedAt:     windowsUninstallStatusNow().Add(-15 * time.Minute),
		LastUpdatedAt: windowsUninstallStatusNow().Add(-15 * time.Minute),
	}); err != nil {
		t.Fatalf("writeWindowsUninstallStatus() error: %v", err)
	}

	inspection := inspectWindowsUninstallStatus(paths)
	if inspection.Kind != windowsUninstallStatusKindInterrupted {
		t.Fatalf("inspectWindowsUninstallStatus() kind = %q, want interrupted", inspection.Kind)
	}
	if inspection.RecoveryCommand != "ha-nova uninstall --yes --purge" {
		t.Fatalf("unexpected recovery command: %q", inspection.RecoveryCommand)
	}
}

func TestInspectWindowsUninstallStatusTreatsAliveButStaleMarkerAsInterrupted(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	root := t.TempDir()
	paths := runtimePaths{
		InstallRoot:         filepath.Join(root, "Programs", "ha-nova"),
		PublicBinary:        filepath.Join(root, "Programs", "ha-nova", publicBinaryName()),
		UninstallStatusFile: filepath.Join(root, "uninstall-status.json"),
	}
	if err := os.MkdirAll(paths.InstallRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	if err := writeWindowsUninstallStatus(paths, windowsUninstallStatus{
		Status:        windowsUninstallStatusRunning,
		Mode:          string(uninstallModePurge),
		HelperPID:     4242,
		StartedAt:     windowsUninstallStatusNow().Add(-30 * time.Minute),
		LastUpdatedAt: windowsUninstallStatusNow().Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("writeWindowsUninstallStatus() error: %v", err)
	}

	inspection := inspectWindowsUninstallStatus(paths)
	if inspection.Kind != windowsUninstallStatusKindInterrupted {
		t.Fatalf("inspectWindowsUninstallStatus() kind = %q, want interrupted", inspection.Kind)
	}
}

func TestWindowsUninstallHeartbeatRefreshesStatusMarker(t *testing.T) {
	root := t.TempDir()
	paths := runtimePaths{UninstallStatusFile: filepath.Join(root, "uninstall-status.json")}

	originalNow := windowsUninstallStatusNow
	originalInterval := windowsUninstallHeartbeatInterval
	t.Cleanup(func() {
		windowsUninstallStatusNow = originalNow
		windowsUninstallHeartbeatInterval = originalInterval
	})

	current := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	windowsUninstallStatusNow = func() time.Time {
		current = current.Add(time.Second)
		return current
	}
	windowsUninstallHeartbeatInterval = 5 * time.Millisecond

	status, err := beginWindowsUninstallStatus(paths, uninstallModeStandard, installSourceBundle)
	if err != nil {
		t.Fatalf("beginWindowsUninstallStatus() error: %v", err)
	}
	initial := status.LastUpdatedAt

	stopHeartbeat := startWindowsUninstallHeartbeat(paths, status)
	time.Sleep(20 * time.Millisecond)
	stopHeartbeat()

	marker, err := loadWindowsUninstallStatus(paths)
	if err != nil {
		t.Fatalf("loadWindowsUninstallStatus() error: %v", err)
	}
	if !marker.LastUpdatedAt.After(initial) {
		t.Fatalf("expected heartbeat to refresh last_updated_at, got %s <= %s", marker.LastUpdatedAt, initial)
	}
}

func TestInspectWindowsUninstallStatusTreatsCorruptJSONAsRecoveryState(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	root := t.TempDir()
	paths := runtimePaths{UninstallStatusFile: filepath.Join(root, "uninstall-status.json")}
	if err := os.WriteFile(paths.UninstallStatusFile, []byte("{"), 0o600); err != nil {
		t.Fatalf("write corrupt marker: %v", err)
	}

	inspection := inspectWindowsUninstallStatus(paths)
	if inspection.Kind != windowsUninstallStatusKindCorrupt {
		t.Fatalf("inspectWindowsUninstallStatus() kind = %q, want corrupt", inspection.Kind)
	}
	if inspection.RecoveryCommand != "ha-nova uninstall --yes" {
		t.Fatalf("unexpected recovery command: %q", inspection.RecoveryCommand)
	}
}

func TestInspectWindowsUninstallStatusKeepsFailedMarkerVisibleWithoutRuntime(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	root := t.TempDir()
	paths := runtimePaths{
		Home:                root,
		InstallRoot:         filepath.Join(root, "Programs", "ha-nova"),
		PublicBinary:        filepath.Join(root, "Programs", "ha-nova", publicBinaryName()),
		UninstallStatusFile: filepath.Join(root, "ha-nova", "uninstall-status.json"),
	}
	if err := writeWindowsUninstallStatus(paths, windowsUninstallStatus{
		Status:        windowsUninstallStatusFailed,
		Mode:          string(uninstallModePurge),
		InstallSource: installSourceBundle,
		InstallRoot:   paths.InstallRoot,
		ErrorSummary:  "HA NOVA uninstall could not remove the stored relay token.",
	}); err != nil {
		t.Fatalf("writeWindowsUninstallStatus() error: %v", err)
	}

	inspection := inspectWindowsUninstallStatus(paths)
	if inspection.Kind != windowsUninstallStatusKindFailed {
		t.Fatalf("inspectWindowsUninstallStatus() kind = %q, want failed", inspection.Kind)
	}
	if inspection.Summary != "HA NOVA uninstall could not remove the stored relay token." {
		t.Fatalf("unexpected summary: %q", inspection.Summary)
	}
	if _, err := os.Stat(paths.UninstallStatusFile); err != nil {
		t.Fatalf("expected failed marker to remain visible, got err=%v", err)
	}
}

func TestCollectWindowsUninstallRemainingPathsSkipsKeptConfigDirForStandardMode(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	root := t.TempDir()
	paths := runtimePaths{
		ConfigDir:       filepath.Join(root, "AppData", "Roaming", "ha-nova"),
		ConfigFile:      filepath.Join(root, "AppData", "Roaming", "ha-nova", "config.json"),
		StateFile:       filepath.Join(root, "AppData", "Roaming", "ha-nova", "state.json"),
		CacheDir:        filepath.Join(root, "AppData", "Local", "ha-nova", "cache"),
		UpdateCacheFile: filepath.Join(root, "AppData", "Local", "ha-nova", "cache", "latest-release.json"),
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	remaining := collectWindowsUninstallRemainingPaths(paths, uninstallModeStandard, installSourceBundle)
	for _, candidate := range remaining {
		if filepath.Clean(candidate) == filepath.Clean(paths.ConfigDir) {
			t.Fatalf("standard uninstall residue unexpectedly included kept config dir: %v", remaining)
		}
	}
}

func TestRunDoctorBlocksOnFailedWindowsUninstallStatus(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	paths, _ := doctorTestSetup(t)
	if err := os.MkdirAll(paths.InstallRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.PublicBinary), 0o755); err != nil {
		t.Fatalf("mkdir public binary dir: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write install binary: %v", err)
	}
	if err := writeWindowsUninstallStatus(paths, windowsUninstallStatus{
		Status:       windowsUninstallStatusFailed,
		Mode:         string(uninstallModePurge),
		ErrorSummary: "HA NOVA uninstall could not remove the stored relay token.",
	}); err != nil {
		t.Fatalf("writeWindowsUninstallStatus() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode != 1 {
		t.Fatalf("runDoctor() exit = %d, want 1\n%s", exitCode, output)
	}
	if !strings.Contains(output, "HA NOVA uninstall could not remove the stored relay token.") {
		t.Fatalf("expected failed uninstall summary:\n%s", output)
	}
	if !strings.Contains(output, "Recovery: run `ha-nova uninstall --yes --purge`.") {
		t.Fatalf("expected purge recovery hint:\n%s", output)
	}
	if strings.Contains(output, "Doctor checks passed") {
		t.Fatalf("doctor should not claim success while uninstall recovery is pending:\n%s", output)
	}
}

func TestRunUpdateBlocksOnFailedWindowsUninstallStatus(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(paths.InstallRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.PublicBinary), 0o755); err != nil {
		t.Fatalf("mkdir public binary dir: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write install binary: %v", err)
	}
	if err := writeWindowsUninstallStatus(paths, windowsUninstallStatus{
		Status:       windowsUninstallStatusFailed,
		Mode:         string(uninstallModeStandard),
		ErrorSummary: "HA NOVA uninstall could not finish PATH cleanup.",
	}); err != nil {
		t.Fatalf("writeWindowsUninstallStatus() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUpdate(paths, nil)
	})
	if exitCode != 1 {
		t.Fatalf("runUpdate() exit = %d, want 1\n%s", exitCode, output)
	}
	if !strings.Contains(output, "HA NOVA uninstall could not finish PATH cleanup.") {
		t.Fatalf("expected uninstall blocker summary:\n%s", output)
	}
	if !strings.Contains(output, "Recovery: run `ha-nova uninstall --yes` first.") {
		t.Fatalf("expected recovery hint:\n%s", output)
	}
}

func TestRunUninstallBlocksWrongRecoveryMode(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(paths.InstallRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.PublicBinary), 0o755); err != nil {
		t.Fatalf("mkdir public binary dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write install binary: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}
	if err := writeWindowsUninstallStatus(paths, windowsUninstallStatus{
		Status:       windowsUninstallStatusFailed,
		Mode:         string(uninstallModePurge),
		ErrorSummary: "HA NOVA uninstall could not remove the stored relay token.",
	}); err != nil {
		t.Fatalf("writeWindowsUninstallStatus() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 1 {
		t.Fatalf("runUninstall() exit = %d, want 1\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Recovery: run `ha-nova uninstall --yes --purge`.") {
		t.Fatalf("expected exact recovery command:\n%s", output)
	}
}

func TestRunUninstallBlocksStandardRecoveryAfterPurgeTokenFailure(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	exitCode, output := captureCommandOutput(t, func() int {
		return handleWindowsUninstallRecovery(windowsUninstallStatusInspection{
			Kind:    windowsUninstallStatusKindFailed,
			Summary: "HA NOVA uninstall could not remove the stored relay token.",
			Status: windowsUninstallStatus{
				Mode:        string(uninstallModePurge),
				FailingStep: "token_cleanup",
			},
			RecoveryCommand: "ha-nova uninstall --yes --purge",
		}, uninstallModeStandard)
	})
	if exitCode != 1 {
		t.Fatalf("runUninstall() exit = %d, want 1\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Recovery: run `ha-nova uninstall --yes --purge`.") {
		t.Fatalf("expected purge recovery hint:\n%s", output)
	}
}

func TestRunUninstallBlocksPurgeRecoveryWhenMarkerIsCorrupt(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	exitCode, output := captureCommandOutput(t, func() int {
		return handleWindowsUninstallRecovery(windowsUninstallStatusInspection{
			Kind:            windowsUninstallStatusKindCorrupt,
			Summary:         "A previous background HA NOVA uninstall left an unreadable recovery marker.",
			RecoveryCommand: "ha-nova uninstall --yes",
		}, uninstallModePurge)
	})
	if exitCode != 1 {
		t.Fatalf("runUninstall() exit = %d, want 1\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Recovery: run `ha-nova uninstall --yes`.") {
		t.Fatalf("expected standard corrupt-marker recovery hint:\n%s", output)
	}
}

func TestFinalizeWindowsUninstallLeavesRuntimeWhenRecoveryFails(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)
	stubUninstallRelayTokenDeletion(t, "test-relay-token", assertError("credential manager unavailable"))

	parent := t.TempDir()
	paths := runtimePaths{
		Home:                parent,
		InstallRoot:         filepath.Join(parent, ".local", "share", "ha-nova"),
		PublicBinary:        filepath.Join(parent, ".local", "bin", "ha-nova"),
		ConfigDir:           filepath.Join(parent, ".config", "ha-nova"),
		ConfigFile:          filepath.Join(parent, ".config", "ha-nova", "config.json"),
		StateFile:           filepath.Join(parent, ".config", "ha-nova", "state.json"),
		CacheDir:            filepath.Join(parent, ".cache", "ha-nova"),
		UpdateCacheFile:     filepath.Join(parent, ".cache", "ha-nova", "latest-release.json"),
		UninstallStatusFile: filepath.Join(parent, ".cache", "ha-nova", "uninstall-status.json"),
	}
	for _, path := range []string{paths.InstallRoot, filepath.Dir(paths.PublicBinary), paths.ConfigDir, filepath.Dir(paths.UpdateCacheFile)} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, "ha-nova.exe"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("shim"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}
	if err := os.WriteFile(paths.StateFile, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(paths.UpdateCacheFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write update cache: %v", err)
	}

	status, err := beginWindowsUninstallStatus(paths, uninstallModePurge, installSourceBundle)
	if err != nil {
		t.Fatalf("beginWindowsUninstallStatus() error: %v", err)
	}
	err = finalizeWindowsUninstall(paths, &uninstallReport{}, uninstallModePurge, status, false)
	if err == nil {
		t.Fatalf("expected finalizeWindowsUninstall() to fail")
	}
	if _, statErr := os.Stat(paths.InstallRoot); statErr != nil {
		t.Fatalf("expected install root to remain for recovery, got %v", statErr)
	}
	marker, err := loadWindowsUninstallStatus(paths)
	if err != nil {
		t.Fatalf("loadWindowsUninstallStatus() error: %v", err)
	}
	if marker.Status != windowsUninstallStatusFailed {
		t.Fatalf("marker status = %q, want failed", marker.Status)
	}
	if marker.FailingStep != "token_cleanup" {
		t.Fatalf("marker failing step = %q, want token_cleanup", marker.FailingStep)
	}
	if len(marker.RemainingPaths) == 0 {
		t.Fatalf("expected remaining paths in failed marker")
	}
}

func TestWindowsUninstallStatusMarkerTicksUsesDotNetTicks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "uninstall-status.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat marker: %v", err)
	}

	got := windowsUninstallStatusMarkerTicks(path)
	want := info.ModTime().UTC().UnixNano()/100 + windowsDotNetEpochTicks
	if got != want {
		t.Fatalf("windowsUninstallStatusMarkerTicks() = %d, want %d", got, want)
	}
}

func assertError(message string) error {
	return &staticError{message: message}
}

type staticError struct {
	message string
}

func (e *staticError) Error() string {
	return e.message
}
