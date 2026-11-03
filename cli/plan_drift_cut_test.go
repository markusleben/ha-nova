package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Cut-plan drift (docs/work/2026-11-03-cut-plan-drift-spec.md): an update
// of an automation whose helper many automations react to. A read budget
// that cut the impact check at another point each run made every apply
// drift, and a blueprint that ran out of budget read as unresolved.

// presenceHome: input_boolean.presence starts 11 automations (p00–p10, p00
// through a blueprint); each turns on a room light whose own automation
// (l00–l10) turns on a lamp. The draft "leave" switches presence off.
func presenceHome() *fakeHA {
	f := newFakeHA()
	f.ent("input_boolean.presence", "input_boolean", "on", map[string]any{"friendly_name": "Presence"})
	var consumers []string
	blueprints := map[string]any{}
	for i := range 11 {
		room, lamp := fmt.Sprintf("light.room_%02d", i), fmt.Sprintf("light.lamp_%02d", i)
		f.ent(room, "hue", "off", map[string]any{"_di": fmt.Sprintf("dev_%02d", i)})
		f.ent(lamp, "hue", "off", nil)
		p := map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.presence", "to": "off"}}, "actions": []any{step("light.turn_on", room)}}
		if i == 0 {
			blueprints["presence/away.yaml"] = p
			p = map[string]any{"use_blueprint": map[string]any{"path": "presence/away.yaml", "input": map[string]any{"light": room}}}
		}
		f.automation(fmt.Sprintf("presence_%02d", i), fmt.Sprintf("p%02d", i), p)
		consumers = append(consumers, fmt.Sprintf("automation.presence_%02d", i))
		f.automation(fmt.Sprintf("lamp_%02d", i), fmt.Sprintf("l%02d", i), map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": room}}, "actions": []any{step("light.turn_on", lamp)}})
		f.related["entity:"+room] = []string{fmt.Sprintf("automation.lamp_%02d", i)}
	}
	f.related["entity:input_boolean.presence"] = consumers
	f.extra = func(_ string, p map[string]any) (any, bool) {
		if p["type"] != "blueprint/substitute" || f.fail["blueprint/substitute"] {
			return nil, false
		}
		return map[string]any{"substituted_config": blueprints[fmt.Sprint(p["path"])]}, true
	}
	f.storeConfig("/api/config/automation/config/leave", leaveDraft())
	return f
}

func leaveDraft() map[string]any {
	return map[string]any{"alias": "Leaving", "triggers": []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.door"}},
		"actions": []any{step("input_boolean.turn_off", "input_boolean.presence")}}
}

// slowHarness answers every read after latency; hangFirst makes the next n
// reads of a config hang until their request ends (the read budget).
type slowHarness struct {
	*writeHarness
	mu   sync.Mutex
	hung map[string]int
}

func newSlowHarness(t *testing.T, f *fakeHA, latency, budget time.Duration) *slowHarness {
	fastVerify(t)
	f.service = applies
	h := &slowHarness{hung: map[string]int{}}
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		h.mu.Lock()
		path := fmt.Sprint(p["path"])
		n := h.hung[path]
		if n > 0 {
			h.hung[path] = n - 1
		}
		h.mu.Unlock()
		if n > 0 {
			<-r.Context().Done()
			return nil
		}
		time.Sleep(latency)
		return f.reply(r, endpoint, p)
	}, true)
	h.writeHarness = &writeHarness{t: t, f: f, paths: paths, log: log}
	saved := relaySessionOverallTimeout
	relaySessionOverallTimeout = budget
	t.Cleanup(func() { relaySessionOverallTimeout = saved })
	return h
}

func (h *slowHarness) hangFirst(config string, n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hung["/api/config/automation/config/"+config] = n
}

func (h *slowHarness) configReads(config string) int {
	n := 0
	for _, c := range h.log.all() {
		if c.payload["path"] == "/api/config/automation/config/"+config {
			n++
		}
	}
	return n
}

func (h *slowHarness) planLeave() verbTestEnvelope {
	h.t.Helper()
	return h.plan("write", "--domain", "automation", "--id", "leave", "--file", draftFile(h.t, leaveDraft()))
}

func (h *slowHarness) plan(args ...string) verbTestEnvelope {
	h.t.Helper()
	_, env := h.run(runPlanCommand, args...)
	if env.Plan == nil {
		h.t.Fatalf("no plan: %+v", env)
	}
	return env
}

// The walk reads breadth first: over a link with 20 ms per read it finishes
// well inside a budget the depth-first walk (about 90 reads in a row) ran
// out of, so each run is the same full check, not a cut at a new point.
func TestRepeatedPlansOverASlowLinkAreTheSamePlan(t *testing.T) {
	h := newSlowHarness(t, presenceHome(), 20*time.Millisecond, 1200*time.Millisecond)
	var hashes []string
	for range 3 {
		env := h.planLeave()
		if env.Tier != "preview" || env.Coverage == nil || !env.Coverage.Complete || len(env.Consumers) != 22 {
			t.Fatalf("tier %s coverage %+v consumers %d reasons %s", env.Tier, env.Coverage, len(env.Consumers), reasonCodes(env))
		}
		hashes = append(hashes, env.Plan.Hash)
	}
	if hashes[0] != hashes[1] || hashes[1] != hashes[2] {
		t.Fatalf("hashes %v", hashes)
	}
}

// A blueprint substitution the read budget cut is a cut check, never an
// unresolved blueprint of that automation (a new reason at every run);
// one Home Assistant can't substitute still is.
func TestABlueprintCutByTheBudgetIsACut(t *testing.T) {
	f := presenceHome()
	f.hang["blueprint/substitute"] = true
	h := newSlowHarness(t, f, 0, 300*time.Millisecond)
	if codes := reasonCodes(h.planLeave()); strings.Contains(codes, reasonBlueprint) || !strings.Contains(codes, reasonLimitCut) {
		t.Fatalf("reasons %s", codes)
	}
	f.hang = map[string]bool{}
	f.fail["blueprint/substitute"] = true
	if codes := reasonCodes(h.planLeave()); !strings.Contains(codes, reasonBlueprint) {
		t.Fatalf("reasons %s", codes)
	}
}
