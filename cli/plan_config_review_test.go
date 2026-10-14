package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regressions from the PR5 review round (two adversarial reviews + codex).

func rawDraft(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "draft.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// lastConfigWrite returns the raw request of the newest config POST.
func (h *writeHarness) lastConfigWrite() string {
	raw := ""
	for _, c := range h.log.all() {
		if strings.HasPrefix(fmt.Sprint(c.payload["path"]), "/api/config/") && c.payload["method"] == "POST" {
			raw = string(c.raw)
		}
	}
	return raw
}

const orderedDraft = `{"alias":"V","variables":{"target":21,"offset":"{{ target - 1 }}"},"triggers":[],"actions":[{"action":"light.turn_on","target":{"entity_id":"light.hall"}}]}`

// 1: the POSTed body keeps the draft's key order; a reorder is never verified
// and is drift.
func TestConfigWriteKeepsSemanticOrder(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", rawDraft(t, orderedDraft))
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 || applied.Status != "verified" {
		t.Fatalf("apply: exit %d env %+v", exit, applied)
	}
	if sent := h.lastConfigWrite(); strings.Index(sent, `"offset"`) < strings.Index(sent, `"target":21`) {
		t.Fatalf("variables reordered in the POST: %s", sent)
	}

	h.f.configWrite = func(f *fakeHA, path string, body map[string]any) any {
		f.storeConfig(path, body) // stored re-encoded: offset now before target
		return coreReply(200, map[string]any{"result": "ok"})
	}
	_, env = h.run(runPlanCommand, "write", "--domain", "automation", "--file", rawDraft(t, strings.Replace(orderedDraft, `"V"`, `"W"`, 1)))
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 6 || applied.Status != "unverified" || !strings.Contains(applied.Verification[0].Note, "(order)") {
		t.Fatalf("a reordering server verified: exit %d env %+v", exit, applied)
	}

	h.f.configWrite = nil
	h.f.storeConfig("/api/config/automation/config/o1", nil)
	h.f.configs["/api/config/automation/config/o1"] = json.RawMessage(orderedDraft)
	_, env = h.run(runPlanCommand, "write", "--domain", "automation", "--id", "o1", "--file", rawDraft(t, strings.Replace(orderedDraft, "21", "22", 1)))
	h.f.configs["/api/config/automation/config/o1"] = json.RawMessage(`{"alias":"V","variables":{"offset":"{{ target - 1 }}","target":21},"triggers":[],"actions":[{"action":"light.turn_on","target":{"entity_id":"light.hall"}}]}`)
	if exit, _ := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 2 {
		t.Fatalf("a live reorder did not drift: exit %d", exit)
	}
}

// 2: a 404 config whose id a YAML/package item owns is never a create.
func TestConfigWriteRefusesYAMLOwnedIDs(t *testing.T) {
	f := newFakeHA()
	f.ent("automation.pkg", "automation", "on", map[string]any{"id": "pkg1"})
	f.full = append(f.full, map[string]any{"entity_id": "automation.pkg", "platform": "automation", "unique_id": "pkg1"})
	h := newWriteHarness(t, f)
	for _, args := range [][]string{{"write", "--domain", "automation", "--id", "pkg1", "--file", draftFile(t, hallDraft("light.turn_on"))}, {"delete", "automation.pkg"}} {
		if exit, env := h.run(runPlanCommand, args...); exit != 4 || env.Error.Code != "CONFIG_NOT_UI_MANAGED" {
			t.Fatalf("%v: exit %d env %+v", args[0], exit, env)
		}
	}
}

// 3: script keys come from --id or the alias, never a millisecond id; a
// script draft carries no id.
func TestScriptKeys(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	draft := map[string]any{"alias": "Good Night", "sequence": []any{step("light.turn_off", "light.hall")}}
	if _, env := h.run(runPlanCommand, "write", "--domain", "script", "--file", draftFile(t, draft)); env.Config == nil || env.Config.Path != "/api/config/script/config/good_night" {
		t.Fatalf("derived key: %+v", env)
	}
	draft["id"] = "x"
	if exit, _ := h.run(runPlanCommand, "write", "--domain", "script", "--file", draftFile(t, draft)); exit != 1 {
		t.Fatalf("a script draft with an id: exit %d", exit)
	}
	delete(draft, "id")
	if exit, _ := h.run(runPlanCommand, "write", "--domain", "script", "--id", "Bad-Id", "--file", draftFile(t, draft)); exit != 1 {
		t.Fatalf("an invalid script key: exit %d", exit)
	}
}

// 4: checkpoints are per server.
func TestUndoCheckpointsArePerServer(t *testing.T) {
	paths := runtimePaths{ConfigDir: t.TempDir()}
	for _, server := range []string{"home", "cabin"} {
		record, _ := json.Marshal(map[string]any{"op": "update", "domain": "automation", "target_id": "a1", "server": server,
			"before_config": map[string]any{"alias": server}, "expected_after": map[string]any{"alias": "after"}})
		if receipt, err := saveUndoSnapshotBytes(paths, record); err != nil || receipt.Action != "created" {
			t.Fatalf("%s: %+v %v (one server overwrote the other)", server, receipt, err)
		}
	}
	snap, err := selectServerUndoSnapshot(paths, "home", "a1", "automation")
	if err != nil || !strings.Contains(string(snap.BeforeConfig), "home") {
		t.Fatalf("home picked %s, %v", snap.BeforeConfig, err)
	}
}

// 5 + 7: a saved-but-unverified update and an unknown outcome stay
// revertible; a revert is an update (re-checkpointed); a failed checkpoint
// save is a surviving notice.
func TestUpdateCheckpointsAndNotices(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
	h := newWriteHarness(t, f)
	plan := func(action string) string {
		_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, hallDraft(action)))
		return env.Plan.ID
	}
	id := plan("light.turn_off")
	h.f.service = func(*fakeHA, string, map[string]any) any { return coreReply(500, map[string]any{"message": "boom"}) }
	if exit, applied := h.run(runApplyCommand, "--plan", id); exit != 6 || applied.Undo == nil || len(applied.BeforeConfig) == 0 {
		t.Fatalf("unverified update: exit %d env %+v", exit, applied)
	}
	h.f.service = applies
	_, reverted := h.run(runPlanCommand, "revert")
	if exit, env := h.run(runApplyCommand, "--plan", reverted.Plan.ID); exit != 0 || env.Undo == nil {
		t.Fatalf("revert: exit %d env %+v", exit, env)
	}
	if exit, again := h.run(runPlanCommand, "revert"); exit != 3 || again.Plan == nil {
		t.Fatalf("a second revert (of the revert) reported drift: exit %d env %+v", exit, again)
	}

	previous := relayWriteTimeout
	relayWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayWriteTimeout = previous })
	id = plan("light.toggle")
	h.f.configWrite = func(*fakeHA, string, map[string]any) any {
		time.Sleep(400 * time.Millisecond)
		return coreReply(200, nil)
	}
	if exit, applied := h.run(runApplyCommand, "--plan", id); exit != 7 || applied.Undo == nil || len(applied.BeforeConfig) == 0 {
		t.Fatalf("unknown outcome: exit %d env %+v", exit, applied)
	}
	relayWriteTimeout = previous
	h.f.mu.Lock() // waits for the abandoned slow write to finish
	h.f.configWrite = nil
	h.f.mu.Unlock()

	id = plan("light.turn_on")
	_ = os.Remove(undoSnapshotStackPath(h.paths))
	_ = os.MkdirAll(undoSnapshotStackPath(h.paths), 0o700) // the stack cannot be written
	exit, applied := h.run(runApplyCommand, "--plan", id)
	notices, _ := json.Marshal(applied.Notices)
	if exit != 0 || !strings.Contains(string(notices), `"undo"`) {
		t.Fatalf("lost undo notice: exit %d notices %s", exit, notices)
	}
}

// 6: --no-snapshot only after a snapshot failure handed the plan back.
func TestNoSnapshotOnlyAfterAFailure(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/n1", map[string]any{"alias": "Night", "actions": []any{}})
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "delete", "automation.night")
	if exit, refused := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code, "--no-snapshot"); exit != 4 || refused.Error.Code != "NO_SNAPSHOT_NOT_OFFERED" || len(h.writes()) != 0 {
		t.Fatalf("first try: exit %d env %+v", exit, refused)
	}
	h.f.snapshotErr = true
	if exit, _ := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 1 {
		t.Fatalf("snapshot failure: exit %d", exit)
	}
	if exit, deleted := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code, "--no-snapshot"); exit != 0 || deleted.Status != "verified" || deleted.Snapshot != "" {
		t.Fatalf("after the failure: exit %d env %+v", exit, deleted)
	}
}

// 8-11: scoped reload, unavailable entities, raw snapshot payload, a delete
// that finds the item already gone.
func TestConfigApplyDetails(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
	f.configs["/api/config/automation/config/a1"] = json.RawMessage(`{"alias":"Hall motion","triggers":[],"actions":[],"x":1.10}`)
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, hallDraft("light.turn_off")))
	h.run(runApplyCommand, "--plan", env.Plan.ID)
	for _, c := range h.posts() {
		if c.payload["path"] == "/api/services/automation/reload" && strings.Contains(string(c.raw), `"body":{"id":"a1"}`) {
			goto scoped
		}
	}
	t.Fatalf("automation reload not scoped to a1: %+v", h.posts())
scoped:
	h.f.configWrite = func(f *fakeHA, path string, body map[string]any) any {
		f.storeConfig(path, body)
		f.states["automation.hall_motion"]["state"] = "unavailable"
		return coreReply(200, nil)
	}
	_, env = h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, hallDraft("light.toggle")))
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 6 || !strings.Contains(applied.Verification[0].Note, "unavailable") {
		t.Fatalf("unavailable entity verified: exit %d env %+v", exit, applied)
	}

	h.f.configs["/api/config/automation/config/a1"] = json.RawMessage(`{"alias":"Hall motion","triggers":[],"actions":[],"x":1.10}`)
	h.f.states["automation.hall_motion"]["state"] = "on"
	h.f.deleteStatus = 400
	_, env = h.run(runPlanCommand, "delete", "automation.hall_motion")
	exit, deleted := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if exit != 0 || deleted.Status != "verified" || !strings.Contains(deleted.Verification[0].Note, "already gone") {
		t.Fatalf("already-gone delete: exit %d env %+v", exit, deleted)
	}
	for _, c := range h.log.all() {
		if c.endpoint == "backups" && !strings.Contains(string(c.raw), `"x":1.10`) {
			t.Fatalf("snapshot payload lost the exact number: %s", c.raw)
		}
	}
}
