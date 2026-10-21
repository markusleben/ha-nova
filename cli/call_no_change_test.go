package main

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

// A call whose every target already reads as its end state asks nothing and
// sends nothing (the live rc3 report: "unlock" an unlocked door still asked).
func TestCallAlreadyAtTargetIsNoChange(t *testing.T) {
	f := newFakeHA()
	f.ent("lock.front", "zwave_js", "unlocked", map[string]any{"friendly_name": "Front Door"})
	h := newWriteHarness(t, f)
	exit, env := h.run(runCallCommand, "lock.unlock", "lock.front")
	if exit != 0 || env.Status != "no_change" || env.Plan != nil || env.Tier != "" || !strings.HasPrefix(env.NextStep, "Relay `say` only.") ||
		env.Say != "Done: “Front Door” is already unlocked. I checked it." || len(env.Deltas) != 1 {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	if len(h.posts()) != 0 || len(h.planFiles()) != 0 {
		t.Fatalf("posts %d plans %v", len(h.posts()), h.planFiles())
	}
}

// Partial no-op: the plan keeps every target. apply re-plans from the stored
// request (I3), so a narrowed plan would drift at once; unlocking a lock
// that is already unlocked is what Home Assistant would do anyway.
func TestCallPartlyAtTargetPlansEveryTarget(t *testing.T) {
	f := newFakeHA() // lock.front locked
	f.ent("lock.back", "zwave_js", "unlocked", map[string]any{"friendly_name": "Back Door"})
	h := newWriteHarness(t, f)
	exit, env := h.run(runCallCommand, "lock.unlock", "lock.front", "lock.back")
	if exit != 3 || env.Status != "needs_confirmation" || env.Plan == nil || strings.Join(env.Action.EntityIDs, ",") != "lock.front,lock.back" {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	f.states["lock.front"]["state"] = "unlocked"
	exit, env = h.run(runCallCommand, "lock.unlock", "lock.front", "lock.back")
	if exit != 0 || env.Status != "no_change" || env.Say != "Done: “lock.front” and “Back Door” are already unlocked. I checked it." ||
		!strings.Contains(env.NextStep, "same request with ha-nova plan call instead of call") {
		t.Fatalf("all unlocked: exit %d env %+v", exit, env)
	}
	// The way out when Home Assistant missed a manual change: plan call never skips.
	if exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front", "lock.back"); exit != 3 || env.Plan == nil {
		t.Fatalf("plan call: exit %d env %+v", exit, env)
	}
	if len(h.posts()) != 0 {
		t.Fatalf("posts %d", len(h.posts()))
	}
}

// unavailable/unknown prove nothing: the normal confirmation path runs.
func TestCallUnknownStateKeepsTheNormalPath(t *testing.T) {
	for _, state := range []string{"unavailable", "unknown"} {
		f := newFakeHA()
		f.ent("lock.front", "zwave_js", state, nil)
		h := newWriteHarness(t, f)
		if exit, env := h.run(runCallCommand, "lock.unlock", "lock.front"); exit != 3 || env.Plan == nil || env.Status != "needs_confirmation" {
			t.Fatalf("%s: exit %d env %+v", state, exit, env)
		}
	}
}

// The door got unlocked between preview and apply: no re-confirmation of a
// no-op, nothing sent, no new plan.
func TestApplyDriftToTheTargetIsNoChange(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	old := h.plan("lock.unlock", "lock.front").Plan
	h.f.states["lock.front"]["state"] = "unlocked"
	exit, env := h.run(runApplyCommand, "--plan", old.ID, "--confirm", old.Code)
	if exit != 0 || env.Status != "no_change" || env.Plan != nil || len(h.posts()) != 0 || len(h.planFiles()) != 0 {
		t.Fatalf("exit %d env %+v posts %d plans %v", exit, env, len(h.posts()), h.planFiles())
	}
}

func TestPlanExpiredSaysHowLongAgo(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	id := h.plan("lock.unlock", "lock.front").Plan.ID
	previous := verbNow
	verbNow = func() time.Time { return previous().Add(planTTL + 12*time.Minute + time.Second) }
	t.Cleanup(func() { verbNow = previous })
	exit, env := h.run(runApplyCommand, "--plan", id)
	if exit != 4 || env.Error.Code != "PLAN_EXPIRED" || !strings.Contains(env.Error.Message, "expired 12 minutes ago") || strings.Contains(env.Error.Message, "UTC") {
		t.Fatalf("exit %d env %+v", exit, env.Error)
	}
	for d, want := range map[time.Duration]string{0: "just now", 90 * time.Second: "1 minute ago", 3 * time.Hour: "3 hours ago"} {
		if got := sayAgo(d); got != want {
			t.Fatalf("sayAgo(%v) = %q, want %q", d, got, want)
		}
	}
}

// alreadyDone is fail-closed: only the access or "on" direction with a
// known end state, no data, every target reporting that state.
func TestAlreadyDoneOnlyForSafeKnownEndStates(t *testing.T) {
	st := func(state string, attrs map[string]any) *entityState {
		return &entityState{State: state, Attributes: attrs}
	}
	cases := []struct {
		name, service string
		states        map[string]*entityState
		data          map[string]any
		want          bool
	}{
		{"unlock an unlocked lock", "lock.unlock", map[string]*entityState{"lock.a": st("unlocked", nil)}, nil, true},
		{"disarm a disarmed alarm", "alarm_control_panel.alarm_disarm", map[string]*entityState{"alarm_control_panel.a": st("disarmed", nil)}, nil, true},
		{"open a fully open cover", "cover.open_cover", map[string]*entityState{"cover.a": st("open", map[string]any{"current_position": 100.0})}, nil, true},
		{"open a cover without position", "homeassistant.turn_on", map[string]*entityState{"cover.a": st("open", nil)}, nil, true},
		{"enable an enabled automation", "automation.turn_on", map[string]*entityState{"automation.a": st("on", nil)}, nil, true},
		{"turn on an input_boolean that is on", "input_boolean.turn_on", map[string]*entityState{"input_boolean.a": st("on", nil)}, nil, true},
		{"light on: may be a hardware group with members off", "light.turn_on", map[string]*entityState{"light.a": st("on", nil)}, nil, false},
		{"switch on: may be a securing switch", "switch.turn_on", map[string]*entityState{"switch.a": st("on", nil)}, nil, false},
		{"open a half-open cover", "cover.open_cover", map[string]*entityState{"cover.a": st("open", map[string]any{"current_position": 50.0})}, nil, false},
		{"lock a locked lock: stale state could hide a needed lock", "lock.lock", map[string]*entityState{"lock.a": st("locked", nil)}, nil, false},
		{"close a closed cover", "cover.close_cover", map[string]*entityState{"cover.a": st("closed", nil)}, nil, false},
		{"arm an armed alarm", "alarm_control_panel.alarm_arm_away", map[string]*entityState{"alarm_control_panel.a": st("armed_away", nil)}, nil, false},
		{"turn off an off switch", "switch.turn_off", map[string]*entityState{"switch.a": st("off", nil)}, nil, false},
		{"siren on", "siren.turn_on", map[string]*entityState{"siren.a": st("on", nil)}, nil, false},
		{"toggle", "light.toggle", map[string]*entityState{"light.a": st("on", nil)}, nil, false},
		{"script", "script.turn_on", map[string]*entityState{"script.a": st("on", nil)}, nil, false},
		{"lock.open is a momentary unlatch", "lock.open", map[string]*entityState{"lock.a": st("open", nil)}, nil, false},
		{"service data", "input_boolean.turn_on", map[string]*entityState{"input_boolean.a": st("on", nil)}, map[string]any{"x": 1.0}, false},
		{"another domain's service", "lock.unlock", map[string]*entityState{"cover.a": st("unlocked", nil)}, nil, false},
		{"unread state", "lock.unlock", map[string]*entityState{"lock.a": nil}, nil, false},
		{"unlocking in progress", "lock.unlock", map[string]*entityState{"lock.a": st("unlocking", nil)}, nil, false},
		{"assumed state (RF, optimistic)", "lock.unlock", map[string]*entityState{"lock.a": st("unlocked", map[string]any{"assumed_state": true})}, nil, false},
		{"a custom integration's own unlock", "foo.unlock", map[string]*entityState{"foo.a": st("unlocked", nil)}, nil, false},
		{"one of two", "lock.unlock", map[string]*entityState{"lock.a": st("unlocked", nil), "lock.b": st("locked", nil)}, nil, false},
	}
	for _, c := range cases {
		e := &tierEngine{states: c.states, complete: true, events: map[string]int{}}
		ids := slices.Sorted(maps.Keys(c.states))
		action := planAction{Service: c.service, EntityIDs: ids, Data: c.data}
		env := &verbEnvelope{Deltas: planDeltas(c.service, ids, c.data, c.states)}
		if got := alreadyDone(e, env, action); got != c.want {
			t.Errorf("%s: alreadyDone = %v, want %v", c.name, got, c.want)
		}
		if c.want { // what HA NOVA can't see, or what reacts to the call itself, keeps the normal path
			e.complete = false
			if alreadyDone(e, env, action) {
				t.Errorf("%s: alreadyDone with a coverage gap", c.name)
			}
			e.complete, e.events["call_service"] = true, 1
			if alreadyDone(e, env, action) {
				t.Errorf("%s: alreadyDone with a call_service listener", c.name)
			}
		}
	}
}

// An unreachable linked phone refuses a plan, but a call that sends nothing
// needs no confirmation channel at all (Codex review).
func TestNoChangeNeedsNoConfirmationChannel(t *testing.T) {
	f := newFakeHA()
	f.ent("lock.front", "zwave_js", "unlocked", nil)
	h := newWriteHarness(t, f)
	p := usePhones(t)
	p.status = phoneMissing
	if exit, env := h.run(runCallCommand, "lock.unlock", "lock.front"); exit != 0 || env.Status != "no_change" || env.Error != nil {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	f.states["lock.front"]["state"] = "locked" // a real unlock still needs that phone
	if exit, env := h.run(runCallCommand, "lock.unlock", "lock.front"); exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" {
		t.Fatalf("locked: exit %d env %+v", exit, env)
	}
}

// A room target with an entity that reports no state: the user must hear
// about it (EXPANDED_TARGET_WITHOUT_STATE), so the plan path keeps it.
func TestNoChangeNeverHidesSkippedTargets(t *testing.T) {
	f := newFakeHA()
	f.ent("lock.front", "zwave_js", "unlocked", nil)
	f.ent("lock.back", "zwave_js", "unlocked", nil)
	f.compact = append(f.compact, map[string]any{"ei": "lock.gone", "pl": "zwave_js"}) // registered, no state
	f.related["area:hall"] = []string{"lock.front", "lock.back", "lock.gone"}
	h := newWriteHarness(t, f)
	exit, env := h.run(runCallCommand, "lock.unlock", "--area", "hall")
	if exit != 3 || env.Status != "needs_confirmation" || env.Coverage == nil || len(env.Coverage.Skipped) == 0 {
		t.Fatalf("exit %d env %+v", exit, env)
	}
}

// Area membership changed between preview and apply: the confirmed set is
// not what is checked now, so it is a new preview, never a silent no_change.
func TestApplyNoChangeOnlyForTheConfirmedTargets(t *testing.T) {
	f := newFakeHA()
	f.ent("lock.back", "zwave_js", "locked", nil)
	f.related["area:hall"] = []string{"lock.front", "lock.back"}
	h := newWriteHarness(t, f)
	old := h.plan("lock.unlock", "--area", "hall").Plan
	f.related["area:hall"] = []string{"lock.front"}
	f.states["lock.front"]["state"] = "unlocked"
	if exit, env := h.run(runApplyCommand, "--plan", old.ID, "--confirm", old.Code); exit != 2 || env.Status != "drift" || len(h.posts()) != 0 {
		t.Fatalf("exit %d env %+v", exit, env)
	}
}

// The window was confirmed, then the door got unlocked before apply sent:
// nothing is sent, and the phone line says so too.
func TestWindowConfirmedThenAlreadyDone(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	w := useWindow(t, windowConfirmed)
	plan := h.plan("lock.unlock", "lock.front").Plan
	w.before = func() { h.f.states["lock.front"]["state"] = "unlocked" }
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 || env.Status != "no_change" || len(h.posts()) != 0 {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	if title, line := phoneApplyResult(verbEnvelope{Status: "no_change"}, verbExitOK); title != "Already done" || !strings.Contains(line, "nothing was sent") {
		t.Fatalf("phone: %q %q", title, line)
	}
}

// A keypad lock or an alarm that needs a code: Home Assistant's code is
// refused for a real call, but when it is already so nothing needs one.
func TestNoChangeBeforeCodeRequired(t *testing.T) {
	f := newFakeHA()
	f.ent("lock.keypad", "zwave_js", "unlocked", map[string]any{"code_format": "^\\d{4}$", "friendly_name": "Keypad"})
	h := newWriteHarness(t, f)
	if exit, env := h.run(runCallCommand, "lock.unlock", "lock.keypad"); exit != 0 || env.Status != "no_change" || env.Say != "Done: “Keypad” is already unlocked. I checked it." {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	f.states["lock.keypad"]["state"] = "locked"
	if exit, env := h.run(runCallCommand, "lock.unlock", "lock.keypad"); exit != 4 || env.Tier != "refused" {
		t.Fatalf("locked: exit %d env %+v", exit, env)
	}
	if len(h.posts()) != 0 {
		t.Fatalf("posts %d", len(h.posts()))
	}
}
