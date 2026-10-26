package main

import (
	"strings"
	"testing"
)

func claudeRegistryEntry() clientRegistryEntry {
	return clientRegistryEntry{ID: "claude", Label: "Claude Code", AdapterKind: "plugin_marketplace", SupportedOS: []string{"macos", "linux", "windows"}}
}

// Claude Desktop brings no claude command: setup says Claude Code is still
// needed instead of a bare "install Claude Code first".
func TestClaudeStatusReasonNamesClaudeDesktopWithoutClaudeCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	installClaudeListJSONMock(t, `[]`) // never the real claude
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	origRuntime, origDesktop := clientRuntimeDetectedForStatus, claudeDesktopInstalled
	t.Cleanup(func() { clientRuntimeDetectedForStatus, claudeDesktopInstalled = origRuntime, origDesktop })
	clientRuntimeDetectedForStatus = func(string) bool { return false }

	desktopHome := ""
	claudeDesktopInstalled = func(h string) bool { desktopHome = h; return true }
	status := evaluateClientStatus(paths, installState{}, claudeRegistryEntry())
	if desktopHome != home || !strings.Contains(status.Reason, "Claude Desktop is installed") || !strings.Contains(status.Reason, "install Claude Code first") {
		t.Fatalf("reason with Claude Desktop = %q (checked home %q), want Claude Desktop named and Claude Code to install", status.Reason, desktopHome)
	}

	claudeDesktopInstalled = func(string) bool { return false }
	if got := evaluateClientStatus(paths, installState{}, claudeRegistryEntry()).Reason; got != "install Claude Code first" {
		t.Fatalf("reason without Claude Desktop = %q", got)
	}

	clientRuntimeDetectedForStatus = func(string) bool { return true }
	claudeDesktopInstalled = func(string) bool { return true }
	if got := evaluateClientStatus(paths, installState{}, claudeRegistryEntry()).Reason; got != "" {
		t.Fatalf("reason with Claude Code here = %q, want none", got)
	}
}

func TestDoctorNamesAnOutdatedClaudePlugin(t *testing.T) {
	outdated := doctorClientNotLoaded(clientStatus{Label: "Claude Code", PluginOutdated: true})
	if outdated != "Claude Code still uses an older version of HA NOVA" {
		t.Fatalf("outdated line = %q", outdated)
	}
	if got := doctorClientNotLoaded(clientStatus{Label: "Codex CLI"}); got != "Codex CLI doesn't load HA NOVA right now" {
		t.Fatalf("not loaded line = %q", got)
	}
}
