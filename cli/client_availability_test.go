package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClientRuntimeDetectedFindsUserLocalBin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("user-local executable fallback is Unix-style")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir user local bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "hermes"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write hermes executable: %v", err)
	}

	if !clientRuntimeDetected("hermes") {
		t.Fatal("expected runtime detection to find ~/.local/bin/hermes when PATH omits it")
	}
}

func TestAntigravityRuntimeDetectedIgnoresStaleDesktopProfileWithoutRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	withAntigravityRuntimePlatform(t, "linux")
	profileDir := filepath.Join(home, ".gemini", "antigravity")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir desktop profile: %v", err)
	}

	if clientRuntimeDetected("antigravity") {
		t.Fatal("expected stale Antigravity profile without CLI, launcher, or app marker to be ignored")
	}
}

func withAntigravityRuntimePlatform(t *testing.T, osName string) {
	t.Helper()
	originalGOOS := antigravityRuntimePlatformOS
	antigravityRuntimePlatformOS = osName
	t.Cleanup(func() {
		antigravityRuntimePlatformOS = originalGOOS
	})
}

func withAntigravityMacApplicationsRoot(t *testing.T, root string) {
	t.Helper()
	originalRoot := antigravityMacApplicationsRoot
	antigravityMacApplicationsRoot = root
	t.Cleanup(func() {
		antigravityMacApplicationsRoot = originalRoot
	})
}

func writeWindowsAntigravityDesktopMarker(t *testing.T, home string) {
	t.Helper()
	localAppData := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", localAppData)
	app := filepath.Join(localAppData, "Programs", "antigravity", "Antigravity.exe")
	if err := os.MkdirAll(filepath.Dir(app), 0o755); err != nil {
		t.Fatalf("mkdir Antigravity app dir: %v", err)
	}
	if err := os.WriteFile(app, []byte("desktop app marker"), 0o644); err != nil {
		t.Fatalf("write Antigravity app marker: %v", err)
	}
}

func TestAntigravityRuntimeDetectedFindsMacDesktopAppWithoutCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	appsRoot := t.TempDir()
	withAntigravityRuntimePlatform(t, "darwin")
	withAntigravityMacApplicationsRoot(t, appsRoot)

	app := filepath.Join(appsRoot, "Antigravity.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatalf("mkdir Antigravity app marker: %v", err)
	}

	if !clientRuntimeDetected("antigravity") {
		t.Fatal("expected Antigravity runtime detection to accept macOS Desktop app without agy")
	}
}

func TestAntigravityRuntimeDetectedFindsWindowsDesktopAppWithoutCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	withAntigravityRuntimePlatform(t, "windows")
	writeWindowsAntigravityDesktopMarker(t, home)

	if !clientRuntimeDetected("antigravity") {
		t.Fatal("expected Antigravity runtime detection to accept Windows Desktop app without agy")
	}
}

func TestSetupClientChoicesEnableAntigravityForWindowsDesktopAppWithoutCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	withAntigravityRuntimePlatform(t, "windows")
	writeWindowsAntigravityDesktopMarker(t, home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	choices, err := buildSetupClientChoices(paths, installState{})
	if err != nil {
		t.Fatalf("buildSetupClientChoices() error: %v", err)
	}
	for _, choice := range choices {
		if choice.Value == "antigravity" {
			if choice.Disabled {
				t.Fatalf("expected Antigravity choice to be enabled for Windows Desktop app, got %+v", choice)
			}
			return
		}
	}
	t.Fatalf("expected Antigravity choice, got %+v", choices)
}

func TestAntigravityRuntimeDetectedFindsLinuxDesktopCommandWithoutCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable mode test")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	withAntigravityRuntimePlatform(t, "linux")

	app := filepath.Join(binDir, "antigravity")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write Antigravity desktop command: %v", err)
	}

	if !clientRuntimeDetected("antigravity") {
		t.Fatal("expected Antigravity runtime detection to accept Linux Desktop command without agy")
	}
}

func TestBuildSetupClientChoicesDisablesMissingRuntime(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	choices, err := buildSetupClientChoices(paths, installState{})
	if err != nil {
		t.Fatalf("buildSetupClientChoices() error: %v", err)
	}
	if len(choices) != 6 {
		t.Fatalf("expected 6 setup choices, got %d", len(choices))
	}
	for _, choice := range choices {
		if !choice.Disabled {
			t.Fatalf("expected all choices disabled without runtimes, got %+v", choices)
		}
	}
}

func TestBuildSetupClientChoicesShowsAvailableClientsBeforeDisabledOnes(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true, "antigravity": true})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	choices, err := buildSetupClientChoices(paths, installState{})
	if err != nil {
		t.Fatalf("buildSetupClientChoices() error: %v", err)
	}

	gotValues := []string{}
	for _, choice := range choices {
		gotValues = append(gotValues, choice.Value)
	}
	wantValues := []string{"claude", "antigravity", "codex", "opencode", "hermes", "all"}
	if strings.Join(gotValues, ",") != strings.Join(wantValues, ",") {
		t.Fatalf("choice order = %v, want %v", gotValues, wantValues)
	}
	if choices[0].Disabled || choices[1].Disabled {
		t.Fatalf("expected available clients first, got %+v", choices)
	}
	for _, choice := range choices[2:5] {
		if !choice.Disabled {
			t.Fatalf("expected disabled clients after available ones, got %+v", choices)
		}
	}
	if choices[5].Disabled {
		t.Fatalf("expected all choice to stay available when at least one client is available, got %+v", choices)
	}
	if choices[5].Value != "all" {
		t.Fatalf("expected all choice to stay last, got %+v", choices)
	}
	if choices[5].Number != "6" {
		t.Fatalf("expected all-choice numbering to be recomputed, got %q", choices[5].Number)
	}
}

func TestResolveSetupClientsForAllReturnsOnlyAvailableClients(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	resolved, skipped, err := resolveSetupClients(paths, "all")
	if err != nil {
		t.Fatalf("resolveSetupClients() error: %v", err)
	}
	if len(resolved) != 1 || resolved[0] != "claude" {
		t.Fatalf("expected all to resolve only Claude, got %+v", resolved)
	}
	if len(skipped) != 4 {
		t.Fatalf("expected four skipped clients, got %+v", skipped)
	}
}

func TestRunSetupFailsBeforePersistenceWhenTargetRuntimeMissing(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"--non-interactive",
			"--host", "192.168.1.5",
			"--relay-token", "test-relay-token",
			"codex",
		})
	})
	if exitCode == 0 {
		t.Fatalf("expected setup to fail when Codex runtime is missing:\n%s", output)
	}
	if !strings.Contains(output, "Codex CLI is not available yet: install Codex CLI first") {
		t.Fatalf("expected explicit missing-client output:\n%s", output)
	}
	if _, err := os.Stat(paths.ConfigFile); !isNotExist(err) {
		t.Fatalf("expected config not to be persisted, err=%v", err)
	}
	if _, err := os.Stat(paths.StateFile); !isNotExist(err) {
		t.Fatalf("expected state not to be persisted, err=%v", err)
	}
}

func TestRunDoctorWarnsWhenConfiguredClientRuntimeMissing(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": false})
	paths, _ := doctorTestSetup(t)
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: 200, Body: []byte(`{"ok":true,"data":{"type":"pong"}}`)}, nil
	}

	state := loadStateOrDefault(paths)
	state.InstalledClients = []string{"codex"}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to degrade for missing Codex runtime:\n%s", output)
	}
	if !strings.Contains(output, "Codex CLI configured, but client runtime not detected now") {
		t.Fatalf("expected configured-but-missing-runtime warning:\n%s", output)
	}
}

func TestPostUpdateSyncSkipsConfiguredClientWhenRuntimeMissing(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": false})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	state := loadStateOrDefault(paths)
	state.InstalledClients = []string{"codex"}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		if err := postUpdateSync(paths); err != nil {
			t.Fatalf("postUpdateSync() error: %v", err)
		}
		return 0
	})
	if exitCode != 0 {
		t.Fatalf("expected successful post-update sync wrapper, got %d:\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Skipping Codex CLI until the client runtime is installed in this environment") {
		t.Fatalf("expected skip warning:\n%s", output)
	}

	restored, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if !containsClient(restored.InstalledClients, "codex") {
		t.Fatalf("expected configured client to stay in state after skip, got %+v", restored.InstalledClients)
	}
}

func TestCurrentVersionSyncOmitsSessionInstructionWhenClientRuntimeIsMissing(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": false})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.2.2","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	state := loadStateOrDefault(paths)
	state.InstalledClients = []string{"codex"}
	state.ClientsVerifiedVersion = "0.2.2"
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return syncInstalledClientsForCurrentVersion(paths, "0.2.2", "0.2.2", 0, captureInstallLifecycleGeneration(paths))
	})
	if exitCode != 0 {
		t.Fatalf("expected skipped client sync to remain retryable, got %d:\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Skipping Codex CLI until the client runtime is installed in this environment") {
		t.Fatalf("expected skip warning:\n%s", output)
	}
	if strings.Contains(output, postUpdateSessionInstruction) {
		t.Fatalf("did not expect new-session instruction after skipped client sync:\n%s", output)
	}
	if strings.Contains(output, postUpdatePartialSessionInstruction) {
		t.Fatalf("did not expect partial new-session instruction when every client was skipped:\n%s", output)
	}
	restored, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if restored.ClientsVerifiedVersion != "0.2.2" {
		t.Fatalf("expected skipped current-version sync to preserve quiet-check marker, got %q", restored.ClientsVerifiedVersion)
	}
}
