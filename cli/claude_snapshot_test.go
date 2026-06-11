package main

import "testing"

func TestClaudePluginInstallSnapshotFromValueIgnoresForeignArrayEntries(t *testing.T) {
	value := []any{
		map[string]any{
			"name":        "some-other-plugin@other-marketplace",
			"installPath": t.TempDir(),
		},
	}

	found, usable := claudePluginInstallSnapshotFromValue(value)
	if found || usable {
		t.Fatalf("expected foreign array entries to be ignored, got found=%v usable=%v", found, usable)
	}
}

func TestClaudePluginInstallSnapshotFromValueFindsHaNovaArrayEntry(t *testing.T) {
	installPath := t.TempDir()
	value := []any{
		map[string]any{
			"name":        "some-other-plugin@other-marketplace",
			"installPath": installPath,
		},
		map[string]any{
			"ha-nova@ha-nova": []any{
				map[string]any{"installPath": installPath},
			},
		},
	}

	found, usable := claudePluginInstallSnapshotFromValue(value)
	if !found || !usable {
		t.Fatalf("expected ha-nova array entry to be detected, got found=%v usable=%v", found, usable)
	}
}
