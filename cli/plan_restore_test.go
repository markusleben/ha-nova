package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	nightPath = "/api/config/automation/config/n1"
	// Key order is behavior for variables; neither map is in sorted order.
	nightRaw     = `{"id":"n1","alias":"Night","variables":{"z":1,"a":2},"triggers":[],"actions":[{"action":"light.turn_off","target":{"entity_id":"light.hall"}}]}`
	snapAutoFile = "automations/auto-automation-night-20261016T120000000Z.json.gz"
	snapScript   = "scripts/auto-script-night-mode-20261016T120000000Z.json.gz"
)

// storedJSON shows a fake config as stored (raw bytes stay as they are).
func storedJSON(config any) string {
	raw, _ := json.Marshal(config)
	return string(raw)
}

// restoreArgs turns a delete result's restore command into plan arguments.
func restoreArgs(t *testing.T, command string) []string {
	t.Helper()
	rest, ok := strings.CutPrefix(command, "ha-nova plan ")
	if !ok {
		t.Fatalf("restore %q is not a plan command", command)
	}
	return strings.Fields(rest)
}

func nightHarness(t *testing.T) *writeHarness {
	f := newFakeHA()
	f.storeConfig(nightPath, map[string]any{"alias": "Night"})
	f.configs[nightPath] = json.RawMessage(nightRaw)
	return newWriteHarness(t, f)
}

// The delete names its undo; that one plan call restores the snapshot's
// bytes (key order kept) under the original id, and apply verifies it.
func TestPlanRestoreUndoesADeleteWithTheSnapshotBytes(t *testing.T) {
	h := nightHarness(t)
	_, env := h.run(runPlanCommand, "delete", "automation.night")
	exit, deleted := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if exit != 0 || deleted.Restore == "" {
		t.Fatalf("delete: exit %d env %+v", exit, deleted)
	}
	exit, planned := h.run(runPlanCommand, restoreArgs(t, deleted.Restore)...)
	if exit != 3 || planned.Tier != "preview" || reasonCodes(planned) != reasonConfigRestore || planned.Config == nil ||
		planned.Config.Op != "restore" || planned.Config.ConfigID != "n1" || planned.Snapshot != "" || planned.Say == "" {
		t.Fatalf("plan restore: exit %d env %+v", exit, planned)
	}
	if string(planned.Config.Body) != nightRaw {
		t.Fatalf("restore body %s, want the snapshot bytes %s", planned.Config.Body, nightRaw)
	}
	exit, applied := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != 0 || applied.Status != "verified" || applied.Undo != nil {
		t.Fatalf("apply restore: exit %d env %+v", exit, applied)
	}
	if got := storedJSON(h.f.configs[nightPath]); got != nightRaw {
		t.Fatalf("stored %s, want the exact snapshot bytes", got)
	}
	if exit, again := h.run(runPlanCommand, restoreArgs(t, deleted.Restore)...); exit != 4 || again.Error.Code != "CONFIG_ID_IN_USE" {
		t.Fatalf("second restore: exit %d env %+v", exit, again)
	}
}

func TestPlanRestoreScriptNeedsItsKey(t *testing.T) {
	f := newFakeHA()
	f.snapshotFiles = map[string]json.RawMessage{snapScript: json.RawMessage(`{"alias":"Night mode","sequence":[]}`)}
	h := newWriteHarness(t, f)
	if exit, env := h.run(runPlanCommand, "restore", "--snapshot", snapScript); exit != 1 || env.Error.Code != "USAGE" {
		t.Fatalf("no --id: exit %d env %+v", exit, env)
	}
	exit, env := h.run(runPlanCommand, "restore", "--snapshot", snapScript, "--id", "night_mode")
	if exit != 3 || env.Config.Path != "/api/config/script/config/night_mode" || env.Tier != "preview" {
		t.Fatalf("script restore: exit %d env %+v", exit, env)
	}
}

// A snapshot must hold one config of its file's domain, under the id named.
func TestPlanRestoreRefusesForeignSnapshots(t *testing.T) {
	cases := []struct {
		name, file, data, code string
		args                   []string
		exit                   int
	}{
		{"automation in scripts/", snapScript, nightRaw, "SNAPSHOT_MISMATCH", []string{"--id", "night_mode"}, 4},
		{"script in automations/", snapAutoFile, `{"alias":"Night mode","sequence":[]}`, "SNAPSHOT_MISMATCH", nil, 4},
		{"other automation id", snapAutoFile, nightRaw, "SNAPSHOT_MISMATCH", []string{"--id", "other"}, 4},
		{"not an object", snapAutoFile, `[1]`, "SNAPSHOT_MISMATCH", nil, 4},
		{"scenes category", "scenes/auto-scene-x-20261016T120000000Z.json.gz", `{}`, "USAGE", nil, 1},
		{"missing file", "automations/auto-gone-20261016T120000000Z.json.gz", "", "SNAPSHOT_NOT_FOUND", nil, 5},
		{"corrupt file", snapAutoFile, "corrupt", "SNAPSHOT_CORRUPT", nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHA()
			if tc.data != "" {
				f.snapshotFiles = map[string]json.RawMessage{tc.file: json.RawMessage(tc.data)}
			}
			h := newWriteHarness(t, f)
			exit, env := h.run(runPlanCommand, append([]string{"restore", "--snapshot", tc.file}, tc.args...)...)
			if exit != tc.exit || env.Error == nil || env.Error.Code != tc.code || env.Plan != nil {
				t.Fatalf("exit %d env %+v", exit, env)
			}
		})
	}
}

// Never an overwrite: an id that exists at plan time is refused, and one
// that appears between preview and apply is refused at apply.
func TestPlanRestoreNeverOverwrites(t *testing.T) {
	h := nightHarness(t)
	h.f.snapshotFiles = map[string]json.RawMessage{snapAutoFile: json.RawMessage(nightRaw)}
	if exit, env := h.run(runPlanCommand, "restore", "--snapshot", snapAutoFile); exit != 4 || env.Error.Code != "CONFIG_ID_IN_USE" {
		t.Fatalf("existing id: exit %d env %+v", exit, env)
	}
	f := newFakeHA()
	f.snapshotFiles = h.f.snapshotFiles
	h = newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "restore", "--snapshot", snapAutoFile)
	h.f.storeConfig(nightPath, hallDraft("light.toggle")) // recreated meanwhile
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 4 || applied.Error.Code != "CONFIG_ID_IN_USE" || len(h.writes()) != 0 {
		t.Fatalf("apply over a recreated id: exit %d env %+v writes %v", exit, applied, h.writes())
	}
}

// The tier comes from the content: a restored automation that unlocks a
// door is typed. Apply writes the planned bytes, never a re-read snapshot.
func TestPlanRestoreKeepsTierAndPlannedBody(t *testing.T) {
	f := newFakeHA()
	unlock := `{"id":"n1","alias":"Night","triggers":[],"actions":[{"action":"lock.unlock","target":{"entity_id":"lock.front"}}]}`
	f.snapshotFiles = map[string]json.RawMessage{snapAutoFile: json.RawMessage(unlock)}
	h := newWriteHarness(t, f)
	exit, env := h.run(runPlanCommand, "restore", "--snapshot", snapAutoFile, "--id", "n1")
	if exit != 3 || env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonAccessLock) || !strings.HasPrefix(env.Plan.Code, "confirm:restore-night-") {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	if exit, refused := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 4 || refused.Error.Code != "CODE_REQUIRED" || len(h.writes()) != 0 {
		t.Fatalf("apply without code: exit %d env %+v", exit, refused)
	}
	h.f.snapshotFiles[snapAutoFile] = json.RawMessage(`{"id":"n1","alias":"Night","triggers":[],"actions":[]}`) // swapped after the preview
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 0 || applied.Status != "verified" {
		t.Fatalf("apply: exit %d env %+v", exit, applied)
	}
	if got := storedJSON(h.f.configs[nightPath]); got != unlock {
		t.Fatalf("stored %s, want the previewed body", got)
	}
}

func TestMCPPlanRestore(t *testing.T) {
	argv, err := planArgv(map[string]json.RawMessage{"kind": json.RawMessage(`"restore"`), "snapshot": json.RawMessage(`"` + snapScript + `"`), "id": json.RawMessage(`"night_mode"`)}, nil)
	if err != nil || strings.Join(argv, " ") != "restore --snapshot="+snapScript+" --id=night_mode" {
		t.Fatalf("argv %v err %v", argv, err)
	}
	if _, err := planArgv(map[string]json.RawMessage{"kind": json.RawMessage(`"restore"`), "snapshot": json.RawMessage(`"--id=x"`)}, nil); err == nil {
		t.Fatal("a flag-shaped snapshot was accepted")
	}
	step := nextStep(map[string]any{"status": "verified", "restore": "ha-nova plan restore --snapshot " + snapScript + " --id night_mode --server home"})
	if step != "If the user asks to undo this delete: ha_plan kind restore snapshot "+snapScript+" id night_mode." {
		t.Fatalf("next_step %q", step)
	}
}
