package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Lever 1 (HANO-58): a plan confirmed through the gate asks first, then
// checks the whole plan again; nothing is sent unless both passed on the
// same bytes.

// The ask opens before any Home Assistant read of this apply, and the
// recheck reads Home Assistant after the answer and before the one send.
func TestGateAsksBeforeItChecksThePlanAgain(t *testing.T) {
	f := newFakeHA()
	var reads atomic.Int32
	f.extra = func(string, map[string]any) (any, bool) {
		reads.Add(1)
		return nil, false
	}
	var mu sync.Mutex
	var atAsk, atSend int32 = -1, -1
	g := &fakeGate{onWait: func() {
		mu.Lock()
		defer mu.Unlock()
		atAsk = reads.Load()
	}}
	g.hold = func(gateSend) any {
		mu.Lock()
		defer mu.Unlock()
		atSend = reads.Load()
		return nil
	}
	h := gatedHarness(t, f, g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	start := reads.Load()
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	asks, _, sends := g.snapshot()
	if exit != 0 || len(asks) != 1 || len(sends) != 1 || sends[0].grant == "" {
		t.Fatalf("exit %d %+v asks %q sends %+v", exit, env.Error, asks, sends)
	}
	if atAsk != start {
		t.Fatalf("the ask waited for %d reads of Home Assistant; it must come first", atAsk-start)
	}
	if atSend <= atAsk {
		t.Fatalf("the send came without a recheck after the answer: reads at answer %d, at send %d", atAsk, atSend)
	}
}

// A plan file edited after its preview (what it sends no longer makes its
// hash) never reaches the phone: the ask shows only what the person saw.
func TestGateAskFirstRefusesAnEditedPlanWithoutAsking(t *testing.T) {
	edit := func(t *testing.T, paths runtimePaths, id string, change func(*storedPlan)) {
		t.Helper()
		file := filepath.Join(planDir(paths), id+".json")
		stored, err := readPlanFile(file)
		if err != nil {
			t.Fatalf("read plan: %v", err)
		}
		change(&stored)
		if err := writePlanFile(file, stored); err != nil {
			t.Fatalf("write plan: %v", err)
		}
	}
	for name, plan := range map[string]func(h *writeHarness) (string, func(*storedPlan)){
		"call": func(h *writeHarness) (string, func(*storedPlan)) {
			return h.plan("lock.unlock", "lock.front").Plan.ID, func(p *storedPlan) { p.Action.EntityIDs = []string{"lock.back"} }
		},
		"batch member": func(h *writeHarness) (string, func(*storedPlan)) {
			unlock := func(alias string) map[string]any {
				return map[string]any{"alias": alias, "sequence": []any{step("lock.unlock", "lock.front")}}
			}
			_, env := h.batch(h.writePlan("script", "s1", unlock("Arrival")), h.writePlan("script", "s2", unlock("Old arrival")))
			return env.Plan.ID, func(p *storedPlan) {
				p.BatchMembers[0].ConfigAction.Body = json.RawMessage(`{"alias":"Arrival","sequence":[{"action":"lock.unlock","target":{"entity_id":"lock.back"}}]}`)
			}
		},
		"config write": func(h *writeHarness) (string, func(*storedPlan)) {
			id := h.writePlan("script", "s1", map[string]any{"alias": "Arrival", "sequence": []any{step("lock.unlock", "lock.front")}})
			return id, func(p *storedPlan) {
				p.ConfigAction.Body = json.RawMessage(`{"alias":"Arrival","sequence":[{"action":"lock.unlock","target":{"entity_id":"lock.back"}}]}`)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGate{}
			h := gatedHarness(t, batchHome(), g)
			id, change := plan(h)
			edit(t, h.paths, id, change)
			exit, env := h.run(runApplyCommand, "--plan", id)
			if asks, _, sends := g.snapshot(); exit != verbExitRefused || env.Error == nil || env.Error.Code != "PLAN_INVALID" || len(asks)+len(sends) != 0 {
				t.Fatalf("exit %d %+v asks %q sends %+v", exit, env.Error, asks, sends)
			}
		})
	}
	t.Run("raw", func(t *testing.T) {
		g := &fakeGate{}
		paths, sent := rawRelay(t, map[string]func(map[string]any) any{
			"health <nil>":          func(map[string]any) any { return map[string]any{"status": "ok", "gate": 1} },
			gateAskRoute + " <nil>": func(p map[string]any) any { return g.ask(context.Background(), p) },
			"input_boolean/list": func(map[string]any) any {
				return []any{map[string]any{"id": "guest", "name": "Guest"}, map[string]any{"id": "host", "name": "Host"}}
			},
			"config/entity_registry/list": func(map[string]any) any {
				return []any{map[string]any{"entity_id": "input_boolean.guest", "platform": "input_boolean", "unique_id": "guest"}}
			},
		})
		_, typed := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/delete","input_boolean_id":"guest"}}]`)
		if typed.Plan == nil || typed.Plan.Channel != channelRelay {
			t.Fatalf("typed raw plan: %+v", typed)
		}
		edit(t, paths, typed.Plan.ID, func(p *storedPlan) {
			p.RawRequests[0].Payload = json.RawMessage(`{"type":"input_boolean/delete","input_boolean_id":"host"}`)
		})
		exit, applied := applyRaw(t, paths, typed)
		if asks, _, _ := g.snapshot(); exit != verbExitRefused || applied.Error == nil || applied.Error.Code != "PLAN_INVALID" || len(asks) != 0 || strings.Contains(strings.Join(sent(), "|"), "input_boolean/delete") {
			t.Fatalf("exit %d %+v asks %q sent %v", exit, applied.Error, asks, sent())
		}
	})
}

// A batch member that changed while the phone asked sends nothing after
// the approval: the recheck names it, and the batch is used up.
func TestGateAskFirstBatchDriftAfterTheApprovalSendsNothing(t *testing.T) {
	f := batchHome()
	g := &fakeGate{onWait: func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.configs["/api/config/script/config/s1"] = arrival("light.toggle")
	}}
	h := gatedHarness(t, f, g)
	unlock := func(alias string) map[string]any {
		return map[string]any{"alias": alias, "sequence": []any{step("lock.unlock", "lock.front")}}
	}
	_, env := h.batch(h.writePlan("script", "s1", unlock("Arrival")), h.writePlan("script", "s2", unlock("Old arrival")))
	if env.Plan == nil || env.Plan.Channel != channelRelay {
		t.Fatalf("batch plan: %+v", env)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if asks, _, sends := g.snapshot(); exit != verbExitDrift || applied.Error == nil || applied.Error.Code != "BATCH_DRIFT" || len(asks) != 1 || len(sends) != 0 {
		t.Fatalf("exit %d %+v asks %d sends %+v", exit, applied.Error, len(asks), sends)
	}
}

// A recheck that can't read Home Assistant after the approval sends
// nothing.
func TestGateAskFirstRecheckErrorAfterTheApprovalSendsNothing(t *testing.T) {
	f := newFakeHA()
	g := &fakeGate{onWait: func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail == nil {
			f.fail = map[string]bool{}
		}
		f.fail["/api/states"] = true
		f.fail["get_states"] = true
		f.fail["config/entity_registry/list_for_display"] = true
	}}
	h := gatedHarness(t, f, g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if asks, _, sends := g.snapshot(); exit == 0 || env.Error == nil || len(asks) != 1 || len(sends) != 0 {
		t.Fatalf("exit %d %+v asks %d sends %+v", exit, env.Error, len(asks), sends)
	}
}

// A plan that changed after the person's OK says first that the OK was
// not used and nothing was sent, then shows the new preview.
func TestGateAskFirstDriftAfterTheOKSaysItWasNotUsed(t *testing.T) {
	f := newFakeHA()
	g := &fakeGate{onWait: func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.states["lock.front"]["state"] = "jammed" // the basis changed
	}}
	h := gatedHarness(t, f, g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	asks, _, sends := g.snapshot()
	if exit != verbExitDrift || env.Error == nil || env.Error.Code != "PLAN_DRIFT" || len(asks) != 1 || len(sends) != 0 ||
		!strings.HasPrefix(env.Say, confirmT("en", "say.drift_after_ok")) || !strings.HasPrefix(env.Error.Next, "The user already confirmed the old preview") {
		t.Fatalf("exit %d %+v say %q asks %d sends %+v", exit, env.Error, env.Say, len(asks), sends)
	}
}
