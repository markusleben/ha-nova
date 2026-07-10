package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// installClaudeListJSONMock puts a claude mock on PATH that answers
// `claude plugin list --json` with the given stdout, logging every
// invocation, and fails every other subcommand.
func installClaudeListJSONMock(t *testing.T, stdout string) string {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	logPath := filepath.Join(binDir, "claude.log")
	script := "#!/usr/bin/env bash\n" +
		"echo \"$*\" >> " + logPath + "\n" +
		"if [ \"$1\" = plugin ] && [ \"$2\" = list ] && [ \"$3\" = --json ]; then\n" +
		"cat <<'CLAUDE_MOCK_EOF'\n" + stdout + "\nCLAUDE_MOCK_EOF\n" +
		"exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func mockInvocations(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read mock log: %v", err)
	}
	return string(data)
}

func writeInstalledPluginsFixture(t *testing.T, home string, withHaNova bool) {
	t.Helper()
	pluginsDir := filepath.Join(home, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	state := `{"plugins":{},"version":2}`
	if withHaNova {
		installPath := filepath.Join(pluginsDir, "cache", "ha-nova", "ha-nova", "0.11.1")
		if err := os.MkdirAll(installPath, 0o755); err != nil {
			t.Fatalf("mkdir installPath: %v", err)
		}
		state = `{"plugins":{"ha-nova@ha-nova":[{"installPath":` + jsonQuote(installPath) + `,"scope":"user","version":"0.11.1"}]},"version":2}`
	}
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}
}

func TestClaudePluginPresenceCLIAnswerCoversMissingFileEntry(t *testing.T) {
	// Claude Code 2.1.x stopped recording some installs in the state file;
	// the CLI's own --json answer must count even with no file at all.
	home := t.TempDir()
	t.Setenv("HOME", home)
	installPath := filepath.Join(t.TempDir(), "plugin-payload")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir installPath: %v", err)
	}
	entries, err := json.Marshal([]map[string]any{{"id": "ha-nova@ha-nova", "installPath": installPath}})
	if err != nil {
		t.Fatalf("marshal entries: %v", err)
	}
	installClaudeListJSONMock(t, string(entries))

	found, usable, stateUnreadable := readClaudePluginPresence(home)
	if !found || !usable || stateUnreadable {
		t.Fatalf("readClaudePluginPresence() = (%v, %v, %v), want (true, true, false) from CLI answer", found, usable, stateUnreadable)
	}
}

func TestClaudePluginPresenceHealthyFileShortCircuitsWithoutExec(t *testing.T) {
	// The healthy path must stay exec-free (session hooks, doctor cadence).
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeInstalledPluginsFixture(t, home, true)
	logPath := installClaudeListJSONMock(t, `[]`)

	found, usable, stateUnreadable := readClaudePluginPresence(home)
	if !found || !usable || stateUnreadable {
		t.Fatalf("readClaudePluginPresence() = (%v, %v, %v), want healthy file verdict", found, usable, stateUnreadable)
	}
	if log := mockInvocations(t, logPath); log != "" {
		t.Fatalf("expected no claude invocation on healthy file state, got: %s", log)
	}
}

func TestClaudePluginPresenceUnreadableFileNeverExecs(t *testing.T) {
	// Torn writes must not trigger any exec — unreadable snapshots may not
	// drive repair decisions at all.
	home := t.TempDir()
	t.Setenv("HOME", home)
	pluginsDir := filepath.Join(home, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte("{torn"), 0o644); err != nil {
		t.Fatalf("write torn file: %v", err)
	}
	logPath := installClaudeListJSONMock(t, `[]`)

	_, _, stateUnreadable := readClaudePluginPresence(home)
	if !stateUnreadable {
		t.Fatalf("readClaudePluginPresence() stateUnreadable = false, want true for torn file")
	}
	if log := mockInvocations(t, logPath); log != "" {
		t.Fatalf("expected no claude invocation on unreadable state, got: %s", log)
	}
}

func TestClaudePluginPresenceFallsBackToFileVerdictOnNonJSONOutput(t *testing.T) {
	// Older claude versions print help/usage for the unknown --json flag;
	// the readable-negative file verdict then stands.
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeInstalledPluginsFixture(t, home, false)
	installClaudeListJSONMock(t, "Usage: claude plugin list [options]")

	found, _, stateUnreadable := readClaudePluginPresence(home)
	if found || stateUnreadable {
		t.Fatalf("readClaudePluginPresence() = (found=%v, unreadable=%v), want readable-negative file verdict", found, stateUnreadable)
	}
}

func TestClaudePluginPresenceCLINegativeIsAuthoritative(t *testing.T) {
	// found-but-unusable file entries (dangling installPath) get re-checked
	// against the CLI; an authoritative empty list means "not installed".
	home := t.TempDir()
	t.Setenv("HOME", home)
	pluginsDir := filepath.Join(home, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	state := `{"plugins":{"ha-nova@ha-nova":[{"installPath":"/nonexistent/path","scope":"user"}]},"version":2}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}
	installClaudeListJSONMock(t, `[]`)

	found, _, _ := readClaudePluginPresence(home)
	if found {
		t.Fatalf("readClaudePluginPresence() found plugin despite authoritative CLI negative")
	}
}
