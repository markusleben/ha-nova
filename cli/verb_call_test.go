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
