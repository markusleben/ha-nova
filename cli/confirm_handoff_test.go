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

// Confirmed, then nothing reached Home Assistant: the answer says so, keeps
// the plan, and never invites a retry on the agent's own. A write whose
// outcome is unknown stays OUTCOME_UNKNOWN.
func TestConfirmedButNotSentSaysWhatIsKnown(t *testing.T) {
	previous := relayWriteTimeout
	relayWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayWriteTimeout = previous })
	says := map[string]string{ // approved wording, pinned
		"en": "Not done: you confirmed, but HA NOVA couldn't reach Home Assistant, so nothing changed.",
		"de": "Nicht erledigt: Du hast bestätigt, aber HA NOVA hat Home Assistant nicht erreicht. Es wurde nichts geändert.",
	}
	for _, c := range []struct{ name, lang string }{{"read fails", "en"}, {"session fails", "en"}, {"session fails", "de"}, {"dashboard read fails", "de"}, {"write unknown", "en"}} {
		name := c.name
		t.Run(name+"/"+c.lang, func(t *testing.T) {
			f := newFakeHA()
			if name == "dashboard read fails" {
				f = dashboardFake()
			}
			f.config = map[string]any{"language": c.lang}
			f.storeConfig("/api/config/automation/config/n1", map[string]any{"alias": "Night", "actions": []any{}})
			h := newWriteHarness(t, f)
			w := useWindow(t, windowConfirmed)
			route := stubRoute(t)
			plan := []string{"call", "lock.unlock", "lock.front"}
			switch name {
			case "read fails":
				plan = []string{"delete", "automation.night"}
				w.before = func() { h.f.fail["/api/config/automation/config/n1"] = true }
			case "session fails":
				w.before = func() { route.err = errors.New("connection refused") }
			case "dashboard read fails": // the plan, the ask, and apply read it; the re-read after the copy fails
				plan = []string{"delete", "--domain", "dashboard", "--id", "home-main"}
				f.lovelace.failReadsFrom = 4
			case "write unknown":
				w.before = func() { h.f.fail["/api/services/lock/unlock"] = true }
			}
			_, planned := h.run(runPlanCommand, plan...)
			id := planned.Plan.ID
			exit, env := h.run(runApplyCommand, "--plan", id)
			if name == "write unknown" {
				if exit != 7 || env.Error == nil || env.Error.Code != "OUTCOME_UNKNOWN" || !env.OutcomeUnknown {
					t.Fatalf("exit %d env %+v", exit, env)
				}
				return
			}
			if exit != 1 || env.Error == nil || env.Error.Code != "CONFIRMED_NOT_SENT" || !strings.Contains(env.Error.Message, "nothing was sent") ||
				!strings.Contains(env.Error.Next, "the user confirmed") || !strings.Contains(env.Error.Next, "Read the target once with ha-nova get --server default to check") ||
				!strings.Contains(env.Error.Next, "Never apply or call it again on your own") || !strings.Contains(env.Error.Next, "ha-nova apply --plan "+id+" again") ||
				strings.Contains(env.Error.Next, "retrying is safe") || env.Say != says[c.lang] || len(h.posts()) != 0 || len(h.planFiles()) != 1 {
				t.Fatalf("exit %d env %+v posts %d plans %v", exit, env, len(h.posts()), h.planFiles())
			}
			if got := mcpNext(env.Error.Next+" Never run it without --server or HA_NOVA_SERVER.", false); strings.Contains(got, "ha_get --server") || !strings.Contains(got, "without --server or") || !strings.Contains(got, "ha_get to check") || !strings.Contains(got, "ha_apply with plan_id "+id+" again") {
				t.Fatalf("MCP next %q", got)
			}
			if name == "dashboard read fails" && (len(h.lovelaceSaves()) != 0 || len(f.snapshots) != 1) {
				t.Fatalf("dashboard writes %v snapshots %d", h.lovelaceSaves(), len(f.snapshots))
			}
		})
	}
}

// Without a human ask, the same dashboard failure keeps the plan for the
// same apply, as a config or service plan does.
func TestDashboardReadFailureBeforeSaveKeepsThePlan(t *testing.T) {
	f := dashboardFake()
	h := newWriteHarness(t, f)
	_, planned := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome()))
	f.lovelace.failReadsFrom = 3 // the plan and apply read it; the re-read after the copy fails
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != 1 || env.Error == nil || env.Error.Code != "RELAY_ERROR" || !strings.Contains(env.Error.Next, "retrying is safe") || len(h.lovelaceSaves()) != 0 || len(h.planFiles()) != 1 {
		t.Fatalf("exit %d env %+v saves %v plans %v", exit, env, h.lovelaceSaves(), h.planFiles())
	}
	f.lovelace.failReadsFrom = 0
	if exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID); exit != 0 || env.Status != "verified" || len(h.lovelaceSaves()) != 1 {
		t.Fatalf("retry: exit %d env %+v", exit, env)
	}
}

// A chat app keeps RELAY_ERROR and its own step, which never names a
// terminal command.
func TestChatAppKeepsItsRelayStepAfterATap(t *testing.T) {
	savedAnswer, savedChat := humanAnswer, chatCall
	humanAnswer, chatCall = "1234", &chatPolicy{}
	t.Cleanup(func() { humanAnswer, chatCall = savedAnswer, savedChat })
	failure := nothingSent(errors.New("connection refused"))
	confirmedNotSent(failure, storedPlan{ID: "p_000000000000", Lang: "en"}, "default", true)
	var env verbEnvelope
	if setVerbError(&env, failure); env.Error.Code != "RELAY_ERROR" || env.Say != "" || strings.Contains(env.Error.Next, "ha-nova get") {
		t.Fatalf("chat app: %+v say %q", env.Error, env.Say)
	}
}
