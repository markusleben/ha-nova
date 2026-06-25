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

func TestAntigravityRuntimeDetectedFindsDesktopProfileWithoutCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	profileDir := filepath.Join(home, ".gemini", "antigravity")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir desktop profile: %v", err)
	}

	if !clientRuntimeDetected("antigravity") {
		t.Fatal("expected Antigravity runtime detection to accept desktop profile without agy")
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
		return relayWSPingResponse{StatusCode: 200, Body: []byte(`{"type":"pong"}`)}, nil
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
