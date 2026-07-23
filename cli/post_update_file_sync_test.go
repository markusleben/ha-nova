package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPostUpdateSyncRefreshesAttachedFileClientsWithoutRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Hermes is WSL-only and this fixture covers all file adapter layouts together")
	}
	paths := setupHealableInstall(t)
	sourceRoot := paths.InstallRoot

	for _, installedDir := range hermesRequiredSkillDirs {
		if installedDir == "ha-nova" {
			continue
		}
		sourceSkill := strings.TrimPrefix(installedDir, "ha-nova-")
		writeBundleTestFile(
			t,
			filepath.Join(sourceRoot, "skills", sourceSkill, "SKILL.md"),
			"name: "+sourceSkill+"\n",
			0o644,
		)
	}
	if _, err := installTreeClient(filepath.Join(paths.Home, ".agents", "skills"), filepath.Join(sourceRoot, "skills"), false); err != nil {
		t.Fatalf("seed copied Codex skills: %v", err)
	}
	if _, err := installTreeClient(filepath.Join(paths.Home, ".config", "opencode", "skills"), filepath.Join(sourceRoot, "skills"), false); err != nil {
		t.Fatalf("seed copied OpenCode skills: %v", err)
	}
	if err := installAntigravityClient(paths.Home, sourceRoot); err != nil {
		t.Fatalf("seed Antigravity skills: %v", err)
	}
	if err := installHermesClient(paths.Home, sourceRoot); err != nil {
		t.Fatalf("seed Hermes skills: %v", err)
	}

	bootstrapPaths := []string{
		filepath.Join(paths.Home, ".agents", "skills", "ha-nova", "ha-nova", "session-bootstrap.md"),
		filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova", "ha-nova", "session-bootstrap.md"),
		filepath.Join(paths.Home, ".gemini", "config", "skills", "ha-nova", "session-bootstrap.md"),
		filepath.Join(paths.Home, ".hermes", "skills", "ha-nova", "ha-nova", "session-bootstrap.md"),
	}
	for _, path := range bootstrapPaths {
		if err := os.WriteFile(path, []byte("stale bootstrap\n"), 0o644); err != nil {
			t.Fatalf("seed stale bootstrap %s: %v", path, err)
		}
	}

	previousRuntimeProbe := clientRuntimeDetectedForStatus
	clientRuntimeDetectedForStatus = func(string) bool { return false }
	t.Cleanup(func() { clientRuntimeDetectedForStatus = previousRuntimeProbe })
	if err := saveState(paths, installState{
		SchemaVersion:    stateSchemaVersion,
		Version:          "0.6.0",
		InstalledClients: []string{"codex", "opencode", "antigravity", "hermes"},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if err := postUpdateSync(paths); err != nil {
		t.Fatalf("postUpdateSync() error: %v", err)
	}
	for _, path := range bootstrapPaths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read refreshed bootstrap %s: %v", path, err)
		}
		if string(content) != "# Session Bootstrap\n" {
			t.Fatalf("bootstrap %s remained stale: %q", path, content)
		}
	}
	state, err := loadState(paths)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.ClientsVerifiedVersion != "0.6.1" {
		t.Fatalf("all attached file clients should be verified at 0.6.1, got %q", state.ClientsVerifiedVersion)
	}
}

func TestPostUpdateSyncRefusesForeignRuntimeAbsentTree(t *testing.T) {
	paths := setupHealableInstall(t)
	foreignRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	writeBundleTestFile(t, filepath.Join(foreignRoot, "ha-nova", "SKILL.md"), "name: unrelated\n", 0o644)
	writeBundleTestFile(t, filepath.Join(foreignRoot, "private.txt"), "keep me\n", 0o600)
	previousRuntimeProbe := clientRuntimeDetectedForStatus
	clientRuntimeDetectedForStatus = func(string) bool { return false }
	t.Cleanup(func() { clientRuntimeDetectedForStatus = previousRuntimeProbe })

	if err := postUpdateSync(paths); err != nil {
		t.Fatalf("postUpdateSync() error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(foreignRoot, "private.txt"))
	if err != nil || string(content) != "keep me\n" {
		t.Fatalf("foreign tree was modified: content=%q err=%v", content, err)
	}
	context, err := os.ReadFile(filepath.Join(foreignRoot, "ha-nova", "SKILL.md"))
	if err != nil || string(context) != "name: unrelated\n" {
		t.Fatalf("foreign context was modified: content=%q err=%v", context, err)
	}
}
