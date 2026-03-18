package main

import (
	"fmt"
	"os"
	"path/filepath"
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

	state, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if !containsClient(state.InstalledClients, "claude") {
		t.Fatalf("expected saved state to include detected Claude install, got %+v", state.InstalledClients)
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
