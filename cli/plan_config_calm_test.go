package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Calmer confirmations item 6: an update of an existing UI automation or
// script runs without a preview when nothing else asks, and its checkpoint
// is saved before the write.

const hallPath = "/api/config/automation/config/a1"

func calmHome() *fakeHA {
	f := newFakeHA()
	f.storeConfig(hallPath, hallDraft("light.turn_on"))
	return f
}

func (h *writeHarness) planHall(action string) verbTestEnvelope {
	h.t.Helper()
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(h.t, hallDraft(action)))
	return env
}

func (h *writeHarness) checkpoints() []undoSnapshot {
	h.t.Helper()
	stack, err := loadUndoSnapshotStack(h.paths)
	if err != nil {
		h.t.Fatal(err)
	}
	return stack.Snapshots
}

func saveOtherCheckpoints(t *testing.T, paths runtimePaths, n int, target string) {
	t.Helper()
	for i := range n {
		record, _ := json.Marshal(map[string]any{"op": "update", "domain": "automation", "target_id": fmt.Sprintf("%s%d", target, i), "server": "default",
			"before_config": map[string]any{"alias": "x"}, "expected_after": map[string]any{"alias": "y"}})
		if _, err := saveUndoSnapshotBytes(paths, record); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlanWriteUpdateRunsWithItsWayBackSavedFirst(t *testing.T) {
	h := newWriteHarness(t, calmHome())
	env := h.planHall("light.turn_off")
	if env.Tier != "ordinary" || env.Plan == nil || env.Plan.Confirm != "none" || env.Status != "ready" || len(env.Diff) == 0 ||
		!strings.HasPrefix(env.NextStep, "No ask: the CLI checked this change and saves its way back first. Run ha-nova apply --plan "+env.Plan.ID+" now") {
		t.Fatalf("plan: %+v", env)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || applied.Status != "verified" || applied.Undo == nil || applied.Undo.Action != "created" || applied.Undo.TargetID != "a1" ||
		!strings.HasSuffix(applied.Say, "You can put it back with “undo”; what already ran isn't undone.") || len(applied.Diff) == 0 {
		t.Fatalf("apply: exit %d env %+v", exit, applied)
	}
	if got := h.checkpoints(); len(got) != 1 || !strings.Contains(string(got[0].BeforeConfig), "light.turn_on") || !strings.Contains(string(got[0].ExpectedAfter), "light.turn_off") {
		t.Fatalf("checkpoints %+v", got)
	}
	if _, reverted := h.run(runPlanCommand, "revert"); reverted.Tier != "preview" { // an undo keeps its preview
		t.Fatalf("revert: %+v", reverted)
	}
	if again := h.planHall("light.toggle"); again.Tier != "preview" { // never replaces this item's checkpoint
		t.Fatalf("second update: %+v", again)
	}
}

func TestPlanWriteUpdateKeepsThePreviewOnAnyDoubt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(h *writeHarness)
	}{
		{"another automation calls it", func(h *writeHarness) {
			h.f.related["entity:automation.hall_motion"] = []string{"automation.bedtime"}
		}},
		{"the caller read fails", func(h *writeHarness) { h.f.fail["search/related"] = true }},
		{"the full registry can't be read", func(h *writeHarness) { h.f.fail["config/entity_registry/list"] = true }},
		{"two entities claim the config id", func(h *writeHarness) {
			h.f.full = append(h.f.full, map[string]any{"entity_id": "automation.hall_motion_2", "platform": "automation", "unique_id": "a1"})
		}},
		{"its checkpoint would push another off the list", func(h *writeHarness) {
			saveOtherCheckpoints(h.t, h.paths, undoSnapshotStackLimit, "other")
		}},
		{"an untagged checkpoint of it exists", func(h *writeHarness) {
			record, _ := json.Marshal(map[string]any{"op": "update", "domain": "automation", "target_id": "a1",
				"before_config": map[string]any{"alias": "x"}, "expected_after": map[string]any{"alias": "y"}})
			if _, err := saveUndoSnapshotBytes(h.paths, record); err != nil {
				h.t.Fatal(err)
			}
		}},
		{"its config holds a hidden value", func(h *writeHarness) {
			stored := hallDraft("light.turn_on")
			stored["variables"] = map[string]any{"pin": secretHidden}
			h.f.configs[hallPath] = stored
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHarness(t, calmHome())
			tc.setup(h)
			if env := h.planHall("light.turn_off"); env.Tier == "ordinary" || env.Plan != nil && env.Plan.Confirm == "none" {
				t.Fatalf("%+v", env)
			}
		})
	}
}

func TestPlanWriteUpdateReachingAnUnlockStaysTyped(t *testing.T) {
	h := newWriteHarness(t, calmHome())
	draft := hallDraft("light.turn_on")
	draft["actions"] = []any{step("lock.unlock", "lock.front")}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, draft))
	if env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonAccessLock) {
		t.Fatalf("%+v", env)
	}
}

// A checkpoint that appears between plan and apply changes the tier: drift,
// nothing sent.
func TestApplyOrdinaryUpdateDriftsWhenItsCheckpointNoLongerFits(t *testing.T) {
	h := newWriteHarness(t, calmHome())
	env := h.planHall("light.turn_off")
	saveOtherCheckpoints(t, h.paths, undoSnapshotStackLimit, "other")
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 2 || applied.Status != "drift" || len(h.writes()) != 0 {
		t.Fatalf("exit %d env %+v writes %v", exit, applied, h.writes())
	}
}

// Home Assistant refuses the write: the checkpoint saved for it goes again.
func TestApplyOrdinaryUpdateRefusedDropsItsCheckpoint(t *testing.T) {
	h := newWriteHarness(t, calmHome())
	env := h.planHall("light.turn_off")
	h.f.configWrite = func(*fakeHA, string, map[string]any) any {
		return coreReply(400, map[string]any{"message": "Message malformed"})
	}
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit == 0 || applied.Undo != nil || len(h.checkpoints()) != 0 {
		t.Fatalf("exit %d env %+v checkpoints %+v", exit, applied, h.checkpoints())
	}
}

func TestAddUndoSnapshotNeverReplacesOrEvicts(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	record := func(target string) []byte {
		raw, _ := json.Marshal(map[string]any{"op": "update", "domain": "script", "target_id": target, "server": "default",
			"before_config": map[string]any{"alias": "x"}, "expected_after": map[string]any{"alias": "y"}})
		return raw
	}
	if receipt, err := addUndoSnapshot(h.paths, record("s1")); err != nil || receipt.Action != "created" {
		t.Fatalf("first: %+v %v", receipt, err)
	}
	if _, err := addUndoSnapshot(h.paths, record("s1")); err == nil {
		t.Fatal("replaced its own checkpoint")
	}
	saveOtherCheckpoints(t, h.paths, undoSnapshotStackLimit-1, "other")
	if _, err := addUndoSnapshot(h.paths, record("s2")); err == nil || len(h.checkpoints()) != undoSnapshotStackLimit {
		t.Fatalf("evicted: %v, %d checkpoints", err, len(h.checkpoints()))
	}
}
