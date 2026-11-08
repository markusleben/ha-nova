package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// One undo step per item: a calm update replaces its item's own last calm
// step, so undo restores the state before the latest calm update. Anything
// else keeps the ask.

func (h *writeHarness) applyHall(action string) verbTestEnvelope {
	h.t.Helper()
	env := h.planHall(action)
	if env.Tier != "ordinary" || env.Plan == nil {
		h.t.Fatalf("plan %s: %+v", action, env)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || applied.Status != "verified" {
		h.t.Fatalf("apply %s: exit %d env %+v", action, exit, applied)
	}
	return applied
}

func TestCalmUpdateReplacesItsOwnStepAndUndoGoesBackOne(t *testing.T) {
	h := newWriteHarness(t, calmHome())
	h.applyHall("light.turn_off")
	if second := h.applyHall("light.toggle"); second.Undo == nil || second.Undo.Action != "replaced" {
		t.Fatalf("second: %+v", second.Undo)
	}
	got := h.checkpoints()
	if len(got) != 1 || !got[0].Calm || !strings.Contains(string(got[0].BeforeConfig), "light.turn_off") || !strings.Contains(string(got[0].ExpectedAfter), "light.toggle") {
		t.Fatalf("checkpoints %+v", got)
	}
	_, reverted := h.run(runPlanCommand, "revert")
	if reverted.Tier != "preview" || reverted.Plan == nil {
		t.Fatalf("revert: %+v", reverted)
	}
	if exit, done := h.run(runApplyCommand, "--plan", reverted.Plan.ID); exit != 0 || done.Status != "verified" {
		t.Fatalf("revert apply: exit %d env %+v", exit, done)
	}
	if live, _ := json.Marshal(h.f.configs[hallPath]); !strings.Contains(string(live), "light.turn_off") {
		t.Fatalf("live after undo: %s", live)
	}
	if again := h.planHall("light.toggle"); again.Tier != "preview" { // a revert's step is no calm one
		t.Fatalf("after undo: %+v", again)
	}
}

func TestCalmUpdateKeepsTheAskWhenTheStepIsNotItsOwn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(h *writeHarness)
	}{
		{"edited outside HA NOVA since", func(h *writeHarness) {
			h.applyHall("light.turn_off")
			h.f.storeConfig(hallPath, hallDraft("light.turn_on"))
		}},
		{"a previewed update's step", func(h *writeHarness) {
			saveHallStep(h, false)
		}},
		{"a calm tag from snapshot save", func(h *writeHarness) {
			record, _ := json.Marshal(map[string]any{"op": "update", "domain": "automation", "target_id": "a1", "server": "default", "calm": true,
				"before_config": hallDraft("light.toggle"), "expected_after": hallDraft("light.turn_on")})
			if _, err := saveUndoSnapshotForServer(h.paths, record, "default"); err != nil {
				h.t.Fatal(err)
			}
		}},
		{"an untagged step beside its own", func(h *writeHarness) {
			saveHallStep(h, true)
			record, _ := json.Marshal(map[string]any{"op": "update", "domain": "automation", "target_id": "a1",
				"before_config": hallDraft("light.toggle"), "expected_after": hallDraft("light.turn_on")})
			if _, err := saveUndoSnapshotBytes(h.paths, record); err != nil {
				h.t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHarness(t, calmHome())
			tc.setup(h)
			if env := h.planHall("light.toggle"); env.Tier == "ordinary" {
				t.Fatalf("%+v", env)
			}
		})
	}
}

// saveHallStep saves a1's step as its last update left it (live turn_on).
func saveHallStep(h *writeHarness, calm bool) {
	h.t.Helper()
	record, _ := json.Marshal(map[string]any{"op": "update", "domain": "automation", "target_id": "a1", "server": "default", "calm": calm,
		"before_config": hallDraft("light.toggle"), "expected_after": hallDraft("light.turn_on")})
	if _, err := saveUndoSnapshotBytes(h.paths, record); err != nil {
		h.t.Fatal(err)
	}
}

// A full undo list: replacing its own step pushes no other off.
func TestCalmUpdateReplacesItsOwnStepOnAFullList(t *testing.T) {
	h := newWriteHarness(t, calmHome())
	saveOtherCheckpoints(t, h.paths, undoSnapshotStackLimit-1, "other")
	h.applyHall("light.turn_off")
	h.applyHall("light.toggle")
	if got := h.checkpoints(); len(got) != undoSnapshotStackLimit {
		t.Fatalf("%d checkpoints", len(got))
	}
}

// Home Assistant refuses the second write: the first step comes back.
func TestCalmUpdateRefusedPutsTheReplacedStepBack(t *testing.T) {
	h := newWriteHarness(t, calmHome())
	h.applyHall("light.turn_off")
	env := h.planHall("light.toggle")
	h.f.configWrite = func(*fakeHA, string, map[string]any) any {
		return coreReply(400, map[string]any{"message": "Message malformed"})
	}
	if exit, _ := h.run(runApplyCommand, "--plan", env.Plan.ID); exit == 0 {
		t.Fatal("refused write succeeded")
	}
	got := h.checkpoints()
	if len(got) != 1 || !got[0].Calm || !strings.Contains(string(got[0].BeforeConfig), "light.turn_on") || !strings.Contains(string(got[0].ExpectedAfter), "light.turn_off") {
		t.Fatalf("checkpoints %+v", got)
	}
}

// Home Assistant kept something else than the draft: the step stays the way
// back, but the next update asks.
func TestCalmUpdateWithADifferingReadBackIsNoCalmStep(t *testing.T) {
	h := newWriteHarness(t, calmHome())
	env := h.planHall("light.turn_off")
	h.f.configWrite = func(f *fakeHA, path string, body map[string]any) any {
		body["description"] = "kept by Home Assistant"
		f.storeConfig(path, body)
		return coreReply(200, map[string]any{"result": "ok"})
	}
	h.run(runApplyCommand, "--plan", env.Plan.ID)
	if got := h.checkpoints(); len(got) != 1 || got[0].Calm || !strings.Contains(string(got[0].ExpectedAfter), "kept by Home Assistant") {
		t.Fatalf("checkpoints %+v", got)
	}
	if again := h.planHall("light.toggle"); again.Tier == "ordinary" {
		t.Fatalf("%+v", again)
	}
}

// Another update replaced the checkpoint while this one verified: the late
// refresh leaves the newer one alone.
func TestRefreshUndoSnapshotLeavesANewerCheckpoint(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	snapshotNow = func() time.Time { return now }
	t.Cleanup(func() { snapshotNow = time.Now })
	record := func(after string) []byte {
		raw, _ := json.Marshal(map[string]any{"op": "update", "domain": "automation", "target_id": "a1", "server": "default", "calm": true,
			"before_config": hallDraft("light.turn_on"), "expected_after": hallDraft(after)})
		return raw
	}
	first, err := addUndoSnapshot(h.paths, record("light.turn_off"))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := saveUndoSnapshotBytes(h.paths, record("light.toggle")); err != nil {
		t.Fatal(err)
	}
	readBack, _ := json.Marshal(hallDraft("light.turn_off"))
	if err := refreshUndoSnapshot(h.paths, "default", first, readBack, true); err != nil {
		t.Fatal(err)
	}
	if got := h.checkpoints(); len(got) != 1 || !strings.Contains(string(got[0].ExpectedAfter), "light.toggle") {
		t.Fatalf("checkpoints %+v", got)
	}
}

// A previous step it can't fully read is never replaced.
func TestOwnCalmStepNeedsAReadableStep(t *testing.T) {
	live, _ := json.Marshal(hallDraft("light.turn_off"))
	step := undoSnapshot{Op: "update", Domain: "automation", TargetID: "a1", Server: "default", Calm: true, BeforeConfig: live, ExpectedAfter: live}
	if !ownCalmStep(undoSnapshotStack{Snapshots: []undoSnapshot{step}}, "default", "automation", "a1", live) {
		t.Fatal("a readable own step was refused")
	}
	step.BeforeConfig = json.RawMessage("null")
	if ownCalmStep(undoSnapshotStack{Snapshots: []undoSnapshot{step}}, "default", "automation", "a1", live) {
		t.Fatal("an unreadable step was replaced")
	}
}
