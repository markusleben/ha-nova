package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// deadRefHome: a home with one automation that names every kind of broken
// reference, a disabled one read from its stored config, one whose config
// can't be read, a YAML one on a merged device id, and a clean script.
func deadRefReply(automations int, fail string) func(*http.Request, string, map[string]any) any {
	registry := []map[string]any{
		{"id": "r1", "entity_id": "automation.broken", "platform": "automation", "unique_id": "b1"},
		{"id": "r2", "entity_id": "automation.disabled_one", "platform": "automation", "unique_id": "d1", "disabled_by": "user"},
		{"id": "r3", "entity_id": "automation.unloaded", "platform": "automation", "unique_id": "u1"},
		{"id": "r4", "entity_id": "script.ok", "platform": "script", "unique_id": "ok"},
		{"id": "r5", "entity_id": "light.ok", "platform": "hue"},
		{"id": "0123456789abcdef0123456789abcdef", "entity_id": "light.off_disabled", "platform": "hue", "disabled_by": "integration"},
		{"id": "r7", "entity_id": "sensor.not_loaded", "platform": "zha"},
	}
	states := []map[string]any{
		{"entity_id": "automation.broken", "state": "on", "attributes": map[string]any{"friendly_name": "Broken one", "id": "b1"}},
		{"entity_id": "automation.yaml_noid", "state": "on", "attributes": map[string]any{"friendly_name": "YAML"}},
		{"entity_id": "automation.empty", "state": "on", "attributes": map[string]any{}},
		{"entity_id": "script.ok", "state": "off", "attributes": map[string]any{"friendly_name": "OK"}},
		{"entity_id": "light.ok", "state": "on", "attributes": map[string]any{}},
		{"entity_id": "sensor.flaky", "state": "unavailable", "attributes": map[string]any{}},
		{"entity_id": "scene.ok", "state": "2026-11-24T10:00:00+00:00", "attributes": map[string]any{}},
	}
	loaded := map[string]any{
		"automation.broken": map[string]any{"alias": "Broken one",
			"triggers": []any{
				map[string]any{"trigger": "state", "entity_id": "sensor.gone", "to": "on"},
				map[string]any{"trigger": "event", "event_type": "mobile_app_notification_action", "event_data": map[string]any{"action": "foo.bar"}},
				map[string]any{"trigger": "state", "entity_id": "{{ watched }}"},
				map[string]any{"trigger": "numeric_state", "entity_id": "light.ok", "above": "sensor.limit_gone", "below": 30},
				map[string]any{"trigger": "zone", "entity_id": "light.ok", "zone": "zone.gone", "event": "enter"},
				map[string]any{"trigger": "event", "event_type": "state_changed", "event_data": map[string]any{"entity_id": "sensor.ev_gone"}},
			},
			"variables":  map[string]any{"entity_id": "sensor.var_gone", "t": "{{ states('sensor.var_tpl_gone') }}"},
			"conditions": []any{map[string]any{"condition": "device", "device_id": "dev_gone", "entity_id": "0123456789abcdef0123456789abcdef", "domain": "light", "type": "is_on"}},
			"actions": []any{
				map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": []any{"light.ok", "light.off_disabled"}, "area_id": "attic"}},
				map[string]any{"action": "hue.activate_scene", "data": map[string]any{"message": "{{ states('sensor.gone2') }} {{ states.sensor.flaky.state }} {{ is_state('light.ok', 'on') }}", "entity_id": "sensor.not_loaded, sensor.gone"}},
				map[string]any{"scene": "scene.gone"},
				map[string]any{"scene": "scene.ok"},
				map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": "all"}},
				map[string]any{"action": "scene.apply", "data": map[string]any{"entities": map[string]any{"light.ok": "on", "light.scene_gone": "off"}}},
				map[string]any{"action": "tts.speak", "target": map[string]any{"entity_id": "light.ok"}, "data": map[string]any{"media_player_entity_id": "media_player.gone",
					"message": "{{ states('sensor.base_' ~ x) }} {{ expand('light.ok', 'light.exp_gone') | count }}"}},
				map[string]any{"action": "notify.mobile_app_x", "data": map[string]any{"message": "hi", "data": map[string]any{"entity_id": "camera.gone"}}},
				map[string]any{"repeat": map[string]any{"for_each": []any{map[string]any{"entity_id": "light.foreach_gone"}}, "sequence": []any{}}},
				map[string]any{"action": "light.turn_on", "target": map[string]any{"area_id": "{{ ['attic', 'cellar'] | first }}", "device_id": "{{ a, b }}"}},
			}},
		"automation.empty": map[string]any{"triggers": []any{}, "actions": nil},
		"automation.yaml_noid": map[string]any{"triggers": []any{map[string]any{"trigger": "device", "device_id": "merged1", "domain": "zha", "type": "remote_button_short_press"}},
			"actions": []any{map[string]any{"action": "script.ok"}}},
		"script.ok": map[string]any{"alias": "OK", "sequence": []any{map[string]any{"action": "light.toggle", "target": map[string]any{"entity_id": "light.ok", "device_id": "dev_ok", "area_id": "kitchen"}}}},
	}
	for i := 0; i < automations; i++ {
		id := fmt.Sprintf("automation.many_%03d", i)
		states = append(states, map[string]any{"entity_id": id, "state": "on", "attributes": map[string]any{}})
		loaded[id] = map[string]any{"triggers": []any{}, "actions": []any{map[string]any{"action": "light.turn_on", "target": map[string]any{"entity_id": fmt.Sprintf("light.gone_%03d", i)}}}}
	}
	stored := map[string]any{
		"/api/config/automation/config/d1": map[string]any{"alias": "Disabled one", "triggers": []any{}, "actions": []any{map[string]any{"action": "notify.gone"}}},
	}
	return func(_ *http.Request, endpoint string, payload map[string]any) any {
		if fail == "null "+fmt.Sprint(payload["type"])+fmt.Sprint(payload["path"]) {
			if endpoint == "ws" {
				return nil
			}
			return coreReply(200, nil)
		}
		if endpoint == "ws" {
			if payload["type"] == fail {
				return fakeRelayFailure{"UPSTREAM_ERROR", "Relay timeout"}
			}
			switch payload["type"] {
			case "config/entity_registry/list":
				return registry
			case "config/device_registry/list":
				return []any{map[string]any{"id": "dev_ok"}, map[string]any{"id": "dev_off", "disabled_by": "user"}}
			case "config/device_registry/list_composite_splits":
				return map[string]any{"merged1": map[string]any{"split_ids": []any{"dev_ok"}}}
			case "config/area_registry/list":
				return []any{map[string]any{"area_id": "kitchen"}}
			case "automation/config", "script/config":
				if cfg, ok := loaded[fmt.Sprint(payload["entity_id"])]; ok {
					return map[string]any{"config": cfg}
				}
				return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "not_found: Entity not found"}
			}
			return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", fmt.Sprintf("not_found: unexpected %v", payload["type"])}
		}
		path := fmt.Sprint(payload["path"])
		switch {
		case path == "/api/states":
			return coreReply(200, states)
		case path == "/api/services":
			return coreReply(200, []any{
				map[string]any{"domain": "light", "services": map[string]any{"turn_on": map[string]any{}, "turn_off": map[string]any{}, "toggle": map[string]any{}}},
				map[string]any{"domain": "script", "services": map[string]any{"ok": map[string]any{}}},
				map[string]any{"domain": "scene", "services": map[string]any{"apply": map[string]any{}}},
				map[string]any{"domain": "tts", "services": map[string]any{"speak": map[string]any{}}},
				map[string]any{"domain": "notify", "services": map[string]any{"mobile_app_x": map[string]any{}}},
			})
		case stored[path] != nil:
			return coreReply(200, stored[path])
		}
		return coreReply(404, map[string]any{"message": "Resource not found"})
	}
}

func deadRefsOf(t *testing.T, args ...string) (int, deadRefResult, *fakeRelayLog, verbTestEnvelope) {
	t.Helper()
	return deadRefsFrom(t, deadRefReply(0, ""), args...)
}

func deadRefsFrom(t *testing.T, reply func(*http.Request, string, map[string]any) any, args ...string) (int, deadRefResult, *fakeRelayLog, verbTestEnvelope) {
	t.Helper()
	paths, log := setupVerbRelayTest(t, reply)
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, append([]string{"--dead-references"}, args...)) })
	var result deadRefResult
	if exit == 0 {
		if err := json.Unmarshal(env.Result, &result); err != nil {
			t.Fatalf("result %s: %v", env.Result, err)
		}
	}
	return exit, result, log, env
}

func TestDeadReferencesFindsEveryKindByPosition(t *testing.T) {
	exit, result, _, env := deadRefsOf(t)
	if exit != 0 {
		t.Fatalf("exit %d, env %+v", exit, env)
	}
	got := map[string]string{}
	for _, f := range result.Findings {
		got[f.Holder+" "+f.Kind+" "+f.Ref] = fmt.Sprintf("%s %s template=%v %s", f.Status, strings.Join(f.Paths, "|"), f.Template, f.EntityID)
	}
	want := map[string]string{
		"automation.broken entity sensor.gone":                      "missing actions[1].data.entity_id|triggers[0].entity_id template=false ",
		"automation.broken entity sensor.not_loaded":                "not_loaded actions[1].data.entity_id template=false ",
		"automation.broken entity sensor.gone2":                     "missing actions[1].data.message template=true ",
		"automation.broken entity sensor.flaky":                     "unavailable actions[1].data.message template=true ",
		"automation.broken entity light.off_disabled":               "disabled actions[0].target.entity_id template=false ",
		"automation.broken entity 0123456789abcdef0123456789abcdef": "disabled conditions[0].entity_id template=false light.off_disabled",
		"automation.broken entity scene.gone":                       "missing actions[2].scene template=false ",
		"automation.broken device dev_gone":                         "missing conditions[0].device_id template=false ",
		"automation.broken area attic":                              "missing actions[0].target.area_id template=false ",
		"automation.broken action hue.activate_scene":               "missing actions[1].action template=false ",
		"automation.disabled_one action notify.gone":                "missing actions[0].action template=false ",
		"automation.broken entity sensor.limit_gone":                "missing triggers[3].above template=false ",
		"automation.broken entity zone.gone":                        "missing triggers[4].zone template=false ",
		"automation.broken entity sensor.ev_gone":                   "missing triggers[5].event_data.entity_id template=false ",
		"automation.broken entity sensor.var_tpl_gone":              "missing variables.t template=true ",
		"automation.broken entity light.scene_gone":                 "missing actions[5].data.entities template=false ",
		"automation.broken entity media_player.gone":                "missing actions[6].data.media_player_entity_id template=false ",
		"automation.broken entity light.exp_gone":                   "missing actions[6].data.message template=true ",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings:\n got %v\nwant %v", got, want)
	}
	for _, f := range result.Findings {
		if f.Holder == "automation.broken" && f.Name != "Broken one" || f.Holder == "automation.disabled_one" && (f.Name != "Disabled one" || f.ConfigID != "d1") {
			t.Fatalf("holder naming: %+v", f)
		}
	}
	if result.Scanned["automation"] != 3 || result.Scanned["script"] != 1 {
		t.Fatalf("scanned %v: want 3 automations (broken, disabled, YAML) and 1 script", result.Scanned)
	}
	if len(result.NotScanned) != 2 || result.NotScanned[0].ID != "automation.empty" || result.NotScanned[1].ID != "automation.unloaded" {
		t.Fatalf("not_scanned %+v: the unreadable and the empty one are named, never clean", result.NotScanned)
	}
	if len(result.NotCovered) == 0 {
		t.Fatal("not_covered is empty")
	}
}

// 140 loaded automations come in 9 batches of at most 16, with the same
// findings as single reads on an older App, and nothing is read per
// reference.
func TestDeadReferencesReadsLoadedConfigsInBatches(t *testing.T) {
	sweep := func(batch bool) (deadRefResult, *fakeRelayLog) {
		if batch {
			withFakeBatches(t)
		}
		exit, result, log, env := deadRefsFrom(t, deadRefReply(140, ""))
		if exit != 0 {
			t.Fatalf("exit %d, env %+v", exit, env)
		}
		return result, log
	}
	single, singleLog := sweep(false)
	batched, batchLog := sweep(true)
	if singleLog.batches.Load() != 0 || batchLog.batches.Load() != 9 {
		t.Fatalf("batches: %d without ws_batch, %d with it; want 0 and 9", singleLog.batches.Load(), batchLog.batches.Load())
	}
	if !reflect.DeepEqual(single, batched) || len(batched.Findings) != 158 {
		t.Fatalf("batched sweep differs or is incomplete: %d vs %d findings", len(single.Findings), len(batched.Findings))
	}
	reads := map[string]int{}
	for _, c := range batchLog.all() {
		key := c.endpoint + " " + fmt.Sprint(c.payload["type"]) + fmt.Sprint(c.payload["path"])
		if c.endpoint == "ws" && (c.payload["type"] == "automation/config" || c.payload["type"] == "script/config") {
			key += " " + fmt.Sprint(c.payload["entity_id"])
		}
		reads[key]++
	}
	var repeated []string
	for key, n := range reads {
		if n > 1 {
			repeated = append(repeated, key)
		}
	}
	sort.Strings(repeated)
	if len(repeated) > 0 || len(reads) > 152 {
		t.Fatalf("%d distinct reads, repeated %v: want each list and config read once", len(reads), repeated)
	}
}

func TestDeadReferencesFailsWhenAListCantBeRead(t *testing.T) {
	for _, list := range []string{"config/entity_registry/list", "config/device_registry/list", "config/area_registry/list", "config/device_registry/list_composite_splits",
		"null config/entity_registry/list<nil>", "null config/device_registry/list<nil>", "null config/area_registry/list<nil>", "null config/device_registry/list_composite_splits<nil>",
		"null <nil>/api/states", "null <nil>/api/services"} {
		if exit, _, _, env := deadRefsFrom(t, deadRefReply(0, list)); exit == 0 {
			t.Fatalf("%s unread: exit 0 with %s; a reference checked against an unread list would look missing", list, env.Result)
		}
	}
}

func TestDeadReferencesTakesNoTarget(t *testing.T) {
	for _, args := range [][]string{{"light.ok"}, {"--references"}, {"--dashboards"}, {"--config"}} {
		if exit, _, _, _ := deadRefsOf(t, args...); exit == 0 {
			t.Fatalf("--dead-references %v: want a usage error", args)
		}
	}
}
