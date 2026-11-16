package main

import (
	"os"
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

// A kept id for other requests (an edited file) or past the ask's life is
// never joined: the apply asks afresh.
func TestGateKeptAskOnlyForTheSameRequests(t *testing.T) {
	for name, edit := range map[string]func(*gateKeptAsk){
		"other requests": func(k *gateKeptAsk) { k.Digest = strings.Repeat("0", 64) },
		"past its life":  func(k *gateKeptAsk) { k.Until = time.Now().Add(-time.Second) },
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
			gateKeepAsk(h.paths, plan.ID, kept)
			close(g.release)
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			asks, _, sends := g.snapshot()
			if exit != 0 || len(asks) != 2 || len(sends) != 1 || sends[0].grant != "grant-ask2" {
				t.Fatalf("exit %d %+v; asks %d sends %+v", exit, env.Error, len(asks), sends)
			}
		})
	}
}
