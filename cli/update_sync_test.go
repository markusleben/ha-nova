package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPostUpdateSyncRefreshesDetectedInstalledClientsWithoutState(t *testing.T) {
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
	writeClaudeMarketplaceFixture(t, paths.InstallRoot)

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := postUpdateSync(paths); err != nil {
		t.Fatalf("postUpdateSync() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logData), "plugin update ha-nova@ha-nova") {
		t.Fatalf("expected detected Claude install to be refreshed, got:\n%s", string(logData))
	}
	if !strings.Contains(string(logData), "plugin marketplace add ") {
		t.Fatalf("expected Claude marketplace to be refreshed, got:\n%s", string(logData))
	}

	state, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if !containsClient(state.InstalledClients, "claude") {
		t.Fatalf("expected saved state to include detected Claude install, got %+v", state.InstalledClients)
	}
}

func TestPostUpdateSyncRefreshesAllDetectedClients(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	t.Setenv("HA_NOVA_DEV_ROOT", filepath.Clean(filepath.Join(cwd, "..")))

	originalRuntimeDetected := clientRuntimeDetectedForStatus
	clientRuntimeDetectedForStatus = func(string) bool { return true }
	t.Cleanup(func() {
		clientRuntimeDetectedForStatus = originalRuntimeDetected
	})

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir Claude plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	if err := os.MkdirAll(filepath.Join(home, ".agents", "skills", "ha-nova", "ha-nova"), 0o755); err != nil {
		t.Fatalf("mkdir Codex attachment: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "skills", "ha-nova", "ha-nova", "SKILL.md"), []byte("name: ha-nova"), 0o644); err != nil {
		t.Fatalf("write Codex attachment: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".config", "opencode", "skills", "ha-nova", "ha-nova"), 0o755); err != nil {
		t.Fatalf("mkdir OpenCode attachment: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "opencode", "skills", "ha-nova", "ha-nova", "SKILL.md"), []byte("name: ha-nova"), 0o644); err != nil {
		t.Fatalf("write OpenCode attachment: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".gemini", "skills", "ha-nova"), 0o755); err != nil {
		t.Fatalf("mkdir Gemini context skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gemini", "skills", "ha-nova", "SKILL.md"), []byte("name: ha-nova"), 0o644); err != nil {
		t.Fatalf("write Gemini context skill: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "skills", "ha-nova-review"), 0o755); err != nil {
		t.Fatalf("mkdir Gemini review skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gemini", "skills", "ha-nova-review", "SKILL.md"), []byte("name: ha-nova-review"), 0o644); err != nil {
		t.Fatalf("write Gemini review skill: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := postUpdateSync(paths); err != nil {
		t.Fatalf("postUpdateSync() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logData), "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected detected Claude install to be refreshed, got:\n%s", string(logData))
	}
	if !strings.Contains(string(logData), "plugin marketplace add ") {
		t.Fatalf("expected Claude marketplace to be refreshed, got:\n%s", string(logData))
	}

	state, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	for _, client := range []string{"claude", "codex", "opencode", "gemini"} {
		if !containsClient(state.InstalledClients, client) {
			t.Fatalf("expected saved state to include %s, got %+v", client, state.InstalledClients)
		}
	}

	if _, err := os.Stat(filepath.Join(home, ".gemini", "skills", "ha-nova-review", "SKILL.md")); err != nil {
		t.Fatalf("expected Gemini skill refresh output: %v", err)
	}

	codexInfo, err := os.Lstat(filepath.Join(home, ".agents", "skills", "ha-nova"))
	if err != nil {
		t.Fatalf("expected Codex skill tree: %v", err)
	}
	if runtime.GOOS != "windows" && codexInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected Codex install to prefer symlink on %s", runtime.GOOS)
	}

	opencodeInfo, err := os.Lstat(filepath.Join(home, ".config", "opencode", "skills", "ha-nova"))
	if err != nil {
		t.Fatalf("expected OpenCode skill tree: %v", err)
	}
	if runtime.GOOS != "windows" && opencodeInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected OpenCode install to prefer symlink on %s", runtime.GOOS)
	}
}

func TestPostUpdateSyncContinuesOtherClientsAfterClaudeFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	t.Setenv("HA_NOVA_DEV_ROOT", filepath.Clean(filepath.Join(cwd, "..")))
	t.Setenv("HA_NOVA_TEST_CLAUDE_INSTALL_FAIL", "1")

	originalRuntimeDetected := clientRuntimeDetectedForStatus
	clientRuntimeDetectedForStatus = func(string) bool { return true }
	t.Cleanup(func() {
		clientRuntimeDetectedForStatus = originalRuntimeDetected
	})

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	state := installState{
		SchemaVersion:    stateSchemaVersion,
		InstalledClients: []string{"claude", "codex"},
	}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	err = postUpdateSync(paths)
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("expected Claude failure summary, got %v", err)
	}

	codexPath := filepath.Join(home, ".agents", "skills", "ha-nova")
	if _, err := os.Lstat(codexPath); err != nil {
		t.Fatalf("expected Codex sync to continue, stat error: %v", err)
	}

	saved, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if !containsClient(saved.InstalledClients, "claude") {
		t.Fatalf("expected failed Claude client to remain configured for retry, got %+v", saved.InstalledClients)
	}
}

func TestRunUpdateAlreadyCurrentRetriesInstalledClientSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	t.Setenv("HA_NOVA_DEV_ROOT", filepath.Clean(filepath.Join(cwd, "..")))

	originalRuntimeDetected := clientRuntimeDetectedForStatus
	clientRuntimeDetectedForStatus = func(string) bool { return true }
	t.Cleanup(func() {
		clientRuntimeDetectedForStatus = originalRuntimeDetected
	})

	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.2.2","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HA_NOVA_TEST_CLAUDE_INSTALL_FAIL", "1")

	state := installState{
		SchemaVersion:    stateSchemaVersion,
		InstalledClients: []string{"claude", "codex"},
	}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	if exitCode := runUpdate(paths, []string{"--version", "0.2.2"}); exitCode != 1 {
		t.Fatalf("runUpdate() first exit = %d, want 1", exitCode)
	}

	t.Setenv("HA_NOVA_TEST_CLAUDE_INSTALL_FAIL", "0")
	if exitCode := runUpdate(paths, []string{"--version", "0.2.2"}); exitCode != 0 {
		t.Fatalf("runUpdate() second exit = %d, want 0", exitCode)
	}

	if !claudePluginInstalled(home) {
		t.Fatal("expected Claude plugin to be installed after retry")
	}
}

func TestApplyStagedBundleWithRollbackRestoresPreviousInstallRoot(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("old-runtime"), 0o755); err != nil {
		t.Fatalf("write old runtime: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("old-link"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}

	stageRoot := filepath.Join(t.TempDir(), "stage")
	if err := os.MkdirAll(stageRoot, 0o755); err != nil {
		t.Fatalf("mkdir stage root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stageRoot, publicBinaryName()), []byte("new-runtime"), 0o755); err != nil {
		t.Fatalf("write new runtime: %v", err)
	}
	metadata := fmt.Sprintf(`{"os":%q,"arch":%q,"binary_name":%q,"version":"0.1.12"}`, bundlePlatformOS(), bundlePlatformArch(), publicBinaryName())
	if err := os.WriteFile(filepath.Join(stageRoot, "bundle.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write bundle metadata: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stageRoot, "clients"), 0o755); err != nil {
		t.Fatalf("mkdir clients: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stageRoot, "clients", "registry.json"), []byte(`{"clients":[{"id":"claude","label":"Claude Code","adapter_kind":"plugin_marketplace","supported_os":["macos","linux","windows"]}]}`), 0o644); err != nil {
		t.Fatalf("write registry.json: %v", err)
	}

	rollback, _, err := applyStagedBundleWithRollback(paths, stageRoot)
	if err != nil {
		t.Fatalf("applyStagedBundleWithRollback() error: %v", err)
	}
	if err := rollback(); err != nil {
		t.Fatalf("rollback() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(paths.InstallRoot, publicBinaryName()))
	if err != nil {
		t.Fatalf("read restored runtime: %v", err)
	}
	if string(data) != "old-runtime" {
		t.Fatalf("expected previous runtime restored, got %q", string(data))
	}
}
