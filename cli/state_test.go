package main

import "testing"

func TestNormalizeStateMigratesLegacyGeminiClient(t *testing.T) {
	state := normalizeState(installState{
		InstalledClients: []string{"gemini", "codex"},
		ClientInstallModes: map[string]string{
			"gemini": "copy",
			"codex":  "symlink",
		},
	})

	if len(state.InstalledClients) != 2 || state.InstalledClients[0] != "antigravity" || state.InstalledClients[1] != "codex" {
		t.Fatalf("InstalledClients = %#v, want antigravity,codex", state.InstalledClients)
	}
	if _, ok := state.ClientInstallModes["gemini"]; ok {
		t.Fatalf("legacy gemini mode key was not migrated: %#v", state.ClientInstallModes)
	}
	if state.ClientInstallModes["antigravity"] != "copy" {
		t.Fatalf("antigravity install mode = %q, want copy", state.ClientInstallModes["antigravity"])
	}
}

func TestNormalizeStatePrefersCurrentAntigravityModeOverLegacyGeminiMode(t *testing.T) {
	state := normalizeState(installState{
		ClientInstallModes: map[string]string{
			"gemini":      "copy",
			"antigravity": "symlink",
		},
	})

	if state.ClientInstallModes["antigravity"] != "symlink" {
		t.Fatalf("antigravity install mode = %q, want symlink", state.ClientInstallModes["antigravity"])
	}
}
