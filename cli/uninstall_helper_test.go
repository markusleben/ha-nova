package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubUninstallRelayTokenDeletion(t *testing.T, token string, deleteErr error) {
	t.Helper()

	originalRead := readRelayAuthTokenForUninstall
	originalDelete := deleteRelayAuthTokenForUninstall
	t.Cleanup(func() {
		readRelayAuthTokenForUninstall = originalRead
		deleteRelayAuthTokenForUninstall = originalDelete
	})

	readRelayAuthTokenForUninstall = func() (string, error) {
		return token, nil
	}
	deleteRelayAuthTokenForUninstall = func() error {
		return deleteErr
	}
}

func TestDiscardInstallRootRemovesVisibleInstallPath(t *testing.T) {
	parent := t.TempDir()
	installRoot := filepath.Join(parent, "ha-nova")
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "ha-nova.exe"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}

	if err := discardInstallRoot(installRoot); err != nil {
		t.Fatalf("discard install root: %v", err)
	}
	if _, err := os.Stat(installRoot); !isNotExist(err) {
		t.Fatalf("expected visible install root to be removed, got err=%v", err)
	}
}

func TestFinalizeWindowsUninstallRemovesInstallAndState(t *testing.T) {
	stubUninstallRelayTokenDeletion(t, "test-relay-token", nil)

	parent := t.TempDir()
	paths := runtimePaths{
		Home:            parent,
		InstallRoot:     filepath.Join(parent, ".local", "share", "ha-nova"),
		ConfigDir:       filepath.Join(parent, ".config", "ha-nova"),
		StateFile:       filepath.Join(parent, ".config", "ha-nova", "state.json"),
		UpdateCacheFile: filepath.Join(parent, ".cache", "ha-nova", "latest-release.json"),
	}
	if err := os.MkdirAll(paths.InstallRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, "ha-nova.exe"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.StateFile), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	stateData, err := json.Marshal(installState{
		SchemaVersion: 1,
		PathManaged:   false,
		PathTarget:    "user-path",
	})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(paths.StateFile, stateData, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.UpdateCacheFile), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(paths.UpdateCacheFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	if err := finalizeWindowsUninstall(paths, &uninstallReport{}, uninstallModeStandard, nil); err != nil {
		t.Fatalf("finalize windows uninstall: %v", err)
	}
	if _, err := os.Stat(paths.InstallRoot); !isNotExist(err) {
		t.Fatalf("expected install root removed, got err=%v", err)
	}
	if _, err := os.Stat(paths.ConfigDir); !isNotExist(err) {
		t.Fatalf("expected config dir removed, got err=%v", err)
	}
	if _, err := os.Stat(paths.UpdateCacheFile); !isNotExist(err) {
		t.Fatalf("expected update cache removed, got err=%v", err)
	}
}

func TestFinalizeWindowsUninstallWarnsAboutClaudeProjectMemoryArtifacts(t *testing.T) {
	stubUninstallRelayTokenDeletion(t, "test-relay-token", nil)

	home := t.TempDir()
	paths := runtimePaths{
		Home:            home,
		InstallRoot:     filepath.Join(home, ".local", "share", "ha-nova"),
		ConfigDir:       filepath.Join(home, ".config", "ha-nova"),
		StateFile:       filepath.Join(home, ".config", "ha-nova", "state.json"),
		UpdateCacheFile: filepath.Join(home, ".cache", "ha-nova", "latest-release.json"),
	}
	if err := os.MkdirAll(paths.InstallRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, "ha-nova.exe"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}

	projectMemoryDir := filepath.Join(home, ".claude", "projects", "project-a", "memory")
	if err := os.MkdirAll(projectMemoryDir, 0o755); err != nil {
		t.Fatalf("mkdir project memory: %v", err)
	}
	for _, path := range []string{
		filepath.Join(projectMemoryDir, "ha-nova-skills.md"),
		filepath.Join(projectMemoryDir, "MEMORY.md"),
	} {
		content := "# HA NOVA Project Memory\n\n## HA NOVA Skill System (see [ha-nova-skills.md](ha-nova-skills.md))\n- ha-nova:read\n"
		if filepath.Base(path) == "ha-nova-skills.md" {
			content = "# HA NOVA Skill System\n"
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	report := &uninstallReport{}
	if err := finalizeWindowsUninstall(paths, report, uninstallModeStandard, nil); err != nil {
		t.Fatalf("finalize windows uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectMemoryDir, "ha-nova-skills.md")); err != nil {
		t.Fatalf("expected ha-nova-skills.md to remain, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(projectMemoryDir, "MEMORY.md")); err != nil {
		t.Fatalf("expected dedicated MEMORY.md to remain, got err=%v", err)
	}
	if !strings.Contains(strings.Join(report.notes, "\n"), "Claude project memory may still mention HA NOVA") {
		t.Fatalf("expected report to mention Claude project memory warning: %+v", report.notes)
	}
}

func TestRunInternalUninstallPrintsFinalSuccess(t *testing.T) {
	stubUninstallRelayTokenDeletion(t, "test-relay-token", nil)

	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(paths.InstallRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}

	originalCleanup := scheduleWindowsSelfDeleteForUninstall
	defer func() {
		scheduleWindowsSelfDeleteForUninstall = originalCleanup
	}()
	cleanupPath := ""
	scheduleWindowsSelfDeleteForUninstall = func(path string) error {
		cleanupPath = path
		return nil
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runInternalUninstall(paths, []string{"--self-path", filepath.Join(home, "temp-helper.exe")})
	})
	if exitCode != 0 {
		t.Fatalf("runInternalUninstall() exit = %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "HA NOVA removed") {
		t.Fatalf("expected final uninstall success output:\n%s", output)
	}
	if strings.Contains(output, "If PowerShell is still waiting now, press Ctrl+C once to return to a fresh prompt.") {
		t.Fatalf("did not expect old console-coupled Ctrl+C guidance:\n%s", output)
	}
	if cleanupPath != filepath.Join(home, "temp-helper.exe") {
		t.Fatalf("expected helper cleanup to be scheduled, got %q", cleanupPath)
	}
}

func TestRunInternalUninstallPrintsPartialRemovalDetailsWhenTokenDeleteFails(t *testing.T) {
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
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	if err := os.WriteFile(paths.StateFile, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(paths.UpdateCacheFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write update cache: %v", err)
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
		return errors.New("credential manager unavailable")
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runInternalUninstall(paths, []string{"--self-path", filepath.Join(home, "temp-helper.exe"), "--purge"})
	})
	if exitCode == 0 {
		t.Fatalf("expected internal uninstall to fail when token deletion fails:\n%s", output)
	}
	if strings.Contains(output, "Removed: "+paths.InstallRoot) {
		t.Fatalf("runtime should remain until recovery can rerun uninstall:\n%s", output)
	}
	if !strings.Contains(output, "failed to remove relay auth token") {
		t.Fatalf("expected relay token deletion failure in output:\n%s", output)
	}
	if _, err := os.Stat(paths.InstallRoot); err != nil {
		t.Fatalf("expected install root to remain after failed helper cleanup, got %v", err)
	}
	marker, err := loadWindowsUninstallStatus(paths)
	if err != nil {
		t.Fatalf("loadWindowsUninstallStatus() error: %v", err)
	}
	if marker.Status != windowsUninstallStatusFailed {
		t.Fatalf("marker status = %q, want failed", marker.Status)
	}
}
