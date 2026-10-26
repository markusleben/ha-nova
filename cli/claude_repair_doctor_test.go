package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// The session-start hook runs `doctor --auto-repair --quiet`: its self-heal
// and its auto-repair both leave a Claude Code that kept an older HA NOVA
// through a repair alone, so the next session start runs no claude at all.
func TestDoctorAutoRepairTriesAClaudePluginKeptOldOncePerRelease(t *testing.T) {
	paths, _ := doctorTestSetup(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HA_NOVA_DEV_ROOT", "")
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "")
	writeStaleClaudeRelease(t, paths)
	logPath := installStaleClaudeMock(t, paths.Home, 99)
	originalHealth, originalWSPing := fetchRelayHealthForReadiness, probeRelayWSPingForReadiness
	t.Cleanup(func() { fetchRelayHealthForReadiness, probeRelayWSPingForReadiness = originalHealth, originalWSPing })
	fetchRelayHealthForReadiness = func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.27.3"}`), nil
	}
	probeRelayWSPingForReadiness = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"ok":true,"data":{"type":"pong"}}`)}, nil
	}
	state := loadStateOrDefault(paths)
	state.Version, state.InstalledClients, state.ClientsVerifiedVersion = "0.27.2", []string{"claude"}, ""
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}
	hook := []string{"--auto-repair", "--quiet"}

	_, first := captureCommandOutput(t, func() int { return runDoctor(paths, slices.Clone(hook)) })
	tried := mockInvocations(t, logPath)
	if !strings.Contains(tried, "plugin install ha-nova@ha-nova") || !claudeRepairFailedFor(paths, "0.27.3") {
		t.Fatalf("the first run should try once and note the release; claude log:\n%s\noutput:\n%s", tried, first)
	}
	if strings.Contains(first, "Fixed HA NOVA") {
		t.Fatalf("a repair that kept the old version says Fixed:\n%s", first)
	}
	_, second := captureCommandOutput(t, func() int { return runDoctor(paths, slices.Clone(hook)) })
	if again := mockInvocations(t, logPath); again != tried {
		t.Fatalf("the next session start ran claude again:\n%s\noutput:\n%s", strings.TrimPrefix(again, tried), second)
	}
}
