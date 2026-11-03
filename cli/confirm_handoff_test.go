package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// After a window or phone Confirm, apply takes over the ask's Link route,
// and a failure that sent nothing says the human had confirmed
// (docs/work/2026-11-03-link-route-timeouts-spec.md, Follow-up).

type routeStub struct {
	selections int
	via        relayVia
	delay      time.Duration
	err        error
}

// stubRoute counts route choices; via, delay, and err shape the next ones.
func stubRoute(t *testing.T) *routeStub {
	t.Helper()
	stub := &routeStub{via: relayViaLocal}
	previous := resolveLocalRelayTransportForCLI
	resolveLocalRelayTransportForCLI = func(ctx context.Context, cfg runtimeConfig) (relayTransportSelection, error) {
		stub.selections++
		time.Sleep(stub.delay)
		if stub.err != nil {
			return relayTransportSelection{}, stub.err
		}
		selected, err := previous(ctx, cfg)
		selected.Via = stub.via
		return selected, err
	}
	t.Cleanup(func() { resolveLocalRelayTransportForCLI = previous })
	return stub
}

// A Confirm over Link in the window, on the phone, or in the window while
// the phone asks too: apply runs on the route the ask logged in to.
func TestConfirmOverLinkHandsTheRouteToApply(t *testing.T) {
	for _, channel := range []string{"window", "phone", "both"} {
		t.Run(channel, func(t *testing.T) {
			h := newWriteHarness(t, newFakeHA())
			if channel != "window" {
				p := usePhones(t, "mobile_app_iphone")
				if channel == "phone" {
					p.answer = func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "owner-id", "REMOTE")} }
				}
			}
			if channel != "phone" {
				useWindow(t, windowConfirmed)
			}
			route := stubRoute(t)
			route.via = relayViaCloud
			id := h.plan("lock.unlock", "lock.front").Plan.ID
			route.selections = 0
			var exit int
			var env verbTestEnvelope
			captureStderr(t, func() { exit, env = h.run(runApplyCommand, "--plan", id) })
			if exit != 0 || env.Status != "verified" || len(h.posts()) != 1 || route.selections != 1 {
				t.Fatalf("exit %d status %q err %+v posts %d route choices %d, want one (the ask's)", exit, env.Status, env.Error, len(h.posts()), route.selections)
			}
		})
	}
}

// A local route, or a configuration that changed while the window was
// open, is chosen again.
func TestApplyChoosesItsRouteAgainUnlessTheSameLinkConfig(t *testing.T) {
	for _, name := range []string{"local", "config changed"} {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, newFakeHA())
			w := useWindow(t, windowConfirmed)
			route := stubRoute(t)
			if name == "config changed" {
				route.via = relayViaCloud
				w.before = func() {
					cfg, err := loadConfig(h.paths)
					cfg.HAHost = "127.0.0.2"
					if err != nil || saveConfig(h.paths, cfg) != nil {
						t.Errorf("change config: %v", err)
					}
				}
			}
			id := h.plan("lock.unlock", "lock.front").Plan.ID
			route.selections = 0
			if exit, env := h.run(runApplyCommand, "--plan", id); exit != 0 || len(h.posts()) != 1 || route.selections != 2 {
				t.Fatalf("exit %d err %+v posts %d route choices %d, want 2", exit, env.Error, len(h.posts()), route.selections)
			}
		})
	}
}

// The window's wait counts from the start of the ask: its own login is
// part of it, so a late Confirm still leaves apply its time.
func TestWindowWaitCountsTheAskLogin(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	useWindow(t, windowConfirmed)
	inner, waited := windowRunner, time.Duration(0)
	windowRunner = func(ctx context.Context, message, lang string, wait time.Duration) windowAnswer {
		waited = wait
		return inner(ctx, message, lang, wait)
	}
	route := stubRoute(t)
	id := h.plan("lock.unlock", "lock.front").Plan.ID
	route.delay = 400 * time.Millisecond
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 0 || waited > cliWindowWait-350*time.Millisecond {
		t.Fatalf("exit %d err %+v: window wait %v after a 0.4 s login, want at most %v", exit, env.Error, waited, cliWindowWait-350*time.Millisecond)
	}
}

// Confirmed, then nothing reached Home Assistant: the answer says so, keeps
// the plan, and never invites a retry on the agent's own. A write whose
// outcome is unknown stays OUTCOME_UNKNOWN.
func TestConfirmedButNotSentSaysWhatIsKnown(t *testing.T) {
	previous := relayWriteTimeout
	relayWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayWriteTimeout = previous })
	for _, name := range []string{"read fails", "session fails", "write unknown"} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHA()
			f.storeConfig("/api/config/automation/config/n1", map[string]any{"alias": "Night", "actions": []any{}})
			h := newWriteHarness(t, f)
			w := useWindow(t, windowConfirmed)
			route := stubRoute(t)
			w.before = func() {
				switch name {
				case "read fails":
					h.f.fail["/api/config/automation/config/n1"] = true
				case "session fails":
					route.err = errors.New("connection refused")
				case "write unknown":
					h.f.fail["/api/services/lock/unlock"] = true
				}
			}
			var planned verbTestEnvelope
			if name == "read fails" {
				_, planned = h.run(runPlanCommand, "delete", "automation.night")
			} else {
				planned = h.plan("lock.unlock", "lock.front")
			}
			id := planned.Plan.ID
			exit, env := h.run(runApplyCommand, "--plan", id)
			if name == "write unknown" {
				if exit != 7 || env.Error == nil || env.Error.Code != "OUTCOME_UNKNOWN" || !env.OutcomeUnknown {
					t.Fatalf("exit %d env %+v", exit, env)
				}
				return
			}
			if exit != 1 || env.Error == nil || env.Error.Code != "CONFIRMED_NOT_SENT" || !strings.Contains(env.Error.Message, "nothing was sent") ||
				!strings.Contains(env.Error.Next, "the user confirmed") || !strings.Contains(env.Error.Next, "Read the target once with ha-nova get") ||
				!strings.Contains(env.Error.Next, "Never apply or call it again on your own") || !strings.Contains(env.Error.Next, "ha-nova apply --plan "+id+" again") ||
				strings.Contains(env.Error.Next, "retrying is safe") || len(h.posts()) != 0 || len(h.planFiles()) != 1 {
				t.Fatalf("exit %d env %+v posts %d plans %v", exit, env, len(h.posts()), h.planFiles())
			}
		})
	}
}
