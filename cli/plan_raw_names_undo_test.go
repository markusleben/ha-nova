package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Calmer confirmations item 9: renaming one entity or device outside every
// access family runs without a preview, its way back saved first; a revert
// puts it back, previewed.

const (
	deskRename  = `[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","name":"Desk lamp"}}]`
	deskAliases = `[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","aliases":["Desk","Lamp","Reading"]}}]`
	lampRename  = `[{"endpoint":"ws","payload":{"type":"config/device_registry/update","device_id":"dlamp","name_by_user":"Reading lamp"}}]`
)

func namesRecords(t *testing.T, paths runtimePaths) []undoSnapshot {
	t.Helper()
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil {
		t.Fatal(err)
	}
	var names []undoSnapshot
	for _, snap := range stack.Snapshots {
		if snap.Op == namesUndoOp {
			names = append(names, snap)
		}
	}
	return names
}

func TestRawRenameRunsWithItsWayBackSavedFirst(t *testing.T) {
	var renamed map[string]any
	var paths runtimePaths
	paths, _, change := replaceRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/update": func(body map[string]any) any {
			if len(namesRecords(t, paths)) != 1 {
				t.Errorf("the way back was not saved before the write")
			}
			renamed = body
			return map[string]any{}
		},
	})
	exit, env := planRaw(t, paths, deskRename)
	if exit != verbExitOK || env.Tier != "ordinary" || env.Plan == nil || env.Plan.Confirm != "none" ||
		!strings.Contains(env.NextStep, "No ask: the CLI checked this change and saves its way back first. Run ha-nova apply --plan "+env.Plan.ID+" now, then show its `say`.") {
		t.Fatalf("plan: exit %d %+v", exit, env)
	}
	exit, applied := applyRaw(t, paths, env)
	if exit != verbExitOK || applied.Undo == nil || applied.Undo.Op != namesUndoOp || applied.Undo.TargetID != "sensor.desk_power" ||
		!strings.HasSuffix(applied.Say, "You can put it back with “undo”.") || renamed["name"] != "Desk lamp" {
		t.Fatalf("apply: exit %d %+v", exit, applied)
	}
	records := namesRecords(t, paths)
	compact := func(raw []byte) string { var out bytes.Buffer; _ = json.Compact(&out, raw); return out.String() }
	if len(records) != 1 || compact(records[0].BeforeConfig) != `{"name":null}` || compact(records[0].ExpectedAfter) != `{"name":"Desk lamp"}` || records[0].Domain != "entity_registry" {
		t.Fatalf("record %+v", records)
	}

	change("entity", map[string]any{"entity_id": "sensor.desk_power", "name": "Desk lamp", "aliases": []any{"Desk", "Lamp"}, "labels": []any{"work"},
		"options": map[string]any{"sensor": map[string]any{"display_precision": 1, "unit_of_measurement": "W"}}})
	if exit, again := planRaw(t, paths, deskRename); exit != verbExitConfirmation || again.Tier != "preview" {
		t.Fatalf("a second rename replaced the first way back: exit %d tier %s", exit, again.Tier)
	}
	exit, revert := runVerbForTest(t, func() int { return runPlanCommand(paths, []string{"revert", "--target", "sensor.desk_power"}) })
	if exit != verbExitConfirmation || revert.Tier != "preview" || revert.Plan == nil {
		t.Fatalf("revert: exit %d %+v", exit, revert)
	}
	if exit, back := applyRaw(t, paths, revert); exit != verbExitOK || renamed["name"] != nil || len(renamed) != 3 {
		t.Fatalf("revert apply: exit %d %+v sent %v", exit, back, renamed)
	}
	change("entity", map[string]any{"entity_id": "sensor.desk_power", "name": nil, "aliases": []any{"Desk", "Lamp"}})
	if exit, done := runVerbForTest(t, func() int { return runPlanCommand(paths, []string{"revert"}) }); exit != verbExitRefused || done.Error == nil || done.Error.Code != "NOTHING_TO_UNDO" {
		t.Fatalf("already back: exit %d %+v", exit, done.Error)
	}
	change("entity", map[string]any{"entity_id": "sensor.desk_power", "name": "Lamp 2", "aliases": []any{"Desk", "Lamp"}})
	if exit, drift := runVerbForTest(t, func() int { return runPlanCommand(paths, []string{"revert"}) }); exit != verbExitDrift || drift.Error == nil || drift.Error.Code != "REVERT_DRIFT" {
		t.Fatalf("drift: exit %d %+v", exit, drift.Error)
	}
}

// A device rename and an added alias run the same way; putting an alias
// back that the rename added saves the entity's copy first.
func TestRawRenameOfDeviceAndAliases(t *testing.T) {
	paths, sent, change := replaceRelay(t, nil)
	if exit, env := planRaw(t, paths, lampRename); exit != verbExitOK || env.Plan.Confirm != "none" {
		t.Fatalf("device: exit %d %+v", exit, env)
	} else if exit, applied := applyRaw(t, paths, env); exit != verbExitOK || applied.Undo == nil || applied.Undo.Domain != "device_registry" {
		t.Fatalf("device apply: exit %d %+v", exit, applied)
	}
	exit, env := planRaw(t, paths, deskAliases)
	if exit != verbExitOK || env.Plan.Confirm != "none" {
		t.Fatalf("aliases: exit %d %+v", exit, env)
	}
	if exit, applied := applyRaw(t, paths, env); exit != verbExitOK || applied.Undo == nil {
		t.Fatalf("aliases apply: exit %d %+v", exit, applied)
	}
	added := map[string]any{"entity_id": "sensor.desk_power", "name": nil, "aliases": []any{"Lamp", "Desk", "Reading"}, "labels": []any{"work"}}
	change("entity", added)
	exit, revert := runVerbForTest(t, func() int { return runPlanCommand(paths, []string{"revert", "--target", "sensor.desk_power"}) })
	if exit != verbExitConfirmation || revert.Plan == nil {
		t.Fatalf("alias revert: exit %d %+v", exit, revert)
	}
	before := len(sent())
	if exit, back := applyRaw(t, paths, revert); exit != verbExitOK {
		t.Fatalf("alias revert apply: exit %d %+v", exit, back)
	}
	if got := sent()[before:]; len(got) != 2 || !strings.HasPrefix(got[0], "backups") {
		t.Fatalf("alias revert sent %v", got)
	}
}

// Everything else stays a preview: an access family, an id, area, or
// label change, more than one item, a failed read, or no room for the
// way back. A rejected rename leaves no way back behind.
func TestRawRenameStaysPreviewOtherwise(t *testing.T) {
	ws := func(payload string) string { return `[{"endpoint":"ws","payload":` + payload + `}]` }
	paths, _, _ := replaceRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "sensor.desk_power", "device_id": "dlamp"}, map[string]any{"entity_id": "lock.front", "device_id": "dfront"}}
		},
	})
	for _, items := range []string{
		ws(`{"type":"config/entity_registry/update","entity_id":"lock.front","name":"Front"}`),
		ws(`{"type":"config/device_registry/update","device_id":"dfront","name_by_user":"Front"}`),
		ws(`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","name":"Desk","new_entity_id":"sensor.desk"}`),
		ws(`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","name":"Desk","area_id":"office"}`),
		ws(`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","name":"Desk","aliases":null}`),
		ws(`{"type":"config/device_registry/update","device_id":"dlamp","name":"Lamp"}`),
		`[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","name":"Desk"}},{"endpoint":"ws","payload":{"type":"config/device_registry/update","device_id":"dlamp","name_by_user":"Lamp"}}]`,
	} {
		if exit, env := planRaw(t, paths, items); exit != verbExitConfirmation || env.Tier == "ordinary" {
			t.Errorf("%s: exit %d tier %s %+v", items, exit, env.Tier, env.Error)
		}
	}

	unread, _, _ := replaceRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/get": func(map[string]any) any { return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "timeout"} },
	})
	if exit, env := planRaw(t, unread, deskRename); env.Tier == "ordinary" {
		t.Errorf("unreadable entity: exit %d %+v", exit, env)
	}

	full, _, _ := replaceRelay(t, nil)
	saveOtherCheckpoints(t, full, undoSnapshotStackLimit, "a")
	if exit, env := planRaw(t, full, deskRename); exit != verbExitConfirmation || env.Tier != "preview" {
		t.Errorf("full undo list: exit %d tier %s", exit, env.Tier)
	}

	var rejected runtimePaths
	rejected, _, _ = replaceRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/update": func(map[string]any) any {
			return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "invalid_format"}
		},
	})
	exit, env := planRaw(t, rejected, deskRename)
	if exit != verbExitOK {
		t.Fatalf("plan: exit %d %+v", exit, env)
	}
	if exit, applied := applyRaw(t, rejected, env); exit == verbExitOK || applied.Undo != nil || len(namesRecords(t, rejected)) != 0 {
		t.Fatalf("rejected rename kept a way back: exit %d %+v %v", exit, applied, namesRecords(t, rejected))
	}
}
