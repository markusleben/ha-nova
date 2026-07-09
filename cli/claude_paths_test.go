package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Tests across this package build Claude state under temp HOMEs; a
// CLAUDE_CONFIG_DIR inherited from the invoking shell (e.g. a Claude Code
// session) would redirect every reader away from those fixtures.
func TestMain(m *testing.M) {
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	os.Exit(m.Run())
}

func TestClaudeConfigRootDefaultsToDotClaude(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	if got, want := claudeConfigRoot(home), filepath.Join(home, ".claude"); got != want {
		t.Fatalf("claudeConfigRoot() = %q, want %q", got, want)
	}
	if claudeConfigRootRedirected(home) {
		t.Fatalf("claudeConfigRootRedirected() = true without CLAUDE_CONFIG_DIR")
	}
}

func TestClaudeConfigRootHonorsClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	redirected := filepath.Join(t.TempDir(), "claude-profile")
	t.Setenv("CLAUDE_CONFIG_DIR", redirected)
	if got := claudeConfigRoot(home); got != redirected {
		t.Fatalf("claudeConfigRoot() = %q, want %q", got, redirected)
	}
	if !claudeConfigRootRedirected(home) {
		t.Fatalf("claudeConfigRootRedirected() = false with CLAUDE_CONFIG_DIR set")
	}
}

func TestClaudePluginInstallStateFollowsClaudeConfigDir(t *testing.T) {
	// Regression: `ha-nova update` inside a Claude Code session spawns
	// `claude plugin install`, which writes plugin state to CLAUDE_CONFIG_DIR;
	// the verifier must read the same root or every sync aborts + rolls back.
	home := t.TempDir()
	redirected := filepath.Join(t.TempDir(), "claude-profile")
	t.Setenv("CLAUDE_CONFIG_DIR", redirected)

	pluginsDir := filepath.Join(redirected, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	installPath := filepath.Join(redirected, "plugins", "cache", "ha-nova", "ha-nova", "0.11.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir installPath: %v", err)
	}
	state := `{"plugins":{"ha-nova@ha-nova":[{"installPath":` + jsonQuote(installPath) + `,"scope":"user","version":"0.11.0"}]},"version":2}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}

	found, usable := readClaudePluginInstallSnapshot(home)
	if !found || !usable {
		t.Fatalf("readClaudePluginInstallSnapshot() = (%v, %v), want (true, true) via CLAUDE_CONFIG_DIR", found, usable)
	}

	// Nothing under the default root: without the redirect the plugin is absent.
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	found, _ = readClaudePluginInstallSnapshot(home)
	if found {
		t.Fatalf("readClaudePluginInstallSnapshot() found plugin under default root unexpectedly")
	}
}

func jsonQuote(s string) string {
	quoted, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(quoted)
}
