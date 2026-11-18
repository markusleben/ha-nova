package main

import (
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// The chat code from the shell (MAR-244 §3): the plan says a code will
// come, apply shows the App's code at once and keeps the ask without the
// digits, and apply --confirm sends what the person typed to that very
// ask; only the App's answer approves.

func codeHarness(t *testing.T) (*writeHarness, *fakeGate) {
	t.Helper()
	g := &fakeGate{code: "4821"}
	h := gatedHarness(t, newFakeHA(), g)
	h.log.code.Store(true)
	h.log.me.Store(map[string]any{"window": false, "why": "off", "phone": false, "code": "on"})
	return h, g
}

func TestChatCodeShownOnceThenTypedBack(t *testing.T) {
	h, g := codeHarness(t)
	env := h.plan("lock.unlock", "lock.front")
	if !strings.HasSuffix(env.Say, confirmT("en", "say.relay_code_ask")) || !strings.Contains(env.NextStep, "one-time code") {
		t.Fatalf("plan: say %q next %q", env.Say, env.NextStep)
	}
	exit, shown := h.run(runApplyCommand, "--plan", env.Plan.ID)
	asks, ops, sends := g.snapshot()
	if exit != verbExitConfirmation || shown.Status != "waiting_for_approval" || shown.Error == nil || shown.Error.Code != "CONFIRM_CODE" ||
		!strings.Contains(shown.Say, "4821") || len(asks) != 1 || strings.Join(ops, ",") != "open" || len(sends) != 0 || g.accepts[0] != true {
		t.Fatalf("shown: exit %d %+v say %q; ops %v sends %v", exit, shown.Error, shown.Say, ops, sends)
	}
	if kept, err := os.ReadFile(gateKeptPath(h.paths, env.Plan.ID)); err != nil || strings.Contains(string(kept), "4821") || !strings.Contains(string(kept), `"code":true`) {
		t.Fatalf("kept ask %q %v", kept, err)
	}
	g.codeOps = []any{fakeRelayStatus{409, "GATE_CODE_WRONG", "That code is wrong; nothing was approved"}}
	if exit, wrong := h.run(runApplyCommand, "--confirm", "1111", "--plan", env.Plan.ID); exit == 0 || wrong.Error == nil || wrong.Error.Code != "CODE_MISMATCH" {
		t.Fatalf("wrong: exit %d %+v", exit, wrong.Error)
	}
	if exit, notDigits := h.run(runApplyCommand, "--confirm", "yes", "--plan", env.Plan.ID); exit == 0 || notDigits.Error == nil || notDigits.Error.Code != "CODE_REQUIRED" {
		t.Fatalf("not digits: exit %d %+v", exit, notDigits.Error)
	}
	exit, applied := h.run(runApplyCommand, "--confirm", "48 21", "--plan", env.Plan.ID)
	asks, ops, sends = g.snapshot()
	if exit != 0 || applied.Status != "verified" || len(asks) != 1 || strings.Join(ops, ",") != "open,code 1111,code 4821" || len(sends) != 1 || sends[0].grant != "grant-ask1" {
		t.Fatalf("typed back: exit %d %+v; ops %v sends %+v", exit, applied.Error, ops, sends)
	}
	if _, err := os.Stat(gateKeptPath(h.paths, env.Plan.ID)); !os.IsNotExist(err) {
		t.Fatalf("kept ask left behind: %v", err)
	}
}

func TestChatCodeTurnedOffMeanwhileEndsTheAsk(t *testing.T) {
	h, g := codeHarness(t)
	plan := h.plan("lock.unlock", "lock.front").Plan
	if exit, _ := h.run(runApplyCommand, "--plan", plan.ID); exit != verbExitConfirmation {
		t.Fatalf("shown: exit %d", exit)
	}
	g.codeOps = []any{fakeRelayStatus{409, "GATE_CODE_OFF", "This computer no longer confirms with a chat code; nothing was approved"}}
	exit, off := h.run(runApplyCommand, "--confirm", "4821", "--plan", plan.ID)
	_, ops, sends := g.snapshot()
	if exit == 0 || off.Error == nil || off.Error.Code != "CONFIRM_CODE_OFF" || strings.Join(ops, ",") != "open,code 4821,cancel" || len(sends) != 0 {
		t.Fatalf("off: exit %d %+v; ops %v", exit, off.Error, ops)
	}
}

// An App that doesn't offer the code (before 0.33) gets no accept_code, and
// --confirm on a plan without a kept code ask is refused as before.
func TestChatCodeOnlyWhereTheAppOffersIt(t *testing.T) {
	h, g := codeHarness(t)
	h.log.code.Store(false)
	plan := h.plan("lock.unlock", "lock.front").Plan
	if exit, env := h.run(runApplyCommand, "--confirm", "4821", "--plan", plan.ID); exit == 0 || env.Error == nil || env.Error.Code != "CONFIRM_IN_HOME_ASSISTANT" {
		t.Fatalf("--confirm without a code ask: exit %d %+v", exit, env.Error)
	}
	exit, applied := h.run(runApplyCommand, "--plan", plan.ID)
	_, ops, _ := g.snapshot()
	if exit != 0 || applied.Status != "verified" || g.accepts[0] != nil || strings.Join(ops, ",") != "open,wait" {
		t.Fatalf("older App: exit %d %+v accepts %v ops %v", exit, applied.Error, g.accepts, ops)
	}
}

// The MCP server's apply never asks for a code: its ask has no chat to
// show one in and waits for the phone or the NOVA page.
func TestChatCodeNeverFromTheMCPServer(t *testing.T) {
	h, g := codeHarness(t)
	gateApproval = &atomic.Int32{}
	t.Cleanup(func() { gateApproval = nil })
	plan := h.plan("lock.unlock", "lock.front")
	if strings.Contains(plan.Say, confirmT("en", "say.relay_code_ask")) {
		t.Fatalf("plan promised a code: %q", plan.Say)
	}
	exit, applied := h.run(runApplyCommand, "--plan", plan.Plan.ID)
	_, ops, _ := g.snapshot()
	if exit != 0 || applied.Status != "verified" || g.accepts[0] != nil || strings.Join(ops, ",") != "open,wait" {
		t.Fatalf("mcp: exit %d %+v accepts %v ops %v", exit, applied.Error, g.accepts, ops)
	}
}

// A waiting answer names only where this ask can be confirmed.
func TestWaitingNamesOnlyTheAsksChannels(t *testing.T) {
	for _, c := range []struct {
		channels []string
		window   bool
		say      string
	}{
		{[]string{"phone"}, false, confirmT("en", "say.relay_waiting_phone")},
		{[]string{"page"}, false, confirmT("en", "say.relay_waiting_page")},
		{[]string{"phone", "page"}, false, confirmT("en", "say.relay_waiting")},
		{nil, false, confirmT("en", "say.relay_waiting")},
		{[]string{"page", "window"}, true, confirmT("en", "say.relay_window_waiting")},
		{[]string{"page", "code"}, false, confirmT("en", "say.relay_code_waiting")},
		{[]string{"code"}, false, confirmT("en", "say.relay_code_waiting")},
	} {
		w := gateWaiting("p1", "en", c.window, c.channels)
		if w.say != c.say || slices.Contains(c.channels, "code") != strings.Contains(w.next, "--confirm <those digits> --plan p1") {
			t.Errorf("%v window %v: say %q next %q", c.channels, c.window, w.say, w.next)
		}
	}
	env := waitingForApproval("p1", "en", gateAskingOn([]string{"phone"}))["structuredContent"].(map[string]any)
	if env["say"] != confirmT("en", "say.relay_waiting_phone") || !strings.Contains(nextOf(env), "on their linked phone") || strings.Contains(nextOf(env), "NOVA page") {
		t.Errorf("mcp phone only: %v", env)
	}
	if gateWaitingText("en", []string{"page", "code"}) != confirmT("en", "gate.waiting_code") {
		t.Errorf("terminal line for a code ask: %q", gateWaitingText("en", []string{"page", "code"}))
	}
}
