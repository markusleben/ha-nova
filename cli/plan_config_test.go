package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// configReply makes the fake Home Assistant store config writes, delete
// configs (with their entity), and record /backups snapshot saves.
func (f *fakeHA) configReply(r *http.Request, endpoint string, p map[string]any) (any, bool) {
	path := fmt.Sprint(p["path"])
	switch {
	case endpoint == "backups" && p["action"] == "load":
		if data := f.snapshotFiles[fmt.Sprint(p["file"])]; string(data) == "corrupt" {
			return fakeRelayFailure{"SNAPSHOT_CORRUPT", "could not be decompressed/parsed"}, true
		} else if data != nil {
			return map[string]any{"data": data, "created_at": "2026-10-16T12:00:00.000Z"}, true
		}
		return fakeRelayFailure{"SNAPSHOT_NOT_FOUND", "No snapshot at " + fmt.Sprint(p["file"])}, true
	case endpoint == "backups":
		if f.snapshotErr {
			return fakeRelayFailure{"SNAPSHOT_STORE_FULL", "store full"}, true
		}
		var sent struct {
			Data json.RawMessage `json:"data"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sent)
		f.snapshots = append(f.snapshots, p)
		file := fmt.Sprintf("%s/%s-20261016T120000%03dZ.json.gz", p["category"], p["name"], len(f.snapshots))
		if f.snapshotFiles == nil {
			f.snapshotFiles = map[string]json.RawMessage{}
		}
		f.snapshotFiles[file] = sent.Data
		return map[string]any{"file": file}, true
	case !strings.HasPrefix(path, "/api/config/"):
		return nil, false
	case p["method"] == "POST":
		body, _ := p["body"].(map[string]any)
		if f.configWrite != nil {
			return f.configWrite(f, path, body), true
		}
		var sent struct {
			Body json.RawMessage `json:"body"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sent)
		f.storeConfig(path, body)
		f.configs[path] = sent.Body // stored as sent, like automations.yaml
		return coreReply(200, map[string]any{"result": "ok"}), true
	case p["method"] == "DELETE":
		delete(f.configs, path)
		id := path[strings.LastIndex(path, "/")+1:]
		for i, row := range f.full {
			if row["unique_id"] == id {
				delete(f.states, fmt.Sprint(row["entity_id"]))
				f.full = append(f.full[:i], f.full[i+1:]...)
				break
			}
		}
		if f.deleteStatus != 0 {
			return coreReply(f.deleteStatus, map[string]any{"message": "Resource not found"}), true
		}
		return coreReply(200, map[string]any{"result": "ok"}), true
	}
	return nil, false
}

// storeConfig keeps a config and, like Home Assistant after the reload,
// registers its entity once.
func (f *fakeHA) storeConfig(path string, body map[string]any) {
	f.configs[path] = body
	parts := strings.Split(path, "/") // /api/config/<domain>/config/<id>
	domain, id := parts[3], parts[5]
	for _, row := range f.full {
		if row["unique_id"] == id {
			return
		}
	}
	entity := domain + "." + strings.ReplaceAll(strings.ToLower(fmt.Sprint(body["alias"])), " ", "_")
	f.ent(entity, domain, "on", map[string]any{"id": id})
	f.unique[entity] = id
	f.full = append(f.full, map[string]any{"entity_id": entity, "platform": domain, "unique_id": id})
}

func draftFile(t *testing.T, draft map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "draft.json")
	raw, _ := json.Marshal(draft)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func hallDraft(action string) map[string]any {
	return map[string]any{"alias": "Hall motion", "triggers": []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.hall"}},
		"actions": []any{step(action, "light.hall")}}
}

// writes lists the config writes (POST/DELETE /api/config/…) the Relay saw.
func (h *writeHarness) writes() []string {
	var out []string
	for _, c := range h.log.all() {
		if path := fmt.Sprint(c.payload["path"]); strings.HasPrefix(path, "/api/config/") && c.payload["method"] != "GET" {
			out = append(out, fmt.Sprint(c.payload["method"], " ", path))
		}
	}
	return out
}

func TestPlanWriteCreateIsPreviewAndApplyVerifies(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("light.turn_on")))
	if exit != 3 || env.Tier != "preview" || env.Reasons[0].Code != reasonConfigWrite || len(env.Diff) == 0 || env.Config == nil || env.Config.Op != "write" {
		t.Fatalf("plan: exit %d env %+v", exit, env)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || applied.Status != "verified" || applied.Verification[0].EntityID != "automation.hall_motion" || applied.Undo != nil {
		t.Fatalf("apply: exit %d env %+v", exit, applied)
	}
	if got := strings.Join(h.writes(), ","); got != "POST "+env.Config.Path {
		t.Fatalf("writes %s", got)
	}
}

func TestPlanWriteReachingAnUnlockIsTyped(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	draft := hallDraft("light.turn_on")
	draft["actions"] = []any{step("lock.unlock", "lock.front")}
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, draft))
	if exit != 3 || env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonAccessLock) || !strings.HasPrefix(env.Plan.Code, "confirm:write-") {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	if exit, env := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 4 || env.Error.Code != "CODE_REQUIRED" || len(h.writes()) != 0 {
		t.Fatalf("apply without code: exit %d env %+v", exit, env)
	}
}

// Update: an external edit drifts (nothing sent); key order does not; a
// verified update lands on the undo stack, and revert restores it.
func TestPlanWriteUpdateDriftUndoAndRevert(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
	h := newWriteHarness(t, f)
	updated := hallDraft("light.turn_off")
	plan := func() verbTestEnvelope {
		_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, updated))
		return env
	}
	id := plan().Plan.ID
	h.f.configs["/api/config/automation/config/a1"] = hallDraft("light.toggle") // external edit
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 2 || len(h.writes()) != 0 {
		t.Fatalf("external edit: exit %d env %+v", exit, env)
	}
	h.f.configs["/api/config/automation/config/a1"] = hallDraft("light.turn_on")
	id = plan().Plan.ID
	reordered, _ := json.Marshal(hallDraft("light.turn_on")) // same content, other key order and a volatile key
	h.f.configs["/api/config/automation/config/a1"] = json.RawMessage(`{"modified_at":"now",` + strings.TrimPrefix(string(reordered), "{"))
	exit, env := h.run(runApplyCommand, "--plan", id)
	if exit != 0 || env.Status != "verified" || env.Undo == nil || env.Undo.TargetID != "a1" {
		t.Fatalf("cosmetic difference drifted or no undo receipt: exit %d env %+v", exit, env)
	}

	_, reverted := h.run(runPlanCommand, "revert")
	if reverted.Tier != "preview" || reverted.Config == nil || reverted.Config.ConfigID != "a1" || len(reverted.Diff) == 0 {
		t.Fatalf("plan revert: %+v", reverted)
	}
	if exit, env := h.run(runApplyCommand, "--plan", reverted.Plan.ID); exit != 0 || env.Status != "verified" {
		t.Fatalf("apply revert: exit %d env %+v", exit, env)
	}
	stored, _ := json.Marshal(h.f.configs["/api/config/automation/config/a1"])
	if !strings.Contains(string(stored), "light.turn_on") {
		t.Fatalf("revert did not restore the before-config: %s", stored)
	}
}

func TestPlanRevertRefusesWhenLiveChanged(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, hallDraft("light.turn_off")))
	if exit, _ := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 {
		t.Fatal("update failed")
	}
	h.f.configs["/api/config/automation/config/a1"] = hallDraft("light.toggle") // edited after the update
	exit, reverted := h.run(runPlanCommand, "revert", "--target", "a1")
	if exit != 2 || reverted.Error.Code != "REVERT_DRIFT" || reverted.Plan != nil {
		t.Fatalf("exit %d env %+v", exit, reverted)
	}
}

func TestApplyWriteVerificationHonesty(t *testing.T) {
	for name, setup := range map[string]func(*fakeHA){
		"read-back differs": func(f *fakeHA) {
			f.configWrite = func(f *fakeHA, path string, body map[string]any) any {
				delete(body, "actions") // Home Assistant dropped part of the draft
				f.storeConfig(path, body)
				return coreReply(200, map[string]any{"result": "ok"})
			}
		},
		"reload failed": func(f *fakeHA) {
			f.service = func(f *fakeHA, path string, body map[string]any) any {
				return coreReply(500, map[string]any{"message": "boom"})
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHA()
			setup(f)
			h := newWriteHarness(t, f)
			_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("light.turn_on")))
			exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
			if exit != 6 || applied.Status != "unverified" || applied.Verification[0].Note == "" || len(h.writes()) != 1 {
				t.Fatalf("exit %d env %+v", exit, applied)
			}
		})
	}
}

// Delete: always typed, consumers listed, the snapshot is taken before the
// DELETE; a failed snapshot deletes nothing and keeps the plan.
func TestPlanDeleteIsTypedAndSnapshotsFirst(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/n1", map[string]any{"alias": "Night", "actions": []any{}})
	f.related["entity:automation.night"] = []string{"automation.caller"}
	h := newWriteHarness(t, f)
	exit, env := h.run(runPlanCommand, "delete", "automation.night")
	if exit != 3 || env.Tier != "typed" || !strings.HasPrefix(env.Plan.Code, "confirm:delete-night-") || len(env.Consumers) != 1 || env.Consumers[0].Automation != "automation.caller" {
		t.Fatalf("plan delete: exit %d env %+v", exit, env)
	}
	h.f.snapshotErr = true
	if exit, failed := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 1 || failed.Error.Code != "SNAPSHOT_FAILED" || len(h.writes()) != 0 {
		t.Fatalf("snapshot failure: exit %d env %+v", exit, failed)
	}
	h.f.snapshotErr = false
	exit, deleted := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if exit != 0 || deleted.Status != "verified" || deleted.Snapshot != "automations/auto-automation-night-20261016T120000001Z.json.gz" ||
		deleted.Restore != "ha-nova plan restore --snapshot "+deleted.Snapshot+" --id n1 --server default" {
		t.Fatalf("delete: exit %d env %+v", exit, deleted)
	}
	var order []string
	for _, c := range h.log.all() {
		if c.endpoint == "backups" || c.payload["method"] == "DELETE" {
			order = append(order, c.endpoint+" "+fmt.Sprint(c.payload["method"]))
		}
	}
	if strings.Join(order, ",") != "backups <nil>,backups <nil>,core DELETE" {
		t.Fatalf("request order %v: the snapshot must precede the DELETE", order)
	}
	saved, _ := json.Marshal(h.f.snapshots[0]["data"])
	if !strings.Contains(string(saved), "Night") {
		t.Fatalf("snapshot data %s", saved)
	}
}

// I7: one config POST even when the write times out; no reload follows.
func TestApplyWriteOutcomeUnknownSendsOnce(t *testing.T) {
	previous := relayWriteTimeout
	relayWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayWriteTimeout = previous })
	f := newFakeHA()
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("light.turn_on")))
	h.f.configWrite = func(*fakeHA, string, map[string]any) any { // the write outlives the client
		time.Sleep(400 * time.Millisecond)
		return coreReply(200, map[string]any{"result": "ok"})
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 7 || !applied.OutcomeUnknown || len(h.writes()) != 1 || len(h.posts()) != 1 {
		t.Fatalf("exit %d env %+v writes %v posts %d", exit, applied, h.writes(), len(h.posts()))
	}
}

// I8: two real processes race for one config plan; exactly one writes.
func TestApplyConfigPlanRaceSendsOnce(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("light.turn_on")))
	cmds := make([]*exec.Cmd, 2)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0], "-test.run=^TestApplyHelperProcess$", "-test.count=1")
		cmds[i].Env = append(os.Environ(), "HA_NOVA_TEST_APPLY_PLAN="+env.Plan.ID)
		if err := cmds[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	exits := 0
	for _, cmd := range cmds {
		_ = cmd.Wait()
		exits += cmd.ProcessState.ExitCode()
	}
	if exits != verbExitRefused || len(h.writes()) != 1 {
		t.Fatalf("exit codes sum %d, writes %v: want one 0, one 4, one write", exits, h.writes())
	}
}

// The undo stack also holds helper checkpoints (saved by the helper skill);
// a bare revert on one is refused and names the skill that owns it.
func TestPlanRevertRefusesHelperCheckpointNamingTheHelperSkill(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	record := []byte(`{"op":"update","domain":"input_number","target_id":"pool_max","before_config":{"id":"pool_max","max":30},"expected_after":{"id":"pool_max","max":35}}`)
	if _, err := saveUndoSnapshotBytes(h.paths, record); err != nil {
		t.Fatal(err)
	}
	exit, env := h.run(runPlanCommand, "revert")
	if exit != 4 || env.Error == nil || env.Error.Code != "REVERT_UNSUPPORTED" || !strings.Contains(env.Error.Message, "ha-nova:helper") || env.Plan != nil {
		t.Fatalf("exit %d env %+v", exit, env)
	}
}

// --create never overwrites: an existing id is refused, and a copied
// automation draft that still carries its original's id is refused before
// anything is planned.
func TestPlanWriteCreateNeverOverwrites(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
	h := newWriteHarness(t, f)
	if exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--create", "--id", "a1", "--file", draftFile(t, hallDraft("light.turn_off"))); exit != 4 || env.Error == nil || env.Error.Code != "CONFIG_ID_IN_USE" {
		t.Fatalf("existing id: exit %d env %+v", exit, env)
	}
	copied := hallDraft("light.turn_off")
	copied["id"] = "a1"
	if exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--create", "--file", draftFile(t, copied)); exit != 1 || env.Error == nil || env.Error.Code != "USAGE" {
		t.Fatalf("copied id: exit %d env %+v", exit, env)
	}
	if exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--create", "--file", draftFile(t, hallDraft("light.turn_off"))); exit != 3 || env.Config == nil || env.Config.Op != "write" {
		t.Fatalf("fresh create: exit %d env %+v", exit, env)
	}
	if len(h.writes()) != 0 {
		t.Fatalf("planning wrote: %v", h.writes())
	}
}
