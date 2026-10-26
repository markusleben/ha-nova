package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// installStaleClaudeMock: a claude on PATH whose first `stale` plugin
// installs keep HA NOVA 0.24.0 (exit 0, nothing changed) and later ones
// install 0.27.3. It logs every call and answers no --json list.
func installStaleClaudeMock(t *testing.T, home string, stale int) string {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	logPath := filepath.Join(binDir, "claude.log")
	plugins := filepath.Join(home, ".claude", "plugins")
	script := `#!/usr/bin/env bash
set -euo pipefail
cmd="$*"
printf '%s\n' "$cmd" >> ` + shellQuote(logPath) + `
plugins=` + shellQuote(plugins) + `
mkdir -p "$plugins"
case "$cmd" in
  "plugin marketplace add "*)
    printf '{"ha-nova":{"source":{"source":"directory","path":"%s"}}}' "${cmd#plugin marketplace add }" > "$plugins/known_marketplaces.json" ;;
  "plugin remove ha-nova@ha-nova")
    rm -f "$plugins/installed_plugins.json" ;;
  "plugin install ha-nova@ha-nova")
    n=$(( $(cat "$plugins/installs" 2>/dev/null || echo 0) + 1 ))
    echo "$n" > "$plugins/installs"
    version=0.27.3
    if [ "$n" -le ` + strconv.Itoa(stale) + ` ]; then version=0.24.0; fi
    mkdir -p "$plugins/cache/ha-nova/ha-nova/$version"
    printf '{"version":2,"plugins":{"ha-nova@ha-nova":[{"scope":"user","installPath":"%s","version":"%s"}]}}' "$plugins/cache/ha-nova/ha-nova/$version" "$version" > "$plugins/installed_plugins.json" ;;
  "plugin list --json") exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// staleClaudeRelease: a 0.27.3 release install whose Claude Code still
// holds HA NOVA 0.24.0.
func staleClaudeRelease(t *testing.T) (runtimePaths, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HA_NOVA_DEV_ROOT", "")
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "")
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	return paths, writeStaleClaudeRelease(t, paths)
}

func writeStaleClaudeRelease(t *testing.T, paths runtimePaths) string {
	t.Helper()
	home := paths.Home
	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, "bundle.json"), []byte(`{"version":"0.27.3"}`), 0o644); err != nil {
		t.Fatalf("write bundle.json: %v", err)
	}
	releaseRoot, err := claudeMarketplaceReleaseRoot(paths, "0.27.3")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	writeClaudeMarketplaceRegistrationFixture(t, home, releaseRoot)
	installPath := filepath.Join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.24.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir installPath: %v", err)
	}
	state := `{"version":2,"plugins":{"ha-nova@ha-nova":[{"scope":"user","installPath":` + jsonQuote(installPath) + `,"version":"0.24.0"}]}}`
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}
	return releaseRoot
}

func TestVerifyClaudePluginReinstallsAPluginClaudeCodeKeptOld(t *testing.T) {
	paths, releaseRoot := staleClaudeRelease(t)
	logPath := installStaleClaudeMock(t, paths.Home, 0)
	if err := verifyClaudePluginInstalled(paths, releaseRoot); err != nil {
		t.Fatalf("verifyClaudePluginInstalled() error: %v", err)
	}
	if log := mockInvocations(t, logPath); !strings.Contains(log, "plugin remove ha-nova@ha-nova\nplugin install ha-nova@ha-nova\n") {
		t.Fatalf("expected one remove and a fresh install, got:\n%s", log)
	}
	if claudeHANovaAt(filepath.Join(paths.Home, ".claude"), "0.27.3") != claudeHANovaUnknown {
		t.Fatal("HA NOVA in Claude Code is still older after the fresh install")
	}
}

func TestVerifyClaudePluginFailsWhenClaudeCodeKeepsItOld(t *testing.T) {
	paths, releaseRoot := staleClaudeRelease(t)
	installStaleClaudeMock(t, paths.Home, 99)
	if err := verifyClaudePluginInstalled(paths, releaseRoot); !errors.Is(err, errClaudeKeptOlderPlugin) {
		t.Fatalf("verifyClaudePluginInstalled() = %v, want the kept-older failure", err)
	}
}

// The session-start hook runs auto-repair at every start: a Claude Code
// that keeps an older HA NOVA is tried once per release, never "Fixed".
func TestAutoRepairOfAClaudePluginKeptOldNeitherLoopsNorSaysFixed(t *testing.T) {
	paths, _ := staleClaudeRelease(t)
	logPath := installStaleClaudeMock(t, paths.Home, 99)
	claude := clientStatus{ID: "claude", Label: "Claude Code", SupportedOnOS: true, RuntimeDetected: true}

	first := attemptClientAutoRepair(paths, claude)
	if first.Repaired || !errors.Is(first.Err, errClaudeKeptOlderPlugin) {
		t.Fatalf("first repair = %+v, want the kept-older failure", first)
	}
	if !claudeRepairFailedFor(paths, "0.27.3") {
		t.Fatal("the failed release isn't noted")
	}
	tried := mockInvocations(t, logPath)
	second := attemptClientAutoRepair(paths, claude)
	if second.Repaired || second.Err != nil || !second.Skipped || !strings.Contains(second.SkipReason, "kept an older HA NOVA") {
		t.Fatalf("second repair = %+v, want it skipped", second)
	}
	if mockInvocations(t, logPath) != tried {
		t.Fatalf("the skipped repair still ran claude:\n%s", mockInvocations(t, logPath))
	}
}

func TestAutoRepairOfAClaudePluginKeptOldOnceIsFixed(t *testing.T) {
	paths, _ := staleClaudeRelease(t)
	installStaleClaudeMock(t, paths.Home, 1) // the sync's own install keeps it old; the fresh one works
	if err := noteClaudeRepair(paths, "0.27.2", true); err != nil {
		t.Fatalf("noteClaudeRepair() error: %v", err)
	}
	got := attemptClientAutoRepair(paths, clientStatus{ID: "claude", Label: "Claude Code", SupportedOnOS: true, RuntimeDetected: true})
	if !got.Repaired || got.Err != nil {
		t.Fatalf("repair = %+v, want repaired", got)
	}
	if fileExists(claudeRepairFailedPath(paths)) {
		t.Fatal("a successful repair keeps the failure note")
	}
}

func TestClaudeHANovaAtComparesWithoutPrereleaseSuffix(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(filepath.Join(root, "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, tc := range []struct{ record, version, want string }{
		{"0.28.0-rc1", "0.28.0", claudeHANovaUnknown},
		{"0.28.0-rc1", "0.28.0-rc2", claudeHANovaUnknown},
		{"0.27.3", "0.28.0-rc1", claudeHANovaOlder},
	} {
		state := `{"version":2,"plugins":{"ha-nova@ha-nova":[{"installPath":"/x","version":"` + tc.record + `"}]}}`
		if err := os.WriteFile(filepath.Join(root, "plugins", "installed_plugins.json"), []byte(state), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if got := claudeHANovaAt(root, tc.version); got != tc.want {
			t.Fatalf("record %s for %s = %q, want %q", tc.record, tc.version, got, tc.want)
		}
	}
}
