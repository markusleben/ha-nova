package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// gateWaitEnds runs apply --plan id with only about a second of the
// command's wait left, so its own wait ends before the person answers.
func gateWaitEnds(t *testing.T, h *writeHarness, id string) (int, verbTestEnvelope) {
	t.Helper()
	saved := commandStart
	defer func() { commandStart = saved }()
	commandStart = time.Now().Add(-(windowWait - time.Second))
	return h.run(runApplyCommand, "--plan", id)
}

// The next apply of the plan joins the ask the command's wait left open:
// an OK given meanwhile counts, nobody is asked again, and the kept id is
// gone after.
func TestGateNextApplyJoinsTheOpenAsk(t *testing.T) {
	g := &fakeGate{release: make(chan struct{})}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	if exit, env := gateWaitEnds(t, h, plan.ID); exit != verbExitConfirmation || env.Status != "waiting_for_approval" {
		t.Fatalf("first: exit %d %+v", exit, env.Error)
	}
	close(g.release) // the person approves meanwhile
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	asks, ops, sends := g.snapshot()
	if exit != 0 || len(asks) != 1 || slices.Contains(ops, "cancel") || len(sends) != 1 || sends[0].grant != "grant-ask1" {
		t.Fatalf("join: exit %d %+v; asks %d ops %v sends %+v", exit, env.Error, len(asks), ops, sends)
	}
	if _, err := os.Stat(gateKeptPath(h.paths, plan.ID)); !os.IsNotExist(err) {
		t.Fatalf("kept ask left behind: %v", err)
	}
}

// A kept ask that ended unanswered, or one the App no longer holds, is
// asked again, as before; one cancelled (or replaced by another ask) ends
// the apply declined, never asked again on its own.
func TestGateJoinedAskThatEnded(t *testing.T) {
	for name, tc := range map[string]struct {
		end   any // the kept ask's first poll answer
		asks  int
		exit  int
		code  string
		sends int
	}{
		"expired":   {map[string]any{"state": "expired"}, 2, 0, "", 1},
		"unknown":   {fakeRelayStatus{404, "GATE_ASK_UNKNOWN", "No such ask from this device"}, 2, 0, "", 1},
		"cancelled": {map[string]any{"state": "cancelled"}, 1, verbExitRefused, "CONFIRM_DECLINED", 0},
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGate{release: make(chan struct{})}
			h := gatedHarness(t, newFakeHA(), g)
			plan := h.plan("lock.unlock", "lock.front").Plan
			gateWaitEnds(t, h, plan.ID)
			close(g.release)
			g.mu.Lock()
			g.waitFor = map[string]any{"ask1": tc.end}
			g.mu.Unlock()
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			asks, _, sends := g.snapshot()
			if exit != tc.exit || len(asks) != tc.asks || len(sends) != tc.sends || tc.code != "" && (env.Error == nil || env.Error.Code != tc.code) {
				t.Fatalf("exit %d %+v; asks %d sends %+v", exit, env.Error, len(asks), sends)
			}
		})
	}
}

// A kept id for other requests (an edited file) is never joined: the
// apply asks afresh.
func TestGateKeptAskOnlyForTheSameRequests(t *testing.T) {
	for name, edit := range map[string]func(*gateKeptAsk){
		"other requests": func(k *gateKeptAsk) { k.Digest = strings.Repeat("0", 64) },
		"no id":          func(k *gateKeptAsk) { k.AskID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGate{release: make(chan struct{})}
			h := gatedHarness(t, newFakeHA(), g)
			plan := h.plan("lock.unlock", "lock.front").Plan
			gateWaitEnds(t, h, plan.ID)
			reqs := []gateRequest{{"core", []byte(strings.TrimPrefix(gateUnlock, "core "))}}
			kept, ok := gateTakeAsk(h.paths, plan.ID, reqs)
			if !ok || kept.AskID != "ask1" {
				t.Fatalf("kept %+v %v", kept, ok)
			}
			edit(&kept)
			if err := gateKeepAsk(h.paths, plan.ID, kept); err != nil {
				t.Fatal(err)
			}
			close(g.release)
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			asks, _, sends := g.snapshot()
			if exit != 0 || len(asks) != 2 || len(sends) != 1 || sends[0].grant != "grant-ask2" {
				t.Fatalf("exit %d %+v; asks %d sends %+v", exit, env.Error, len(asks), sends)
			}
		})
	}
}

// A Cancel in the window that races the end of the command's wait wins:
// the ask is cancelled, and nothing is kept to join.
func TestGateWaitEndRacingACancel(t *testing.T) {
	g := &fakeGate{release: make(chan struct{})}
	windowConfirmStubs(t, func(windowText) windowAnswer { return windowNoAnswer }, func(ctx context.Context, _ string) windowAnswer {
		<-ctx.Done()
		return windowCancelled
	})
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	exit, env := gateWaitEnds(t, h, plan.ID)
	_, ops, sends := g.snapshot()
	if exit != verbExitRefused || env.Error == nil || env.Error.Code != "CONFIRM_DECLINED" || !slices.Contains(ops, "cancel") || len(sends) != 0 {
		t.Fatalf("exit %d %+v; ops %v sends %+v", exit, env.Error, ops, sends)
	}
	if _, err := os.Stat(gateKeptPath(h.paths, plan.ID)); !os.IsNotExist(err) {
		t.Fatalf("kept after a Cancel: %v", err)
	}
}

// An ask that can't be kept (an unwritable plan folder) is ended, never
// promised to the next apply.
func TestGateAskThatCantBeKeptEnds(t *testing.T) {
	g := &fakeGate{release: make(chan struct{})}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	if err := os.MkdirAll(filepath.Join(gateKeptPath(h.paths, plan.ID), "x"), 0o700); err != nil { // a folder where the kept ask would go
		t.Fatal(err)
	}
	exit, env := gateWaitEnds(t, h, plan.ID)
	_, ops, _ := g.snapshot()
	if exit != verbExitRefused || env.Error == nil || env.Error.Code != "CONFIRM_CANCELLED" || !slices.Contains(ops, "cancel") {
		t.Fatalf("exit %d %+v; ops %v", exit, env.Error, ops)
	}
}

// A plan used up or refused for good ends the ask kept for it.
func TestGateKeptAskEndsWithItsPlan(t *testing.T) {
	g := &fakeGate{release: make(chan struct{})}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	gateWaitEnds(t, h, plan.ID)
	saved := verbNow
	t.Cleanup(func() { verbNow = saved })
	verbNow = func() time.Time { return saved().Add(planTTL + time.Second) } // the plan expired meanwhile
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	_, ops, sends := g.snapshot()
	if exit != verbExitRefused || env.Error == nil || env.Error.Code != "PLAN_EXPIRED" || !slices.Contains(ops, "cancel") || len(sends) != 0 {
		t.Fatalf("exit %d %+v; ops %v sends %+v", exit, env.Error, ops, sends)
	}
	if _, err := os.Stat(gateKeptPath(h.paths, plan.ID)); !os.IsNotExist(err) {
		t.Fatalf("kept ask left behind: %v", err)
	}
}
