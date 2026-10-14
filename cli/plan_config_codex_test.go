package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Codex review of #664.

// 1: one order-aware comparator everywhere — a reorder after the
// checkpoint is revert drift, snapshot-verify drift, and an ha-nova diff row.
func TestRevertGuardIsOrderAware(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/o1", nil)
	f.configs["/api/config/automation/config/o1"] = json.RawMessage(orderedDraft)
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "o1", "--file", rawDraft(t, strings.Replace(orderedDraft, "21", "22", 1)))
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 || applied.Undo == nil {
		t.Fatalf("update: exit %d env %+v", exit, applied)
	}
	reordered := `{"alias":"V","variables":{"offset":"{{ target - 1 }}","target":22},"triggers":[],"actions":[{"action":"light.turn_on","target":{"entity_id":"light.hall"}}]}`
	h.f.configs["/api/config/automation/config/o1"] = json.RawMessage(reordered)
	if exit, reverted := h.run(runPlanCommand, "revert", "--target", "o1"); exit != 2 || reverted.Error.Code != "REVERT_DRIFT" {
		t.Fatalf("revert after a reorder: exit %d env %+v", exit, reverted)
	}
	live := rawDraft(t, reordered)
	exit := 0
	out := captureStdout(t, func() { exit = runSnapshotCommand(h.paths, []string{"verify", "--against", live, "--target", "o1"}) })
	if exit != snapshotExitDrift || strings.TrimSpace(out) != "drift" {
		t.Fatalf("snapshot verify: exit %d %q", exit, out)
	}
	out = captureStdout(t, func() {
		exit = runDiffCommand(h.paths, []string{"--before", rawDraft(t, strings.Replace(orderedDraft, "21", "22", 1)), "--after", live})
	})
	if exit != 0 || !strings.Contains(out, "(order)") {
		t.Fatalf("ha-nova diff: exit %d %q", exit, out)
	}
}

// 2: registry-disabled items have no state: ids resolve through the
// registry and a write verifies without a state.
func TestDisabledItemsResolveThroughTheRegistry(t *testing.T) {
	disabled := func() *fakeHA {
		f := newFakeHA()
		f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
		delete(f.states, "automation.hall_motion")
		f.full[len(f.full)-1]["disabled_by"] = "user"
		return f
	}
	h := newWriteHarness(t, disabled())
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, hallDraft("light.turn_off")))
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || applied.Status != "verified" || !strings.Contains(applied.Verification[0].Note, "disabled") {
		t.Fatalf("write to a disabled item: exit %d env %+v", exit, applied)
	}

	h = newWriteHarness(t, disabled())
	exit, env = h.run(runPlanCommand, "delete", "automation.hall_motion")
	if exit != 3 || env.Config == nil || env.Config.ConfigID != "a1" {
		t.Fatalf("plan delete of a disabled item: exit %d env %+v", exit, env)
	}
	if exit, deleted := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 0 || deleted.Status != "verified" {
		t.Fatalf("delete of a disabled item: exit %d env %+v", exit, deleted)
	}
}

// 3: snapshot show/verify select this server's checkpoint (untagged legacy
// records stay eligible) and --list names the server.
func TestSnapshotCommandsAreServerAware(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	server := activeServerProfile()
	for _, rec := range []struct{ server, alias string }{{"", "legacy"}, {server, "mine"}, {"cabin", "theirs"}} {
		record, _ := json.Marshal(map[string]any{"op": "update", "domain": "automation", "target_id": "a1", "server": rec.server,
			"before_config": map[string]any{"alias": rec.alias}, "expected_after": map[string]any{"alias": "after"}})
		if _, err := saveUndoSnapshotBytes(h.paths, record); err != nil {
			t.Fatal(err)
		}
	}
	exit := 0
	out := captureStdout(t, func() {
		exit = runSnapshotCommand(h.paths, []string{"show", "--target", "a1", "--domain", "automation"})
	})
	if exit != 0 || !strings.Contains(out, `"mine"`) {
		t.Fatalf("show picked %q (exit %d), want this server's record", out, exit)
	}
	out = captureStdout(t, func() { exit = runSnapshotCommand(h.paths, []string{"show", "--list"}) })
	if exit != 0 || !strings.Contains(out, `"server": "cabin"`) {
		t.Fatalf("--list hides the server: %s", out)
	}
	live := filepath.Join(t.TempDir(), "live.json")
	_ = writeJSONFile(live, map[string]any{"alias": "after"}, 0o600)
	out = captureStdout(t, func() {
		exit = runSnapshotCommand(h.paths, []string{"verify", "--against", live, "--target", "a1", "--domain", "automation"})
	})
	if exit != 0 || strings.TrimSpace(out) != "match" {
		t.Fatalf("verify: exit %d %q", exit, out)
	}
}
