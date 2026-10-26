package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A stale file record whose cache is gone doesn't outrank the CLI's answer
// for a working install: only a file-backed install is checked for its age.
func TestInspectClaudeInstallSnapshotTrustsCLIOverStaleFileRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HA_NOVA_DEV_ROOT", "")
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "")
	installClaudeListJSONMock(t, `[{"id":"ha-nova@ha-nova","enabled":true}]`)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, "bundle.json"), []byte(`{"version":"0.27.3"}`), 0o644); err != nil {
		t.Fatalf("write bundle.json: %v", err)
	}
	releaseRoot, err := claudeMarketplaceReleaseRoot(paths, "0.27.3")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	writeClaudeMarketplaceRegistrationFixture(t, home, releaseRoot)
	state := `{"version":2,"plugins":{"ha-nova@ha-nova":[{"scope":"user","installPath":"/nonexistent/0.24.0","version":"0.24.0"}]}}`
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}
	if snapshot := inspectClaudeInstallSnapshot(paths, installState{}); !snapshot.Attached || snapshot.MismatchReason != "" {
		t.Fatalf("Attached=%v MismatchReason=%q, want the CLI's working install", snapshot.Attached, snapshot.MismatchReason)
	}
}

// An update that left Claude Code's old install record in place: the
// marketplace points at this release, the plugin is still an older one.
func TestInspectClaudeInstallSnapshotFlagsPluginOlderThanRelease(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HA_NOVA_DEV_ROOT", "")
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "")
	logPath := installClaudeListJSONMock(t, `[]`)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, "bundle.json"), []byte(`{"version":"0.27.3"}`), 0o644); err != nil {
		t.Fatalf("write bundle.json: %v", err)
	}
	releaseRoot, err := claudeMarketplaceReleaseRoot(paths, "0.27.3")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	writeClaudeMarketplaceRegistrationFixture(t, home, releaseRoot)
	for _, tc := range []struct {
		record   string
		attached bool
		reason   string
	}{
		{"0.24.0", false, claudePluginOutdated},
		{"0.27.3", true, ""},
	} {
		installPath := filepath.Join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", tc.record)
		if err := os.MkdirAll(installPath, 0o755); err != nil {
			t.Fatalf("mkdir installPath: %v", err)
		}
		state := `{"version":2,"plugins":{"ha-nova@ha-nova":[{"scope":"user","installPath":` + jsonQuote(installPath) + `,"version":"` + tc.record + `"}]}}`
		if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), []byte(state), 0o644); err != nil {
			t.Fatalf("write installed_plugins.json: %v", err)
		}
		snapshot := inspectClaudeInstallSnapshot(paths, installState{})
		if snapshot.Attached != tc.attached || snapshot.MismatchReason != tc.reason {
			t.Fatalf("record %s: Attached=%v MismatchReason=%q, want %v %q", tc.record, snapshot.Attached, snapshot.MismatchReason, tc.attached, tc.reason)
		}
		status := evaluateClientStatus(paths, installState{}, claudeRegistryEntry())
		if status.PluginOutdated != (tc.reason == claudePluginOutdated) || status.Attached != tc.attached {
			t.Fatalf("record %s: status %+v", tc.record, status)
		}
	}
	if log := mockInvocations(t, logPath); log != "" {
		t.Fatalf("a readable install record needs no claude call, got: %s", log)
	}
}

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
