package main

import (
	"strings"
	"sync"
	"testing"
)

// replaceRelay is a raw relay with the entity sensor.desk_power (aliases Desk and
// Lamp, labels work, and sensor options), the device dlamp (label work),
// the area office (aliases Study, label work), the floor upstairs (alias
// Up), and the voice assistant home (prefers local intents); change swaps
// one of them, as another client would, and extra adds handlers.
func replaceRelay(t *testing.T, extra map[string]func(map[string]any) any) (paths runtimePaths, sent func() []string, change func(key string, item map[string]any)) {
	t.Helper()
	var mu sync.Mutex
	items := map[string]map[string]any{
		"entity":   {"entity_id": "sensor.desk_power", "name": nil, "aliases": []any{"Desk", "Lamp"}, "labels": []any{"work"}, "options": map[string]any{"sensor": map[string]any{"display_precision": 1, "unit_of_measurement": "W"}}},
		"device":   {"id": "dlamp", "name": "Lamp", "labels": []any{"work"}},
		"area":     {"area_id": "office", "name": "Office", "aliases": []any{"Study"}, "labels": []any{"work"}},
		"floor":    {"floor_id": "upstairs", "name": "Upstairs", "aliases": []any{"Up"}},
		"pipeline": {"id": "home", "name": "Home", "language": "en", "prefer_local_intents": true},
	}
	get := func(key string) map[string]any { mu.Lock(); defer mu.Unlock(); return items[key] }
	handlers := map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "sensor.desk_power", "device_id": "dlamp"}}
		},
		"config/entity_registry/get":  func(map[string]any) any { return get("entity") },
		"config/device_registry/list": func(map[string]any) any { return []any{get("device")} },
		"config/area_registry/list":   func(map[string]any) any { return []any{get("area")} },
		"config/floor_registry/list":  func(map[string]any) any { return []any{get("floor")} },
		"assist_pipeline/pipeline/list": func(map[string]any) any {
			return map[string]any{"pipelines": []any{get("pipeline")}, "preferred_pipeline": "home"}
		},
	}
	for key, handle := range extra {
		handlers[key] = handle
	}
	paths, sent = rawRelay(t, handlers)
	return paths, sent, func(key string, item map[string]any) { mu.Lock(); defer mu.Unlock(); items[key] = item }
}

const (
	entityCopy   = `{"endpoint":"backups","payload":{"action":"save","category":"metadata","name":"auto-sensor-desk-power","data":{"entity_id":"sensor.desk_power","name":null,"aliases":["Desk","Lamp"],"labels":["work"],"options":{"sensor":{"display_precision":1,"unit_of_measurement":"W"}}}}}`
	areaCopy     = `{"endpoint":"backups","payload":{"action":"save","category":"metadata","name":"auto-office","data":{"area_id":"office","name":"Office","aliases":["Study"],"labels":["work"]}}}`
	areaKeepsAll = `{"endpoint":"ws","payload":{"type":"config/area_registry/update","area_id":"office","aliases":[" Study ","Den"],"labels":["work"]}}`
)

// A registry, person, or voice assistant update that leaves out what the
// live item holds is refused unless its copy is saved first in the same
// plan, and the refusal says what would go and how to keep it: an alias or
// label left out of a list it sends, a key left out of the options it
// sends, a setting left out of a voice assistant. One that keeps everything
// (an alias with spaces around it, a list it doesn't send, or null) is a
// preview, and so is one after its copy.
func TestPlanRawReplacingUpdateThatDropsNeedsACopyFirst(t *testing.T) {
	paths, sent, _ := replaceRelay(t, nil)
	ws := func(payload string) string { return `{"endpoint":"ws","payload":` + payload + `}` }
	for payload, says := range map[string]string{
		`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","aliases":["Desk"]}`:                                          `would remove the aliases, labels, or options "Lamp" (aliases) from it`,
		`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","labels":[]}`:                                                 `"work" (labels)`,
		`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","options_domain":"sensor","options":{"display_precision":2}}`: `"unit_of_measurement" (sensor options)`,
		`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","options_domain":"sensor","options":null}`:                    `"display_precision" (sensor options), "unit_of_measurement" (sensor options)`,
		`{"type":"config/device_registry/update","device_id":"dlamp","labels":["home"]}`:                                                       `would remove the labels "work" from it`,
		`{"type":"config/area_registry/update","area_id":"office","aliases":["Den"]}`:                                                          `would remove the aliases or labels "Study" (aliases) from it`,
		`{"type":"config/floor_registry/update","floor_id":"upstairs","aliases":[]}`:                                                           `would remove the aliases "Up" from it`,
		`{"type":"assist_pipeline/pipeline/update","pipeline_id":"home","name":"Home","language":"de"}`:                                        `would remove the settings "prefer_local_intents" from it`,
	} {
		exit, env := planRaw(t, paths, "["+ws(payload)+"]")
		if exit != verbExitRefused || env.Error == nil || env.Error.Code != "REFUSED" || !strings.Contains(env.Error.Message, says) ||
			!strings.Contains(env.Error.Next, `"action":"save","category":"`) || !strings.Contains(env.Error.Next, "If they were left out by mistake, read it again and send every") {
			t.Errorf("%s: exit %d %+v", payload, exit, env.Error)
		}
	}
	for _, items := range []string{
		areaKeepsAll,
		ws(`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","name":"Desk lamp","aliases":null}`),
		ws(`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","options_domain":"sensor","options":{"display_precision":2,"unit_of_measurement":"kW"}}`),
		ws(`{"type":"assist_pipeline/pipeline/update","pipeline_id":"home","name":"Home","language":"de","prefer_local_intents":true}`),
		entityCopy + "," + ws(`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","aliases":["Desk"],"labels":[]}`),
		areaCopy + "," + ws(`{"type":"config/area_registry/update","area_id":"office","aliases":[]}`),
	} {
		if exit, env := planRaw(t, paths, "["+items+"]"); exit != verbExitConfirmation || env.Tier != "preview" {
			t.Errorf("%s: exit %d tier %s %+v", items, exit, env.Tier, env.Error)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning sent %v", got)
	}
}

// A registry update that sends no list it could drop from reads nothing for
// it: a rename plans without the entity's registry entry.
func TestPlanRawRegistryUpdateWithoutListsReadsNoEntry(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any { return []any{map[string]any{"entity_id": "sensor.desk_power"}} },
		"config/entity_registry/get":  func(map[string]any) any { return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "no answer"} },
	})
	if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","name":"Desk lamp"}}]`); exit != verbExitConfirmation {
		t.Fatalf("rename: exit %d %+v", exit, env.Error)
	}
	if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","labels":["work"]}}]`); exit != verbExitError || env.Plan != nil ||
		env.Error == nil || !strings.Contains(env.Error.Message, "cannot read the entity sensor.desk_power") {
		t.Fatalf("labels with an unreadable entry: exit %d %+v", exit, env.Error)
	}
}

// After its copy, an update says what it removes, in the home's language;
// apply re-reads the item before the update, so one changed since the
// preview is drift and one changed while the copy saved is refused unsent.
func TestPlanRawReplacingUpdateSaysWhatGoesAndRechecks(t *testing.T) {
	removeStudy := areaCopy + `,{"endpoint":"ws","payload":{"type":"config/area_registry/update","area_id":"office","aliases":[]}}`
	for lang, says := range map[string]string{"en": "Removes “Study”.", "de": "Entfernt „Study“."} {
		paths, _, _ := replaceRelay(t, map[string]func(map[string]any) any{
			"GET /api/config": func(map[string]any) any {
				return map[string]any{"status": 200, "body": map[string]any{"language": lang}}
			},
		})
		if exit, env := planRaw(t, paths, "["+removeStudy+"]"); exit != verbExitConfirmation || !strings.Contains(env.Say, says) {
			t.Errorf("%s: exit %d say %q %+v", lang, exit, env.Say, env.Error)
		}
	}
	for _, tc := range []struct {
		area       map[string]any
		code, says string
	}{
		{map[string]any{"area_id": "office", "name": "Home office", "aliases": []any{"Study"}, "labels": []any{"work"}}, "PLAN_DRIFT", "nothing was sent"},
		{map[string]any{"area_id": "office", "name": "Office", "aliases": []any{"Study", "Library"}, "labels": []any{"work"}}, "REFUSED", `"Library"`},
	} {
		paths, sent, change := replaceRelay(t, nil)
		_, env := planRaw(t, paths, "["+areaKeepsAll+"]")
		change("area", tc.area)
		if exit, applied := applyRaw(t, paths, env); applied.Error == nil || applied.Error.Code != tc.code || !strings.Contains(applied.Error.Message, tc.says) || len(sent()) != 0 {
			t.Errorf("apply after the area changed: exit %d %+v %v", exit, applied.Error, sent())
		}
	}
	var change func(string, map[string]any)
	paths, sent, change := replaceRelay(t, map[string]func(map[string]any) any{
		"backups save": func(map[string]any) any {
			change("area", map[string]any{"area_id": "office", "name": "Office", "aliases": []any{"Study", "Library"}, "labels": []any{"work"}})
			return map[string]any{"file": "metadata/auto-office.json.gz"}
		},
	})
	_, env := planRaw(t, paths, "["+removeStudy+"]")
	exit, applied := applyRaw(t, paths, env)
	if outcomes := rawOutcomes(t, applied); exit == 0 || statuses(outcomes) != "done,refused" || !strings.Contains(outcomes[1].Error, "the area office changed in Home Assistant since the preview") ||
		strings.Join(sent(), " | ") != "backups save" {
		t.Fatalf("an area changed while its copy saved: exit %d %+v %s %v", exit, applied.Error, applied.Result, sent())
	}
}

// ha-nova relay ws sends one request, so no copy can come first: an update
// that drops an alias is refused there, also inside a collect_events
// envelope, with the plan that keeps a copy; one that keeps them goes.
func TestRelayReplacingUpdateThatDropsIsRefused(t *testing.T) {
	paths, sent, _ := replaceRelay(t, nil)
	drop := `{"type":"config/floor_registry/update","floor_id":"upstairs","aliases":["Top"]}`
	for _, body := range []string{drop, `{"message":` + drop + `,"collect_events":{"max_events":1}}`} {
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", body}) })
		if exit != rawRefusedExit || !strings.Contains(stderr, "refused raw config/floor_registry/update") || !strings.Contains(stderr, `would remove the aliases "Up"`) ||
			!strings.Contains(stderr, `"category":"metadata","name":"auto-upstairs"`) {
			t.Errorf("%s: exit %d %s", body, exit, stderr)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("refused updates sent %v", got)
	}
	var exit int
	stderr := captureStderr(t, func() {
		exit = runRelayCommand(paths, []string{"ws", "--data", `{"type":"config/floor_registry/update","floor_id":"upstairs","aliases":["Up","Top"]}`})
	})
	if exit != 0 || strings.Join(sent(), " | ") != "config/floor_registry/update" {
		t.Fatalf("an update keeping every alias: exit %d %s %v", exit, stderr, sent())
	}
}

// Home Assistant strips aliases but keeps a label id as sent (one it
// doesn't know is dropped), and a computed-name alias (null) is not the
// text "null": each comparison follows it. Two spellings of one entity_id
// are one entity, updated once per plan; an unknown one is NOT_FOUND. A
// value HA NOVA hides elsewhere in the entry doesn't block a removal after
// its copy: a restore sends back only the dropped list.
func TestPlanRawRegistryDropsCompareAsHomeAssistantStores(t *testing.T) {
	paths, _, change := replaceRelay(t, nil)
	update := func(payload string) string { return `[{"endpoint":"ws","payload":` + payload + `}]` }
	if exit, env := planRaw(t, paths, update(`{"type":"config/area_registry/update","area_id":"office","labels":["work "]}`)); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"work" (labels)`) {
		t.Errorf("a label with a space: exit %d %+v", exit, env.Error)
	}
	change("entity", map[string]any{"entity_id": "sensor.desk_power", "aliases": []any{nil, "Desk"}, "options": map[string]any{"lock": map[string]any{"default_code": secretHidden}}, "labels": []any{"work"}})
	if exit, env := planRaw(t, paths, update(`{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","aliases":["null","Desk"]}`)); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"null" (aliases)`) {
		t.Errorf("a null alias sent as text: exit %d %+v", exit, env.Error)
	}
	hiddenCopy := `{"endpoint":"backups","payload":{"action":"save","category":"metadata","name":"auto-sensor-desk-power","data":{"entity_id":"sensor.desk_power","aliases":[null,"Desk"],"options":{"lock":{"default_code":"` + secretHidden + `"}},"labels":["work"]}}}`
	if exit, env := planRaw(t, paths, "["+hiddenCopy+`,{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","labels":[]}}]`); exit != verbExitConfirmation {
		t.Errorf("a label removal beside a hidden value: exit %d %+v", exit, env.Error)
	}
	twice := `[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"sensor.desk_power","labels":["work"]}},{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"SENSOR.Desk_Power","labels":["work","home"]}}]`
	if exit, env := planRaw(t, paths, twice); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "once") {
		t.Errorf("one entity in two spellings: exit %d %+v", exit, env.Error)
	}
	gone, _ := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/get": func(map[string]any) any {
			return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "not_found: Entity not found"}
		},
	})
	if exit, env := planRaw(t, gone, update(`{"type":"config/entity_registry/update","entity_id":"sensor.gone","labels":[]}`)); exit != verbExitNotFound || env.Error == nil || env.Error.Code != "NOT_FOUND" {
		t.Errorf("an unknown entity: exit %d %+v", exit, env.Error)
	}
}
