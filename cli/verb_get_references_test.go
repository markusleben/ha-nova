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

func TestMatchNeedlesMatchesWholeIDsOnly(t *testing.T) {
	needles := []string{"sensor.temp", "mobile_app_pixel_7", "a1b2"}
	for s, want := range map[string][]string{
		"sensor.temp":                                 {"sensor.temp"},
		"{{ states('sensor.temp') | float }}":         {"sensor.temp"},
		"{{ states.sensor.temp.state }}":              {"sensor.temp"},
		"sensor.temp_2":                               nil,
		"binary_sensor.temp":                          nil,
		"sensor.temperature":                          nil,
		"binary_sensor.temp, sensor.temp":             {"sensor.temp"}, // a rejected first occurrence never hides a later one
		"notify.mobile_app_pixel_7":                   {"mobile_app_pixel_7"},
		"notify.mobile_app_pixel_7_pro":               nil,
		"device a1b2":                                 {"a1b2"},
		"xa1b2":                                       nil,
		"sensor.temp sensor.temp mobile_app_pixel_7.": {"sensor.temp", "mobile_app_pixel_7"},
	} {
		if got := matchNeedles(s, needles); !reflect.DeepEqual(got, want) {
			t.Errorf("matchNeedles(%q) = %v, want %v", s, got, want)
		}
	}
}

// refHome is a fake home: an old phone (device dev1, mobile_app) with a
// battery sensor and a tracker, consumed by every holder family.
func refHome(t *testing.T, extra func(endpoint string, payload map[string]any) (any, bool)) func(*http.Request, string, map[string]any) any {
	registry := []map[string]any{
		{"id": "reg1battery00000000000000000000aa", "entity_id": "sensor.pixel_7_battery", "platform": "mobile_app", "device_id": "dev1", "config_entry_id": "ce_mobile", "unique_id": "u1", "original_name": "Battery level", "translation_key": nil},
		{"id": "reg2tracker00000000000000000000bb", "entity_id": "device_tracker.pixel_7", "platform": "mobile_app", "device_id": "dev1", "config_entry_id": "ce_mobile", "unique_id": "u2", "original_name": "Pixel 7"},
		{"id": "reg3other0000000000000000000000cc", "entity_id": "sensor.pixel_7_battery_2", "platform": "mqtt", "device_id": "dev2", "unique_id": "u3"},
		{"id": "r4", "entity_id": "automation.low_battery", "platform": "automation", "unique_id": "100"},
		{"id": "r5", "entity_id": "automation.arrive", "platform": "automation", "unique_id": "200", "disabled_by": "user"},
		{"id": "r6", "entity_id": "script.package_script", "platform": "script", "unique_id": "package_script"},
		// A helper linked to the phone's device by its own config entry, and
		// a group helper that is unavailable (attributes hidden).
		{"id": "r9", "entity_id": "sensor.pixel_7_battery_meter", "platform": "utility_meter", "device_id": "dev1", "config_entry_id": "ce_meter", "unique_id": "u9"},
		{"id": "r10", "entity_id": "light.dead_group", "platform": "group", "config_entry_id": "ce_group", "unique_id": "u10"},
		{"id": "r7", "entity_id": "automation.bp_battery", "platform": "automation", "unique_id": "300"},
		{"id": "r8", "entity_id": "automation.bp_missing", "platform": "automation", "unique_id": "400"},
	}
	states := []map[string]any{
		{"entity_id": "sensor.pixel_7_battery", "state": "80", "attributes": map[string]any{"device_class": "battery", "unit_of_measurement": "%"}},
		{"entity_id": "device_tracker.pixel_7", "state": "home", "attributes": map[string]any{"source_type": "gps"}},
		{"entity_id": "sensor.pixel_7_battery_2", "state": "1", "attributes": map[string]any{}},
		{"entity_id": "automation.low_battery", "state": "on", "attributes": map[string]any{"id": "100"}},
		{"entity_id": "automation.yaml_without_id", "state": "on", "attributes": map[string]any{}},
		{"entity_id": "script.package_script", "state": "off", "attributes": map[string]any{}},
		{"entity_id": "sensor.pixel_7_battery_meter", "state": "3", "attributes": map[string]any{"source": "sensor.pixel_7_battery"}},
		{"entity_id": "light.dead_group", "state": "unavailable", "attributes": map[string]any{"friendly_name": "Dead group"}},
		{"entity_id": "person.markus", "state": "home", "attributes": map[string]any{"device_trackers": []any{"device_tracker.pixel_7"}}},
		{"entity_id": "sensor.min_battery", "state": "80", "attributes": map[string]any{"entity_id": []any{"sensor.pixel_7_battery", "sensor.pixel_7_battery_2"}}},
	}
	configs := map[string]any{
		"/api/config/automation/config/100": map[string]any{"alias": "Low battery",
			"triggers":   []any{map[string]any{"trigger": "numeric_state", "entity_id": "sensor.pixel_7_battery", "below": 20}},
			"conditions": []any{map[string]any{"condition": "template", "value_template": "{{ states('sensor.pixel_7_battery_2') | int < 5 }}"}},
			"actions": []any{map[string]any{"action": "notify.mobile_app_pixel_7", "data": map[string]any{"message": "{{ states('sensor.pixel_7_battery') }} %"}},
				map[string]any{"action": "scene.apply", "data": map[string]any{"entities": map[string]any{"sensor.pixel_7_battery": "on"}}}}},
		"/api/config/automation/config/200": map[string]any{"alias": "Arrive",
			"triggers": []any{map[string]any{"trigger": "device", "device_id": "dev1", "entity_id": "reg2tracker00000000000000000000bb", "domain": "device_tracker", "type": "enters", "zone": "zone.home"}},
			"actions": []any{map[string]any{"action": "lock.unlock", "target": map[string]any{"entity_id": "lock.front"}},
				map[string]any{"action": "light.turn_on", "target": map[string]any{"device_id": "merged1"}}}}, // a pre-2026.9 merged id
	}
	// A blueprint whose body names the battery by default and hard-codes the
	// tracker its instance also passes as an input. A second instance's
	// blueprint is gone.
	configs["/api/config/automation/config/300"] = map[string]any{"alias": "Battery blueprint",
		"use_blueprint": map[string]any{"path": "battery.yaml", "input": map[string]any{"tracker": "device_tracker.pixel_7"}}}
	configs["/api/config/automation/config/400"] = map[string]any{"alias": "Missing blueprint",
		"use_blueprint": map[string]any{"path": "missing.yaml"}}
	return func(_ *http.Request, endpoint string, payload map[string]any) any {
		if extra != nil {
			if reply, ok := extra(endpoint, payload); ok {
				return reply
			}
		}
		if endpoint == "ws" {
			switch payload["type"] {
			case "config/entity_registry/list":
				return registry
			case "config/entity_registry/get":
				for _, row := range registry {
					if row["entity_id"] == payload["entity_id"] {
						return row
					}
				}
				return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "not_found: Entity not found"}
			case "config/device_registry/list":
				return []any{map[string]any{"id": "dev1", "name": "Pixel 7", "name_by_user": "Markus phone", "manufacturer": "Google", "model": "Pixel 7", "config_entries": []any{"ce_mobile", "ce_meter"}, "primary_config_entry": "ce_mobile"}}
			case "config/device_registry/list_composite_splits":
				return map[string]any{"merged1": map[string]any{"split_ids": []any{"devX", "dev1"}}, "merged2": map[string]any{"split_ids": []any{"dev9"}}}
			case "script/config":
				if payload["entity_id"] == "script.package_script" {
					return map[string]any{"config": map[string]any{"sequence": []any{map[string]any{"condition": "state", "entity_id": "device_tracker.pixel_7", "state": "home"}}}}
				}
			case "automation/config":
				if payload["entity_id"] == "automation.yaml_without_id" {
					return fakeRelayFailure{"UPSTREAM_ERROR", "Relay timeout"}
				}
			case "blueprint/substitute":
				if payload["path"] == "battery.yaml" {
					input, _ := payload["input"].(map[string]any)
					return map[string]any{"substituted_config": map[string]any{
						"triggers":   []any{map[string]any{"trigger": "numeric_state", "entity_id": "sensor.pixel_7_battery", "below": 10}},
						"conditions": []any{map[string]any{"condition": "state", "entity_id": input["tracker"], "state": "home"}},
						"variables":  map[string]any{"owner": "device_tracker.pixel_7"}}}
				}
				return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "home_assistant_error: Failed to load blueprint"}
			case "get_panels":
				return map[string]any{
					"lovelace":       map[string]any{"component_name": "lovelace", "url_path": "lovelace", "config": map[string]any{"mode": "storage"}},
					"dashboard-yaml": map[string]any{"component_name": "lovelace", "url_path": "dashboard-yaml", "config": map[string]any{"mode": "yaml"}},
					"dashboard-auto": map[string]any{"component_name": "lovelace", "url_path": "dashboard-auto", "config": map[string]any{"mode": "storage"}},
					"map":            map[string]any{"component_name": "map", "url_path": "map"},
				}
			case "lovelace/config":
				switch payload["url_path"] {
				case nil:
					return map[string]any{"views": []any{map[string]any{"cards": []any{map[string]any{"type": "tile", "entity": "device_tracker.pixel_7"}}}}}
				case "dashboard-yaml":
					return map[string]any{"views": []any{map[string]any{"cards": []any{map[string]any{"type": "entities", "entities": []any{"sensor.pixel_7_battery"}}}}}}
				case "dashboard-auto":
					return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "config_not_found: No config found."}
				}
			case "energy/get_prefs":
				return map[string]any{"device_consumption": []any{map[string]any{"stat_consumption": "sensor.pixel_7_battery"}}}
			case "search/related":
				return map[string]any{"automation": []string{"automation.low_battery"}, "person": []string{"person.markus"}}
			}
			return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", fmt.Sprintf("not_found: unexpected %v", payload["type"])}
		}
		path := fmt.Sprint(payload["path"])
		switch {
		case path == "/api/states":
			return coreReply(200, states)
		case strings.HasPrefix(path, "/api/states/"):
			for _, st := range states {
				if "/api/states/"+fmt.Sprint(st["entity_id"]) == path {
					return coreReply(200, st)
				}
			}
			return coreReply(404, map[string]any{"message": "Entity not found."})
		case path == "/api/services":
			return coreReply(200, []any{map[string]any{"domain": "notify", "services": map[string]any{"mobile_app_pixel_7": map[string]any{}, "mobile_app_pixel_7_pro": map[string]any{}}}})
		case configs[path] != nil:
			return coreReply(200, configs[path])
		}
		return coreReply(404, map[string]any{"message": "Resource not found"})
	}
}

func referencesOf(t *testing.T, env verbTestEnvelope) (getResult, map[string]refHit) {
	t.Helper()
	var result getResult
	if err := json.Unmarshal(env.Result, &result); err != nil || result.References == nil {
		t.Fatalf("no references in %s (%v)", env.Result, err)
	}
	hits := map[string]refHit{}
	for _, h := range result.References.Hits {
		key := h.Family + " " + h.ID + " " + h.Needle
		if h.Managed == "blueprint" {
			key += " (blueprint)"
		}
		hits[key] = h
	}
	return result, hits
}

func TestGetReferencesScansEveryHolderForTheWholeDevice(t *testing.T) {
	paths, log := setupVerbRelayTest(t, refHome(t, nil))
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"sensor.pixel_7_battery", "--references"}) })
	if exit != 0 {
		t.Fatalf("exit %d, env %+v", exit, env)
	}
	result, hits := referencesOf(t, env)
	refs := result.References
	if refs.Device == nil || refs.Device.ID != "dev1" || strings.Join(refs.Device.LegacyIDs, ",") != "merged1" || refs.Device.Name != "Markus phone" || refs.NotifyService != "notify.mobile_app_pixel_7" {
		t.Fatalf("device/notify: %+v %q", refs.Device, refs.NotifyService)
	}
	if len(refs.Entities) != 2 || refs.Entities[1].EntityID != "sensor.pixel_7_battery" || refs.Entities[1].DeviceClass != "battery" || refs.Entities[1].Unit != "%" {
		t.Fatalf("entities: %+v", refs.Entities)
	}
	want := map[string][]string{
		"automation automation.low_battery sensor.pixel_7_battery":            {"actions[0].data.message", `actions[1].data.entities["sensor.pixel_7_battery"]`, "triggers[0].entity_id"},
		"automation automation.low_battery mobile_app_pixel_7":                {"actions[0].action"},
		"automation automation.arrive dev1":                                   {"triggers[0].device_id"},
		"automation automation.arrive reg2tracker00000000000000000000bb":      {"triggers[0].entity_id"},
		"script script.package_script device_tracker.pixel_7":                 {"sequence[0].entity_id"},
		"dashboard lovelace device_tracker.pixel_7":                           {"views[0].cards[0].entity"},
		"dashboard dashboard-yaml sensor.pixel_7_battery":                     {"views[0].cards[0].entities[0]"},
		"energy energy sensor.pixel_7_battery":                                {"device_consumption[0].stat_consumption"},
		"entity person.markus device_tracker.pixel_7":                         {"attributes.device_trackers[0]"},
		"entity sensor.min_battery sensor.pixel_7_battery":                    {"attributes.entity_id[0]"},
		"automation automation.bp_battery device_tracker.pixel_7":             {"use_blueprint.input.tracker"},
		"automation automation.bp_battery sensor.pixel_7_battery (blueprint)": {"triggers[0].entity_id"},
		"automation automation.bp_battery device_tracker.pixel_7 (blueprint)": {"variables.owner"},
		"automation automation.arrive merged1":                                {"actions[1].target.device_id"},
		"entity sensor.pixel_7_battery_meter sensor.pixel_7_battery":          {"attributes.source"},
	}
	for key, wantPaths := range want {
		h, ok := hits[key]
		if !ok {
			t.Errorf("missing hit %s in %+v", key, refs.Hits)
			continue
		}
		got := append([]string(nil), h.Paths...)
		if strings.Join(sortedCopy(got), ",") != strings.Join(sortedCopy(wantPaths), ",") {
			t.Errorf("%s paths = %v, want %v", key, h.Paths, wantPaths)
		}
	}
	if len(hits) != len(want) {
		t.Errorf("hits = %d, want %d (no sibling-device or boundary false positives): %+v", len(hits), len(want), refs.Hits)
	}
	if h := hits["automation automation.low_battery sensor.pixel_7_battery"]; !h.Template || h.ConfigID != "100" {
		t.Errorf("template flag / config id: %+v", h)
	}
	if h := hits["script script.package_script device_tracker.pixel_7"]; h.Managed != "yaml" {
		t.Errorf("package script must be managed yaml: %+v", h)
	}
	if h := hits["dashboard dashboard-yaml sensor.pixel_7_battery"]; h.Managed != "yaml" {
		t.Errorf("yaml dashboard must be managed yaml: %+v", h)
	}
	// The blueprint body's own reference is flagged: the blueprint, not the
	// instance, holds it; the instance's input stays an ordinary hit.
	// A merged id reaches its other split device too: a re-point keeps it.
	if h := hits["automation automation.arrive merged1"]; strings.Join(h.Keep, ",") != "devX" {
		t.Errorf("merged id hit must keep its other split devices: %+v", h)
	}
	if h := hits["automation automation.arrive dev1"]; len(h.Keep) != 0 {
		t.Errorf("the device's own id keeps nothing: %+v", h)
	}
	if h := hits["automation automation.bp_battery sensor.pixel_7_battery (blueprint)"]; h.Managed != "blueprint" {
		t.Errorf("blueprint-owned hit: %+v", h)
	}
	if h := hits["automation automation.bp_battery device_tracker.pixel_7"]; h.Managed != "" {
		t.Errorf("instance input hit: %+v", h)
	}
	// Failed reads and expansions are named, never a silent zero; the
	// auto-generated dashboard counts as scanned.
	gaps := map[string]bool{}
	for _, g := range refs.NotScanned {
		gaps[g.ID] = true
	}
	if len(refs.NotScanned) != 3 || !gaps["automation.yaml_without_id"] || !gaps["automation.bp_missing"] || !gaps["light.dead_group"] {
		t.Errorf("not_scanned = %+v", refs.NotScanned)
	}
	if refs.Scanned["automation"] != 4 || refs.Scanned["script"] != 1 || refs.Scanned["dashboard"] != 3 || refs.Scanned["energy"] != 1 {
		t.Errorf("scanned = %+v", refs.Scanned)
	}
	if len(result.Related) == 0 || len(refs.NotCovered) == 0 {
		t.Errorf("--references includes search/related and the not-covered list: %+v", result)
	}
	for _, c := range log.all() { // the default dashboard is addressed without url_path
		if c.payload["type"] == "lovelace/config" && c.payload["url_path"] == "lovelace" {
			t.Errorf("default dashboard read with url_path: %v", c.payload)
		}
	}
}

func TestGetReferencesScansARemovedEntityByItsID(t *testing.T) {
	paths, _ := setupVerbRelayTest(t, refHome(t, func(endpoint string, payload map[string]any) (any, bool) {
		// The removed sensor still sits in a dashboard card.
		if endpoint == "ws" && payload["type"] == "lovelace/config" && payload["url_path"] == nil {
			return map[string]any{"views": []any{map[string]any{"cards": []any{map[string]any{"entity": "sensor.old_kitchen_temp"}}}}}, true
		}
		return nil, false
	}))
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"sensor.old_kitchen_temp", "--references"}) })
	if exit != 0 || env.Resolved[0].By != "absent" {
		t.Fatalf("exit %d, env %+v", exit, env)
	}
	result, hits := referencesOf(t, env)
	if result.State != nil && string(result.State) != "null" || !strings.Contains(result.Note, "does not exist") || result.References.Device != nil {
		t.Fatalf("result %+v", result)
	}
	if _, ok := hits["dashboard lovelace sensor.old_kitchen_temp"]; !ok || len(hits) != 1 {
		t.Fatalf("hits %+v", result.References.Hits)
	}
	// Without --references a missing entity stays exit 5.
	if exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"sensor.old_kitchen_temp", "--related"}) }); exit != verbExitNotFound || env.Error.Code != "NOT_FOUND" {
		t.Fatalf("--related on a missing entity: exit %d env %+v", exit, env)
	}
}

func TestGetReferencesNamesAnUnresolvedNotifyService(t *testing.T) {
	paths, _ := setupVerbRelayTest(t, refHome(t, func(endpoint string, payload map[string]any) (any, bool) {
		if endpoint == "core" && payload["path"] == "/api/services" {
			return coreReply(200, []any{map[string]any{"domain": "notify", "services": map[string]any{"mobile_app_pixel_7_pro": map[string]any{}}}}), true
		}
		return nil, false
	}))
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"device_tracker.pixel_7", "--references"}) })
	result, _ := referencesOf(t, env)
	refs := result.References
	if exit != 0 || refs.NotifyService != "" {
		t.Fatalf("exit %d, notify %q", exit, refs.NotifyService)
	}
	named := false
	for _, g := range refs.NotScanned {
		named = named || (g.Family == "notify" && strings.Contains(g.Reason, "notify.mobile_app_pixel_7 "))
	}
	if !named {
		t.Fatalf("an unresolved notify service must be named: %+v", refs.NotScanned)
	}
}

func TestGetReferencesFailedHolderFamiliesAreNotScanned(t *testing.T) {
	paths, _ := setupVerbRelayTest(t, refHome(t, func(endpoint string, payload map[string]any) (any, bool) {
		if endpoint == "ws" && (payload["type"] == "get_panels" || payload["type"] == "energy/get_prefs") {
			return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "unauthorized: Unauthorized"}, true
		}
		return nil, false
	}))
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"sensor.pixel_7_battery", "--references"}) })
	result, _ := referencesOf(t, env)
	families := map[string]bool{}
	for _, g := range result.References.NotScanned {
		families[g.Family] = true
	}
	if exit != 0 || !families["dashboard"] || !families["energy"] || result.References.Scanned["dashboard"] != 0 {
		t.Fatalf("exit %d, not_scanned %+v, scanned %+v", exit, result.References.NotScanned, result.References.Scanned)
	}
}

func TestJoinRefPathQuotesNonIdentifierKeys(t *testing.T) {
	if got := joinRefPath("entities", "light.kitchen"); got != `entities["light.kitchen"]` {
		t.Fatalf("got %s", got)
	}
	if got := joinRefPath("", "triggers"); got != "triggers" {
		t.Fatalf("got %s", got)
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestGetReferencesKeepsTheScanWhenACrossCheckFails(t *testing.T) {
	paths, _ := setupVerbRelayTest(t, refHome(t, func(endpoint string, payload map[string]any) (any, bool) {
		switch {
		case endpoint == "ws" && payload["type"] == "search/related":
			return fakeRelayFailure{"UPSTREAM_ERROR", "Relay timeout"}, true
		case endpoint == "ws" && payload["type"] == "lovelace/config" && payload["url_path"] == "dashboard-yaml":
			// a YAML dashboard whose file is missing is a gap, never "auto-generated"
			return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "config_not_found: No config found."}, true
		}
		return nil, false
	}))
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"sensor.pixel_7_battery", "--references"}) })
	result, hits := referencesOf(t, env)
	gaps := map[string]bool{}
	for _, g := range result.References.NotScanned {
		gaps[g.Family+" "+g.ID] = true
	}
	if exit != 0 || !gaps["related "] || !gaps["dashboard dashboard-yaml"] || len(hits) == 0 {
		t.Fatalf("exit %d, not_scanned %+v", exit, result.References.NotScanned)
	}
	// Plain --related still fails loud.
	if exit, _ := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"sensor.pixel_7_battery", "--related"}) }); exit != verbExitError {
		t.Fatalf("--related with a failed search: exit %d", exit)
	}
}

func TestRefScannerMaskKeepsNoNeedle(t *testing.T) {
	sc := &refScanner{needles: []string{"sensor.a"}}
	got := sc.mask(map[string]any{"x": []any{"sensor.a sensor.a,{{ states('sensor.a') }}", "sensor.a_2"}})
	raw, _ := json.Marshal(got)
	if len(sc.find(got)) != 0 || !strings.Contains(string(raw), "sensor.a_2") {
		t.Fatalf("mask: %s", raw)
	}
}
