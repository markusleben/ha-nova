package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The agent and the waiting window say why this computer's window can't
// confirm (GET /gate/v1/me): one catalog line, and for a Mac's old sign-in
// the command that finishes it, in say too; never a raw App string.
func TestGateMeWhyThisWindowCantConfirm(t *testing.T) {
	g := &fakeGate{release: make(chan struct{})}
	h := gatedHarness(t, newFakeHA(), g)
	h.log.me.Store(map[string]any{"window": false, "why": "legacy_cleanup_pending"})
	var texts []string
	windowConfirmStubs(t, func(windowText) windowAnswer { return windowNoAnswer }, func(ctx context.Context, message string) windowAnswer {
		texts = append(texts, message)
		close(g.release)
		<-ctx.Done()
		return windowNoAnswer
	})
	env := h.plan("lock.unlock", "lock.front")
	cleanup := confirmT("en", "gate.why_cleanup", "command", "ha-nova setup --server default")
	if !strings.HasSuffix(env.Say, confirmT("en", "say.relay_ask")+" "+cleanup) {
		t.Fatalf("say %q", env.Say)
	}
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 || len(texts) != 1 || !strings.HasPrefix(texts[0], cleanup+" "+confirmT("en", "gate.window", "approve", confirmT("en", "page.approve"))) {
		t.Fatalf("exit %d %+v; window %q", exit, applied.Error, texts)
	}
	for why, want := range map[string]string{"off": "gate.why_off", "not_yours": "gate.why_not_yours", "pair_again": "gate.why_pair_again", "file": "gate.why_file", "owner": "gate.why_owner", "on": "", "device": "", "": ""} {
		if got := gateWindowWhy("de", why, "default"); want == "" && got != "" || want != "" && got != confirmT("de", want) {
			t.Fatalf("%q: %q", why, got)
		}
	}
	if gateCleanupCommand("Büro; rm") != "ha-nova setup" || gateCleanupCommand("buero-2") != "ha-nova setup --server buero-2" {
		t.Fatal("only a valid profile name enters the command")
	}
}

// An answer outside the closed set, or an App without the route, reads as
// unread: today's texts, no window named.
func TestGateMeUnreadKeepsTodaysTexts(t *testing.T) {
	for name, me := range map[string]any{"no route": nil, "unknown why": map[string]any{"window": true, "why": "maybe"}, "window, not on": map[string]any{"window": true, "why": "off"}} {
		t.Run(name, func(t *testing.T) {
			h := gatedHarness(t, newFakeHA(), &fakeGate{})
			if me != nil {
				h.log.me.Store(me)
			}
			useWindow(t)
			env := h.plan("lock.unlock", "lock.front")
			if !strings.HasSuffix(env.Say, confirmT("en", "say.relay_ask")) || strings.Contains(env.NextStep, "window") || h.log.meGot.Load() != 1 {
				t.Fatalf("say %q next %q reads %d", env.Say, env.NextStep, h.log.meGot.Load())
			}
		})
	}
}

// A window the App lets confirm, named only where one opens here.
func TestGateMeNamesTheWindowOnlyWhereItOpens(t *testing.T) {
	h := gatedHarness(t, newFakeHA(), &fakeGate{})
	h.log.me.Store(map[string]any{"window": true, "why": "on"})
	if env := h.plan("lock.unlock", "lock.front"); !strings.HasSuffix(env.Say, confirmT("en", "say.relay_ask")) || strings.Contains(env.NextStep, "window") {
		t.Fatalf("no display: say %q next %q", env.Say, env.NextStep)
	}
	useWindow(t)
	if env := h.plan("lock.unlock", "lock.front"); !strings.HasSuffix(env.Say, confirmT("en", "say.relay_window_ask")) || !strings.Contains(env.NextStep, "a confirmation window on their computer") {
		t.Fatalf("a display: say %q next %q", env.Say, env.NextStep)
	}
}

// ha-nova mcp's waiting answer names the window only while its ask shows
// Confirm there.
func TestGateWaitingAnswerNamesTheWindow(t *testing.T) {
	for phase, want := range map[int32]string{gateAsking: "say.relay_waiting", gateAskingWindow: "say.relay_window_waiting", gateLinking: "say.relay_link_waiting"} {
		envelope := waitingForApproval("p_1", "de", phase)["structuredContent"].(map[string]any)
		if envelope["say"] != confirmT("de", want) || envelope["status"] != "waiting_for_approval" {
			t.Fatalf("phase %d: %+v", phase, envelope)
		}
	}
}

// Through Link, GET /gate/v1/me is its own functional route.
func TestCloudGateMeEndpoint(t *testing.T) {
	endpoint, ok := cloudFunctionalEndpoint("GET", gateMeRoute)
	if !ok || endpoint != CloudEndpointGateMe || !isCloudFunctionalIngressEndpoint(endpoint) {
		t.Fatalf("%q %v", endpoint, ok)
	}
	if _, ok := cloudFunctionalEndpoint("POST", gateMeRoute); ok {
		t.Fatal("only GET")
	}
}

// The window is named only where its Confirm can show the plan: a plan too
// long for it keeps the phone and the page in say and next_step.
func TestGateMeNamesTheWindowOnlyForAPlanItFits(t *testing.T) {
	saved := windowAvailable
	t.Cleanup(func() { windowAvailable = saved })
	windowAvailable = func() bool { return true }
	s := &relaySession{paths: runtimePaths{ConfigFile: filepath.Join(t.TempDir(), "none.json")},
		gate: &relayGate{checked: true, on: true, me: &gateWindowState{window: true, why: "on"}}}
	step := "Run apply: " + channelAsks[channelRelay] + " asks them."
	short := storedPlan{Kind: "call", Lang: "en", Prompt: "Unlock?\n\nFront door"}
	long := storedPlan{Kind: "call", Lang: "en", Prompt: "Turn these off?\n\n" + strings.Repeat("• light.room\n", 60)}
	env := &verbEnvelope{NextStep: step}
	if got := relayAskSay("en", s, short, env); got != confirmT("en", "say.relay_window_ask") || !strings.Contains(env.NextStep, "a confirmation window on their computer") {
		t.Fatalf("fits: %q / %q", got, env.NextStep)
	}
	env = &verbEnvelope{NextStep: step}
	if got := relayAskSay("en", s, long, env); got != confirmT("en", "say.relay_ask") || env.NextStep != step {
		t.Fatalf("too long: %q / %q", got, env.NextStep)
	}
}

// A job's waiting answer stops naming the window once it closed or never
// opened, while the ask still waits for the phone and the page.
func TestGateWaitingPhaseLeavesAFailedWindow(t *testing.T) {
	saved := gateApproval
	t.Cleanup(func() { gateApproval = saved })
	gateApproval = &atomic.Int32{}
	g := &fakeGate{release: make(chan struct{}), phones: true, channels: []string{"window"}}
	var during, after int32
	windowConfirmStubs(t, func(windowText) windowAnswer {
		during = gateApproval.Load()
		go func() {
			time.Sleep(300 * time.Millisecond)
			after = gateApproval.Load()
			close(g.release)
		}()
		return windowFailed
	}, nil)
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 || during != gateAskingWindowOnly || after != gateAsking { // the ask's only channel is the window
		t.Fatalf("exit %d %+v; phase %d then %d", exit, env.Error, during, after)
	}
}
