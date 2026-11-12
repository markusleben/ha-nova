package main

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// ha-nova mcp runs ha_apply as a job (mcp_stdio_apply.go).

func shortApplyWait(t *testing.T) {
	old := stdioApplyWait
	stdioApplyWait = 300 * time.Millisecond
	t.Cleanup(func() { stdioApplyWait = old })
}

// A slow apply answers applying before the AI app gives up on the call;
// calling again joins the same run, a cancelled call changes nothing, and
// the outcome is handed out once. Meanwhile another call, another plan's
// apply included, runs nothing and answers BUSY; ha_skill still answers.
func TestMCPSlowApplyIsAJob(t *testing.T) {
	shortApplyWait(t)
	f := batchHome()
	h := newWriteHarness(t, f)
	id := h.writePlan("script", "s1", arrival("light.turn_off"))
	other := h.writePlan("script", "s2", arrival("light.turn_on"))
	reached, release := holdPost(t, f, "/api/config/script/config/")
	c := startMCP(t, h.paths)
	mcpInit(c)
	apply := map[string]any{"plan_id": id}
	for i := range 2 {
		env := envelopeOf(c.tool("ha_apply", apply))
		if env["status"] != "applying" || !strings.Contains(nextOf(env), "call ha_apply again with plan_id "+id) {
			t.Fatalf("call %d: %v", i, env)
		}
		<-reached
	}
	c.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`)
	for name, args := range map[string]map[string]any{"ha_get": {"entity_id": "light.kitchen"}, "ha_apply": {"plan_id": other}} {
		if env := envelopeOf(c.tool(name, args)); envCode(env) != "BUSY" || !strings.Contains(nextOf(env), "(plan_id "+id+") is still running: call ha_apply with that plan_id") {
			t.Fatalf("%s meanwhile: %v", name, env)
		}
	}
	if result := c.tool("ha_skill", map[string]any{"name": "scene"}); envCode(envelopeOf(result)) == "BUSY" {
		t.Fatalf("ha_skill meanwhile: %v", result)
	}
	release()
	var env map[string]any
	waitFor(t, "the outcome", func() bool { env = envelopeOf(c.tool("ha_apply", apply)); return env["status"] != "applying" })
	if env["status"] != "verified" || len(h.writes()) != 1 {
		t.Fatalf("outcome: %v (writes %v)", env, h.writes())
	}
	if env := envelopeOf(c.tool("ha_apply", apply)); envCode(env) != "PLAN_NOT_FOUND" || len(h.writes()) != 1 {
		t.Fatalf("after the outcome: %v (writes %v)", env, h.writes())
	}
	if env := envelopeOf(c.tool("ha_apply", map[string]any{"plan_id": other})); env["status"] != "verified" || len(h.writes()) != 2 {
		t.Fatalf("the other plan afterwards: %v (writes %v)", env, h.writes())
	}
}

// When the AI app closes the server's input while a change runs, the server
// returns only once the change has ended.
func TestMCPEndOfInputWaitsForTheChange(t *testing.T) {
	shortApplyWait(t)
	f := batchHome()
	h := newWriteHarness(t, f)
	id := h.writePlan("script", "s1", arrival("light.turn_off"))
	reached, release := holdPost(t, f, "/api/config/script/config/")
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		_ = serveMCP(h.paths, inR, outW)
		close(done)
	}()
	c := &mcpClient{t: t, in: inW, out: bufio.NewReader(outR)}
	mcpInit(c)
	go func() { _, _ = io.Copy(io.Discard, outR) }()
	c.send(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ha_apply","arguments":{"plan_id":"` + id + `"}}}`)
	<-reached
	_ = inW.Close()
	select {
	case <-done:
		t.Fatal("the server returned while the change ran")
	case <-time.After(500 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the server never returned")
	}
	if len(h.writes()) != 1 {
		t.Fatalf("writes %v", h.writes())
	}
}

// An apply that crashes may have sent its change: it answers
// OUTCOME_UNKNOWN, never a plain tool error; another verb's crash stays one.
func TestMCPApplyCrashIsUnknown(t *testing.T) {
	srv := &mcpServer{paths: runtimePaths{ConfigDir: t.TempDir()}, told: map[string]bool{}}
	crash := func(*verbCall) int { panic("boom") }
	mcpVerbLock.Lock()
	if env := envelopeOf(srv.runLocked("ha_apply", "", false, crash)); envCode(env) != "OUTCOME_UNKNOWN" || !strings.Contains(nextOf(env), "Never apply this plan again") {
		t.Fatalf("apply: %v", env)
	}
	mcpVerbLock.Lock()
	if env := envelopeOf(srv.runLocked("ha_get", "", false, crash)); envCode(env) != "TOOL_ERROR" {
		t.Fatalf("get: %v", env)
	}
}

// Once ha-nova mcp is told to stop, a plan's window that is still to come
// never opens and nothing is sent; the plan stays for later.
func TestStopSignalEndsTheAskUnsent(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	useWindow(t) // any window opening fails the test
	plan := h.plan("lock.unlock", "lock.front").Plan
	asks := planAsks
	t.Cleanup(func() { planAsks = asks })
	ended, end := context.WithCancel(context.Background())
	end()
	planAsks = ended
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "CONFIRM_CANCELLED" || len(h.posts()) != 0 {
		t.Fatalf("stopping: exit %d %+v (sent %d)", exit, env.Error, len(h.posts()))
	}
}
