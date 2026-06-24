package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStatusJSONReportsInstallIntegrityWithoutWritingState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	writeStatusInstallMetadata(t, paths, "0.6.2")
	if err := saveState(paths, installState{
		SchemaVersion:          stateSchemaVersion,
		Version:                "0.6.1",
		ClientsVerifiedVersion: "0.6.0",
		InstalledClients:       []string{"hermes"},
	}); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}
	before, err := os.ReadFile(paths.StateFile)
	if err != nil {
		t.Fatalf("read state before: %v", err)
	}

	backupRef := filepath.Join(home, ".local", "share", installBackupPrefixOld+"status", "docs")
	writeBundleTestFile(t, filepath.Join(home, ".hermes", "skills", "ha-nova", "ha-nova", "SKILL.md"), "See `"+backupRef+"`\n", 0o644)
	writeBundleTestFile(t, filepath.Join(paths.ConfigDir, "version-check"), "#!/bin/sh\n", 0o755)

	exitCode, output := captureCommandOutput(t, func() int {
		return runStatus(paths, []string{"--json"})
	})
	if exitCode != 0 {
		t.Fatalf("runStatus() exit = %d, want 0\n%s", exitCode, output)
	}

	after, err := os.ReadFile(paths.StateFile)
	if err != nil {
		t.Fatalf("read state after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("status --json must not write state\nbefore=%s\nafter=%s", before, after)
	}

	var report installStatusReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("status JSON did not decode: %v\n%s", err, output)
	}
	if report.EffectiveVersion != "0.6.2" {
		t.Fatalf("EffectiveVersion = %q, want 0.6.2", report.EffectiveVersion)
	}
	if report.Bundle.Version != "0.6.2" || !report.Bundle.Present {
		t.Fatalf("bundle status = %+v, want present v0.6.2", report.Bundle)
	}
	if report.VersionFile.SkillVersion != "0.6.2" {
		t.Fatalf("version file status = %+v, want skill version 0.6.2", report.VersionFile)
	}
	if report.State.Version != "0.6.1" || report.State.ClientsVerifiedVersion != "0.6.0" {
		t.Fatalf("state status = %+v, want state/client markers", report.State)
	}
	if len(report.ActiveDriftClients) != 1 || report.ActiveDriftClients[0] != "hermes" {
		t.Fatalf("ActiveDriftClients = %v, want [hermes]", report.ActiveDriftClients)
	}
	if len(report.Clients) != 1 || report.Clients[0].ID != "hermes" || !report.Clients[0].ActiveDrift {
		t.Fatalf("clients = %+v, want active drift for hermes", report.Clients)
	}
	foundWrapper := false
	for _, artifact := range report.InactiveArtifacts {
		if artifact.Kind == "repo_dev_version_check_wrapper" && artifact.Path == filepath.Join(paths.ConfigDir, "version-check") {
			foundWrapper = true
		}
	}
	if !foundWrapper {
		t.Fatalf("inactive artifacts = %+v, want repo/dev version-check wrapper", report.InactiveArtifacts)
	}
}

func TestRunStatusJSONTreatsDevBuildClaudeMarketplaceAsAttached(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})

	origVersion, origChannel, origStamp := Version, BuildChannel, BuildStamp
	t.Cleanup(func() { Version, BuildChannel, BuildStamp = origVersion, origChannel, origStamp })
	Version, BuildChannel, BuildStamp = "0.7.0", "dev", "2026-06-22T09:30-local"

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	writeStatusInstallMetadata(t, paths, "0.6.2")
	if err := saveState(paths, installState{
		SchemaVersion:          stateSchemaVersion,
		Version:                "0.7.0",
		ClientsVerifiedVersion: "0.7.0",
		InstalledClients:       []string{"claude"},
	}); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)
	writeClaudeMarketplaceRegistrationFixture(t, home, claudeMarketplaceDevRoot(paths))

	exitCode, output := captureCommandOutput(t, func() int {
		return runStatus(paths, []string{"--json"})
	})
	if exitCode != 0 {
		t.Fatalf("runStatus() exit = %d, want 0\n%s", exitCode, output)
	}

	var report installStatusReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("status JSON did not decode: %v\n%s", err, output)
	}
	if len(report.Clients) != 1 || report.Clients[0].ID != "claude" {
		t.Fatalf("clients = %+v, want only Claude", report.Clients)
	}
	if !report.Clients[0].Attached || !report.Clients[0].Ready {
		t.Fatalf("Claude status = %+v, want attached and ready for dev marketplace source", report.Clients[0])
	}
}

func TestRunDoctorReportsActiveDriftButDoesNotFailInactiveArtifacts(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})

	paths, _ := doctorTestSetup(t)
	writeStatusInstallMetadata(t, paths, "0.6.2")
	if err := saveState(paths, installState{
		SchemaVersion:    stateSchemaVersion,
		InstalledClients: []string{"hermes"},
	}); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	t.Cleanup(func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	})
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.6.2"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: 200, Body: []byte(`{"type":"pong"}`)}, nil
	}

	backupRef := filepath.Join(paths.Home, ".local", "share", installBackupPrefixOld+"doctor", "docs")
	writeBundleTestFile(t, filepath.Join(paths.Home, ".hermes", "skills", "ha-nova", "ha-nova", "SKILL.md"), "See `"+backupRef+"`\n", 0o644)
	writeBundleTestFile(t, filepath.Join(paths.ConfigDir, "version-check"), "#!/bin/sh\n", 0o755)

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to fail for active drift:\n%s", output)
	}
	if !strings.Contains(output, "Hermes Agent has active install drift") {
		t.Fatalf("expected active drift warning:\n%s", output)
	}
	if !strings.Contains(output, "Repair: run `ha-nova setup hermes`.") {
		t.Fatalf("expected repair hint:\n%s", output)
	}
	if !strings.Contains(output, "Inactive legacy/dev artifact ignored:") {
		t.Fatalf("expected inactive artifact to be reported separately:\n%s", output)
	}
}

func writeStatusInstallMetadata(t *testing.T, paths runtimePaths, version string) {
	t.Helper()
	meta := `{"bundle_format_version":1,"version":"` + version + `","os":"` + bundlePlatformOS() + `","arch":"` + bundlePlatformArch() + `","binary_name":"` + publicBinaryName() + `"}`
	writeBundleTestFile(t, paths.BundleFile, meta, 0o644)
	writeBundleTestFile(t, paths.VersionFile, `{"skill_version":"`+version+`","min_relay_version":"0.1.0"}`, 0o644)
}
