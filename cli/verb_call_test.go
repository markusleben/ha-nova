package main

import (
	"io"
	"os"
	"testing"
)

// Each tool call writes only to its own output (runCaptured): nothing reaches
// the process's stdout or stderr, which a call running beside it would share.
func TestMCPToolsLeaveProcessOutputAlone(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	c.request("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}})
	stdout, stderr := os.Stdout, os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	read := func(r *os.File, into *string, done chan<- struct{}) {
		b, _ := io.ReadAll(r)
		*into = string(b)
		close(done)
	}
	var leakedOut, leakedErr string
	outDone, errDone := make(chan struct{}), make(chan struct{})
	go read(outR, &leakedOut, outDone)
	go read(errR, &leakedErr, errDone)
	os.Stdout, os.Stderr = outW, errW
	calls := []struct {
		name string
		args map[string]any
	}{
		{"ha_find", map[string]any{"domain": "light"}},
		{"ha_get", map[string]any{"entity_id": "light.kitchen"}},
		{"ha_get", map[string]any{"entity_id": "light.nowhere"}},
		{"ha_history", map[string]any{"entity_id": "light.kitchen"}},
		{"ha_map", map[string]any{}},
		{"ha_trace", map[string]any{"action": "list", "entity_id": "automation.a"}},
		{"ha_read", map[string]any{"endpoint": "core", "path": "/api/states"}},
		{"ha_read", map[string]any{"endpoint": "snapshot", "action": "show", "target": "nope"}},
		{"ha_plan", map[string]any{"kind": "call", "service": "light.turn_on", "entity_ids": []string{"light.kitchen"}}},
		{"ha_call", map[string]any{"service": "light.turn_on", "entity_ids": []string{"light.kitchen"}}},
		{"ha_apply", map[string]any{"plan_id": "p_000000000000"}},
	}
	var missing []string
	for _, call := range calls {
		if res := c.tool(call.name, call.args); res["structuredContent"] == nil {
			missing = append(missing, call.name)
		}
	}
	os.Stdout, os.Stderr = stdout, stderr
	_, _ = outW.Close(), errW.Close()
	<-outDone
	<-errDone
	if len(missing) != 0 || leakedOut != "" || leakedErr != "" {
		t.Fatalf("results without an envelope %v; process stdout %q; process stderr %q", missing, leakedOut, leakedErr)
	}
}

// An MCP call loads its own profile: its --server, else the call's server,
// beats the process's selection, and the transport reads the credential of
// the profile its config was loaded for, not the process's.
func TestVerbConfigSelectsTheCallsProfile(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)
	setServerSelectionOverride("default")
	if cfg, err := verbConfig(paths, verbOptions{call: &verbCall{server: "cabin"}}); err != nil || cfg.RelayBaseURL != "http://cabin:8791" || cfg.profileName() != "cabin" {
		t.Fatalf("the call's server: %q %q %v", cfg.RelayBaseURL, cfg.profileName(), err)
	}
	if cfg, err := verbConfig(paths, verbOptions{call: &verbCall{server: "cabin"}, server: "default", serverSet: true}); err != nil || cfg.profileName() != "default" {
		t.Fatalf("the call's --server: %q %v", cfg.profileName(), err)
	}
	setActiveServerProfile("cabin")
	if err := writeDeviceCredential(testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	setActiveServerProfile(defaultServerProfileName)
	paired := runtimeConfig{RelayBaseURL: "http://cabin:8791", RelaySecureBaseURL: "https://cabin:18792", RelaySpkiPin: "PIN", profile: "cabin"}
	if _, _, token, _, err := relayFunctionalTransport(paired); err != nil || token != testProfileCredentialB {
		t.Fatalf("cabin's credential: token %q err %v", token, err)
	}
}

// A read's config load leaves the process's selection alone (it runs beside
// a change, whose confirmation reads it); a change's load under
// mcpVerbLock sets it, as the CLI's does.
func TestReadLoadLeavesTheProcessSelectionAlone(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)
	setActiveServerProfile("cabin")
	if cfg, err := verbConfig(paths, verbOptions{call: &verbCall{server: "default"}}); err != nil || cfg.profileName() != "default" || activeServerProfile() != "cabin" {
		t.Fatalf("a read: %q %v, process %q", cfg.profileName(), err, activeServerProfile())
	}
	if cfg, err := verbConfig(paths, verbOptions{call: &verbCall{server: "default", serial: true}}); err != nil || cfg.profileName() != "default" || activeServerProfile() != "default" {
		t.Fatalf("a change: %q %v, process %q", cfg.profileName(), err, activeServerProfile())
	}
}
