package main

import (
	"strings"
	"testing"
	"time"
)

// Q6: a failed pre-apply read sends nothing, says so, and keeps the plan, so
// the same apply can be retried.
func TestApplyReadFailureBeforeSendKeepsThePlan(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
	h := newWriteHarness(t, f)
	_, planned := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, hallDraft("light.turn_off")))
	h.f.fail["/api/config/automation/config/a1"] = true
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != 1 || env.Error == nil || env.Error.Code != "RELAY_ERROR" || !strings.Contains(env.Error.Message, "nothing was sent") ||
		!strings.Contains(env.Error.Next, "retrying is safe") || env.Say != "" || len(h.writes()) != 0 {
		t.Fatalf("exit %d env %+v writes %v", exit, env, h.writes())
	}
	h.f.fail["/api/config/automation/config/a1"] = false
	if exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID); exit != 0 || env.Status != "verified" || len(h.writes()) != 1 {
		t.Fatalf("retry: exit %d env %+v", exit, env)
	}
}

// A write the Relay refuses before forwarding sends nothing to Home
// Assistant: the plan is released for a retry. After a send whose outcome is
// unknown, the plan is gone and the answer never invites a retry.
func TestApplySendFailureSaysWhetherItWasSent(t *testing.T) {
	previous := relayWriteTimeout
	relayWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayWriteTimeout = previous })
	f := newFakeHA()
	f.related["area:kitchen"] = []string{"light.kitchen", "light.hall"}
	f.ent("light.hall", "hue", "on", nil)
	h := newWriteHarness(t, f)
	id := h.plan("light.turn_off", "--area", "kitchen").Plan.ID
	h.f.service = func(*fakeHA, string, map[string]any) any { return fakeRelayRefusal{"VALIDATION_ERROR"} }
	exit, env := h.run(runApplyCommand, "--plan", id)
	if exit != 1 || env.OutcomeUnknown || !strings.Contains(env.Error.Message, "nothing was sent") || !strings.Contains(env.Error.Next, "retrying is safe") {
		t.Fatalf("refused: exit %d env %+v", exit, env)
	}
	h.f.service = applies
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 0 || env.Status != "verified" {
		t.Fatalf("retry: exit %d env %+v", exit, env)
	}

	h.f.states["light.kitchen"]["state"], h.f.states["light.hall"]["state"] = "on", "on"
	id = h.plan("light.turn_off", "--area", "kitchen").Plan.ID
	h.f.fail["/api/services/light/turn_off"] = true
	exit, env = h.run(runApplyCommand, "--plan", id)
	if exit != 7 || !env.OutcomeUnknown || strings.Contains(env.Error.Message+env.Error.Next, "nothing was sent") || !strings.Contains(env.Error.Next, "never retry") {
		t.Fatalf("unknown: exit %d env %+v", exit, env)
	}
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 4 || env.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("an unknown outcome kept the plan: exit %d env %+v", exit, env)
	}
}
