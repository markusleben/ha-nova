package main

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// PR #668 review round: Codex, race, and strings/errors findings.

// R1: a refusal after the recompute shows the STORED plan, never the
// recompute's unsaved id or code.
func TestApplyRefusalShowsTheStoredPlan(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	planned := h.plan("lock.unlock", "lock.front")
	for _, code := range []string{"", "yes"} {
		args := []string{"--plan", planned.Plan.ID}
		if code != "" {
			args = append(args, "--confirm", code)
		}
		exit, env := h.run(runApplyCommand, args...)
		if exit != 4 || env.Plan == nil || env.Plan.ID != planned.Plan.ID || env.Plan.Code != "" {
			t.Fatalf("code %q: exit %d plan %+v", code, exit, env.Plan)
		}
	}
	if exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code); exit != 0 {
		t.Fatalf("the stored code no longer applies: exit %d %+v", exit, env.Error)
	}
}

// C3: a failed loaded-config read is a failed read, never "no config".
func TestLoadedConfigReadFailurePropagates(t *testing.T) {
	cases := []tierCase{
		{name: "trigger of an id-less automation whose loaded config fails", setup: func(f *fakeHA) {
			f.automation("night", "", nil)
			f.fail["automation/config"] = true
		}, args: []string{"automation.trigger", "automation.night"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}, absent: []string{reasonYAMLNoID}},
		{name: "the listener scan types it as a failed read", setup: func(f *fakeHA) {
			f.automation("night", "", nil)
			f.fail["automation/config"] = true
			f.events = []map[string]any{{"event": "call_service", "listener_count": 1}}
		}, args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}, absent: []string{reasonYAMLNoID}},
	}
	runTierCases(t, cases)
	f := newFakeHA()
	f.automation("night", "", nil)
	f.fail["automation/config"] = true
	h := newWriteHarness(t, f)
	if exit, env := h.run(runGetCommand, "automation.night", "--config"); exit != 1 || env.Error == nil || env.Error.Code == "CONFIG_NOT_FOUND" {
		t.Fatalf("get --config: exit %d %+v", exit, env.Error)
	}
	if exit, env := h.run(runPlanCommand, "delete", "automation.night"); exit != 1 || env.Error == nil || env.Error.Code == "CONFIG_NOT_FOUND" {
		t.Fatalf("plan delete: exit %d %+v", exit, env.Error)
	}
}

// C4: "still" only when every checked field kept its value.
func TestStillOnlyWhenNothingMoved(t *testing.T) {
	fastVerify(t)
	f := newFakeHA() // light.hall off, no brightness
	f.service = func(f *fakeHA, _ string, _ map[string]any) any {
		f.states["light.hall"]["state"] = "on" // brightness never arrives
		return coreReply(200, []any{})
	}
	h := newWriteHarness(t, f)
	if exit, env := h.run(runCallCommand, "light.turn_on", "light.hall", "--set", "brightness=100"); exit != 6 || !strings.HasPrefix(env.Verification[0].Note, "ended ") {
		t.Fatalf("exit %d rows %+v", exit, env.Verification)
	}
}

// R2, R3, R5: create collisions read first; an unknown create retries only
// with its own id; a panic after the send is an unknown outcome.
func TestOutcomeUnknownPaths(t *testing.T) {
	for _, domain := range []string{"automation", "script"} {
		if next := configIDInUseNext(domain); !strings.HasPrefix(next, "It may be the item an earlier apply created") {
			t.Fatalf("%s: %q", domain, next)
		}
	}
	var env verbEnvelope
	failure := writeFailure(&env, serviceResult{}, &relayOutcomeUnknownError{message: "timeout"}, "1700000000001")
	var vf *verbFailure
	if !errors.As(failure, &vf) || vf.exit != verbExitUnknown || !strings.Contains(vf.next, "--id 1700000000001 --create") {
		t.Fatalf("create outcome unknown: %+v", failure)
	}
	run := func(sent bool) (err error) {
		defer afterSend(&sent, &env, &err)
		panic("boom")
	}
	if err := run(true); !errors.As(err, &vf) || vf.code != "OUTCOME_UNKNOWN" || vf.exit != verbExitUnknown || !env.OutcomeUnknown {
		t.Fatalf("panic after send: %v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("a panic before the send was swallowed")
			}
		}()
		_ = run(false)
	}()
	step := nextStep(map[string]any{"error": map[string]any{"code": "OUTCOME_UNKNOWN", "next": "Verify the target state; never retry automatically."}})
	if !strings.Contains(step, "ha_get") || !strings.Contains(step, "never call or apply again") {
		t.Fatalf("MCP next_step %q", step)
	}
}

// S1: get --config of a stored automation whose entity did not load reads
// its config through the registry row, never a synthetic "does not exist".
func TestGetConfigOfAnUnloadedEntity(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
	delete(f.states, "automation.hall_motion")
	h := newWriteHarness(t, f)
	if exit, env := h.run(runGetCommand, "automation.hall_motion", "--config"); exit != 0 || !strings.Contains(string(env.Result), `"config_id":"a1"`) {
		t.Fatalf("exit %d %s %+v", exit, env.Result, env.Error)
	}
}

// S6: a write whose registry read fails during verification says so.
func TestConfigVerifyRegistryReadFailed(t *testing.T) {
	f := newFakeHA()
	var written atomic.Bool
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		if p["method"] == "POST" && strings.HasPrefix(fmtPath(p), "/api/config/") {
			written.Store(true)
		} else if written.Load() && p["type"] == "config/entity_registry/list" {
			return fakeRelayFailure{"UPSTREAM_ERROR", "boom"}
		}
		return f.reply(r, endpoint, p)
	}, true)
	h := &writeHarness{t: t, f: f, paths: paths, log: log}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("light.turn_on")))
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 6 || !strings.Contains(applied.Verification[0].Observed, "present: registry read failed") {
		t.Fatalf("exit %d rows %+v", exit, applied.Verification)
	}
}

// S7: an alias without key characters says what to do.
func TestScriptAliasWithoutKeyCharacters(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	draft := map[string]any{"alias": "!!!", "sequence": []any{step("light.turn_on", "light.hall")}}
	if exit, env := h.run(runPlanCommand, "write", "--domain", "script", "--file", draftFile(t, draft)); exit == 0 || env.Error == nil || !strings.Contains(env.Error.Message, "no a-z/0-9 characters") {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
}

func fmtPath(p map[string]any) string { s, _ := p["path"].(string); return s }
