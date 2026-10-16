package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func ago(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339Nano) }

func st(id, state string, attrs map[string]any) map[string]any {
	if attrs == nil {
		attrs = map[string]any{}
	}
	return map[string]any{"entity_id": id, "state": state, "last_changed": ago(time.Hour), "attributes": attrs}
}

type mapFixture struct {
	floors, areas, devices, entities, entries, helpers, states []map[string]any
	related                                                    map[string]map[string][]string
}

func (f mapFixture) reply(_ *http.Request, endpoint string, payload map[string]any) any {
	if endpoint == "core" && payload["path"] == "/api/states" {
		return coreReply(200, f.states)
	}
	switch payload["type"] {
	case "config/floor_registry/list":
		return f.floors
	case "config/area_registry/list":
		return f.areas
	case "config/device_registry/list":
		return f.devices
	case "config/entity_registry/list":
		return f.entities
	case "config_entries/get":
		if payload["type_filter"] != nil {
			return f.helpers
		}
		return f.entries
	case "search/related":
		return f.related[fmt.Sprint(payload["item_type"], ":", payload["item_id"])]
	}
	return nil
}

func ent(id, device, platform, entry string, extra map[string]any) map[string]any {
	row := map[string]any{"entity_id": id, "device_id": device, "platform": platform, "config_entry_id": entry, "name": nil, "area_id": nil, "disabled_by": nil}
	for k, v := range extra {
		row[k] = v
	}
	return row
}

func smallHome() mapFixture {
	hub := st("sensor.hub_status", "unavailable", nil)
	hub["last_changed"] = ago(9 * 24 * time.Hour)
	return mapFixture{
		floors: []map[string]any{{"floor_id": "upper", "name": "Upper", "level": 1}, {"floor_id": "ground", "name": "Ground", "level": 0}},
		areas: []map[string]any{{"area_id": "kitchen", "name": "Kitchen", "floor_id": "ground"}, {"area_id": "living", "name": "Living", "floor_id": "ground"},
			{"area_id": "bedroom", "name": "Bedroom", "floor_id": "upper"}, {"area_id": "garage", "name": "Garage", "floor_id": nil}},
		devices: []map[string]any{
			{"id": "d_bridge", "area_id": nil, "config_entry_id": "e_hue"},
			{"id": "d_bulb", "area_id": "kitchen", "config_entry_id": "e_hue", "name": "Bulb", "name_by_user": "Kitchen bulb"},
			{"id": "d_child", "area_id": nil, "parent_device_id": "d_bulb", "config_entry_id": "e_hue"},
			{"id": "d_sensor", "area_id": "living", "config_entries": []string{"e_zha"}},
			{"id": "d_off", "area_id": "bedroom", "disabled_by": "user", "config_entry_id": "e_zha"},
			{"id": "d_phone", "area_id": nil, "config_entry_id": "e_mobile"},
			{"id": "d_service", "area_id": nil, "config_entry_id": "e_bad", "entry_type": "service"},
		},
		entities: []map[string]any{
			ent("light.kitchen", "d_bulb", "hue", "e_hue", nil),
			ent("light.kitchen_child", "d_child", "hue", "e_hue", nil),
			ent("sensor.door_battery", "d_sensor", "zha", "e_zha", map[string]any{"entity_category": "diagnostic"}),
			ent("sensor.window_battery", "d_sensor", "zha", "e_zha", map[string]any{"entity_category": "diagnostic"}),
			ent("binary_sensor.remote_battery", "", "zha", "e_zha", map[string]any{"area_id": "bedroom", "entity_category": "diagnostic"}),
			ent("sensor.car_battery", "", "tesla", "", nil),
			ent("light.kitchen_status", "d_bulb", "hue", "e_hue", nil),
			ent("automation.ghost", "", "automation", "", nil),
			ent("sensor.met_status", "d_service", "met", "e_bad", nil),
			ent("sensor.phone_battery", "d_phone", "mobile_app", "e_mobile", nil),
			ent("sensor.temp", "d_sensor", "zha", "e_zha", nil),
			ent("sensor.hub_status", "d_bridge", "hue", "e_hue", nil),
			ent("automation.morning", "", "automation", "", nil),
			ent("automation.broken", "", "automation", "", nil),
			ent("automation.never", "", "automation", "", nil),
			ent("automation.old", "", "automation", "", nil),
			ent("automation.gone", "", "automation", "", map[string]any{"disabled_by": "user", "name": "Gone"}),
			ent("script.a", "", "script", "", nil),
			ent("scene.movie", "", "homeassistant", "", nil),
			ent("button.reset", "", "demo", "", nil),
			ent("input_boolean.guest", "", "input_boolean", "", nil),
			ent("sensor.template_x", "", "template", "e_tmpl", nil),
			ent("sensor.disabled", "d_sensor", "zha", "e_zha", map[string]any{"disabled_by": "integration"}),
			ent("light.group_all", "", "group", "e_group", nil),
		},
		entries: []map[string]any{
			{"entry_id": "e_hue", "domain": "hue", "source": "user", "state": "loaded", "title": "owner@example.com"},
			{"entry_id": "e_zha", "domain": "zha", "source": "user", "state": "loaded"},
			{"entry_id": "e_mobile", "domain": "mobile_app", "source": "user", "state": "loaded"},
			{"entry_id": "e_bad", "domain": "met", "source": "user", "state": "setup_retry"},
			{"entry_id": "e_off", "domain": "cast", "source": "user", "state": "not_loaded", "disabled_by": "user"},
			{"entry_id": "e_ign", "domain": "dlna_dmr", "source": "ignore", "state": "not_loaded"},
			{"entry_id": "e_tmpl", "domain": "template", "source": "user", "state": "loaded"},
			{"entry_id": "e_group", "domain": "group", "source": "user", "state": "loaded"},
		},
		helpers: []map[string]any{{"entry_id": "e_tmpl", "domain": "template", "source": "user"}, {"entry_id": "e_group", "domain": "group", "source": "user"}},
		states: []map[string]any{
			st("light.kitchen", "on", map[string]any{"friendly_name": "Kitchen light"}),
			st("light.kitchen_child", "off", map[string]any{"friendly_name": "Kitchen spot"}),
			st("sensor.door_battery", "7", map[string]any{"device_class": "battery", "friendly_name": "Door battery", "secret_attr": "do-not-leak"}),
			st("sensor.window_battery", "15", map[string]any{"device_class": "battery", "friendly_name": "Window battery"}),
			st("binary_sensor.remote_battery", "on", map[string]any{"device_class": "battery", "friendly_name": "Remote battery"}),
			st("sensor.phone_battery", "5", map[string]any{"device_class": "battery", "friendly_name": "Phone battery"}),
			st("sensor.car_battery", "10", map[string]any{"device_class": "battery", "friendly_name": "Car charge"}),
			st("light.kitchen_status", "on", map[string]any{"friendly_name": "Kitchen status light"}),
			st("automation.ghost", "unavailable", map[string]any{"friendly_name": "Ghost", "restored": true}),
			st("sensor.met_status", "ok", nil),
			st("device_tracker.someone_phone", "unknown", map[string]any{"friendly_name": "Someone phone"}),
			st("sensor.temp", "21.5", map[string]any{"device_class": "temperature", "friendly_name": "Living temperature"}),
			hub,
			st("automation.morning", "on", map[string]any{"friendly_name": "Morning", "last_triggered": ago(time.Hour)}),
			st("automation.broken", "unavailable", map[string]any{"friendly_name": "Broken"}),
			st("automation.never", "on", map[string]any{"friendly_name": "Never", "last_triggered": nil}),
			st("automation.old", "off", map[string]any{"friendly_name": "Old", "last_triggered": ago(48 * time.Hour)}),
			st("script.a", "off", map[string]any{"friendly_name": "Script A"}),
			st("scene.movie", "unknown", map[string]any{"friendly_name": "Movie", "entity_id": []string{"light.kitchen"}}),
			st("button.reset", "unknown", nil),
			st("input_boolean.guest", "off", nil),
			st("sensor.template_x", "3", nil),
			st("light.group_all", "on", map[string]any{"friendly_name": "All lights", "entity_id": []string{"light.kitchen", "light.kitchen_child"}}),
			st("sensor.yaml_unknown", "unknown", map[string]any{"friendly_name": "YAML sensor"}),
			st("person.someone", "home", map[string]any{"friendly_name": "Someone", "latitude": 52.1}),
		},
		related: map[string]map[string][]string{
			"entity:light.kitchen": {"automation": {"automation.morning"}, "scene": {"scene.movie"}, "device": {"d_bulb"}},
			// a device read also returns what uses the device's other entities
			"device:d_bulb":               {"automation": {"automation.never", "automation.morning", "automation.old", "automation.child_only"}, "script": {"script.a"}, "scene": {"scene.movie"}, "entity": {"light.kitchen", "light.kitchen_status"}},
			"entity:light.kitchen_status": {"automation": {"automation.old"}},
			// a device read includes its child devices and their entities
			"entity:light.kitchen_child": {"automation": {"automation.child_only"}},
		},
	}
}

func runMap(t *testing.T, f mapFixture, args ...string) (int, verbTestEnvelope, *fakeRelayLog) {
	t.Helper()
	paths, log := setupVerbRelayTest(t, f.reply)
	exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, args) })
	return exit, env, log
}

func decodeInto[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func TestMapSummaryCountsExactly(t *testing.T) {
	exit, env, log := runMap(t, smallHome())
	if exit != 0 || env.Status != "ok" {
		t.Fatalf("exit %d env %+v", exit, env.Error)
	}
	s := decodeInto[mapSummary](t, env.Result)
	// d_service (entry_type service) never counts as a device without area
	want := map[string]int{"floors": 2, "areas": 4, "devices": 6, "devices_disabled": 1, "devices_without_area": 2,
		"entities": 25, "entities_disabled": 2, "unregistered": 3, "device_entities_without_area": 2}
	for k, v := range want {
		if s.Counts[k] != v {
			t.Errorf("counts[%s] = %d, want %d (all %v)", k, s.Counts[k], v, s.Counts)
		}
	}
	if s.Domains["light"] != 4 || s.Domains["automation"] != 5 || s.Domains["person"] != 1 {
		t.Errorf("domains %v", s.Domains)
	}
	areas := map[string]areaSummary{}
	for _, a := range s.Areas.Rows {
		areas[a.AreaID] = a
	}
	// d_child inherits the kitchen from its parent; the disabled bedroom
	// device does not count; the remote counts through its own area.
	if a := areas["kitchen"]; a.Devices != 2 || a.Entities != 3 {
		t.Errorf("kitchen %+v", a)
	}
	if a := areas["living"]; a.Devices != 1 || a.Entities != 3 {
		t.Errorf("living %+v", a)
	}
	if a := areas["bedroom"]; a.Devices != 0 || a.Entities != 1 {
		t.Errorf("bedroom %+v", a)
	}
	if s.Areas.Total != 4 || s.Areas.Shown != 4 || s.Areas.Rows[0].AreaID != "kitchen" || s.Areas.Rows[1].AreaID != "living" { // tie: by name
		t.Errorf("areas %+v", s.Areas)
	}
	if s.Floors[0]["floor_id"] != "ground" || s.Floors[0]["areas"] != float64(2) {
		t.Errorf("floors by level: %v", s.Floors)
	}
	auto := s.Automations
	for k, v := range map[string]float64{"total": 6, "on": 2, "off": 1, "unavailable": 1, "restored": 1, "disabled": 1, "never_triggered": 1} {
		if auto[k] != v {
			t.Errorf("automations[%s] = %v, want %v", k, auto[k], v)
		}
	}
	if s.Scripts["total"] != 1 || s.Scenes["total"] != 1 {
		t.Errorf("scripts %v scenes %v", s.Scripts, s.Scenes)
	}
	if s.Helpers["total"] != float64(3) {
		t.Errorf("helpers %v", s.Helpers)
	}
	integrations := s.Integrations
	if integrations["entries"] != float64(5) || integrations["domains"] != float64(5) || integrations["disabled"] != float64(1) {
		t.Errorf("integrations %v", integrations)
	}
	problems := fmt.Sprint(integrations["problems"])
	if !strings.Contains(problems, "met") || !strings.Contains(problems, "setup_retry") || strings.Contains(problems, "cast") {
		t.Errorf("problems %s", problems)
	}
	rows := fmt.Sprint(integrations["rows"])
	if !strings.Contains(rows, "map[devices:3 domain:hue entities:4 entries:1]") {
		t.Errorf("hue row: %s", rows)
	}
	u := s.Unavailable
	// the device tracker's unknown is presence, never an availability row
	if u["total"] != float64(4) || u["unavailable"] != float64(3) || u["unknown"] != float64(1) || u["at_least_7d"] != float64(1) || u["restored"] != float64(1) {
		t.Errorf("unavailable %v", u)
	}
	if !strings.HasPrefix(fmt.Sprint(u["examples"]), "[map[age:9d entity_id:sensor.hub_status") {
		t.Errorf("oldest first: %v", u["examples"])
	}
	b := s.Batteries
	if b["total"] != float64(5) || b["low"] != float64(3) || b["phones_low"] != float64(1) || b["levels_low"] != float64(1) {
		t.Errorf("batteries %v", b)
	}
	if ex := fmt.Sprint(b["examples"]); strings.Contains(ex, "phone") || strings.Contains(ex, "car") || !strings.Contains(ex, "level:low") {
		t.Errorf("battery examples %s", ex)
	}
	raw := string(env.Result)
	for _, leak := range []string{"owner@example.com", "do-not-leak", "52.1", "latitude", "person.", "device_tracker."} {
		if strings.Contains(raw, leak) {
			t.Errorf("output leaks %q", leak)
		}
	}
	if strings.Join(s.More, ",") != "ha-nova map --section unavailable" { // 4 rows, 3 examples
		t.Errorf("more = %v", s.More)
	}
	for _, path := range log.corePaths() {
		if path != "/api/states" {
			t.Errorf("unexpected core read %s", path)
		}
	}
}

func TestMapFirstLookContract(t *testing.T) {
	exit, env, _ := runMap(t, smallHome(), "--first-look")
	if exit != 0 {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
	r := decodeInto[firstLookResult](t, env.Result)
	if r.Home["areas"] != 4 || r.Home["devices"] != 6 || r.Home["automations"] != 6 || r.Home["integrations"] != 5 {
		t.Errorf("home %v", r.Home)
	}
	if len(r.Observations) != 2 || r.Observations[0].Kind != "battery_low" || r.Observations[0].EntityID != "binary_sensor.remote_battery" ||
		r.Observations[0].Count != 3 || r.Observations[1].Kind != "automation_unavailable" || r.Observations[1].EntityID != "automation.broken" {
		t.Errorf("observations %+v", r.Observations)
	}
	if r.Seeds["area"].AreaID != "kitchen" || r.Seeds["light"].EntityID != "light.kitchen" || r.Seeds["climate"].EntityID != "sensor.temp" ||
		r.Seeds["automation"].EntityID != "automation.morning" {
		t.Errorf("seeds %+v", r.Seeds)
	}
	if _, ok := r.Seeds["cover"]; ok {
		t.Error("cover seed without a cover")
	}
	if strings.Contains(string(env.Result), "person.") {
		t.Error("a person in the first look")
	}
}

func TestMapFirstLookFallsBackInPriority(t *testing.T) {
	f := smallHome()
	var states []map[string]any
	for _, s := range f.states {
		if a, _ := s["attributes"].(map[string]any); a["device_class"] != "battery" && s["entity_id"] != "automation.broken" {
			states = append(states, s)
		}
	}
	f.states = states
	_, env, _ := runMap(t, f, "--first-look")
	r := decodeInto[firstLookResult](t, env.Result)
	if len(r.Observations) != 2 || r.Observations[0].Kind != "integration_problem" || r.Observations[0].Domain != "met" ||
		r.Observations[1].Kind != "automation_never_triggered" {
		t.Errorf("observations %+v", r.Observations)
	}
}

func TestMapWhereUsed(t *testing.T) {
	exit, env, log := runMap(t, smallHome(), "--entity", "light.kitchen")
	if exit != 0 {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
	r := decodeInto[whereUsedResult](t, env.Result)
	if r.Name != "Kitchen light" || r.Area != "Kitchen" || r.Device != "Kitchen bulb" || r.Integration != "hue" {
		t.Errorf("identity %+v", r)
	}
	if r.Automations.Total != 2 || r.Automations.Rows[0].EntityID != "automation.morning" || r.Automations.Rows[0].Via != "" ||
		r.Automations.Rows[1].EntityID != "automation.never" || r.Automations.Rows[1].Via != "device" {
		t.Errorf("automations %+v", r.Automations)
	}
	if r.Scripts.Total != 1 || r.Scripts.Rows[0].Via != "device" || r.Scenes.Total != 1 {
		t.Errorf("scripts %+v scenes %+v", r.Scripts, r.Scenes)
	}
	// automation.old uses only light.kitchen_status (a sibling): never listed
	if strings.Contains(string(env.Result), "automation.old") || strings.Contains(string(env.Result), "automation.child_only") {
		t.Error("a sibling's automation listed as a use")
	}
	// the group state lists the light; a scene's entity_id list is no group
	if r.Groups.Total != 1 || r.Groups.Rows[0].EntityID != "light.group_all" {
		t.Errorf("groups %+v", r.Groups)
	}
	if len(r.NotChecked) == 0 {
		t.Error("not_checked missing")
	}
	related := 0
	for _, wsType := range log.wsTypes() {
		if wsType == "search/related" {
			related++
		}
	}
	if related != 5 {
		t.Errorf("search/related reads = %d, want entity + device + child device + the family's 2 other entities", related)
	}
}

func TestMapWhereUsedUnknownEntity(t *testing.T) {
	exit, env, _ := runMap(t, smallHome(), "--entity", "light.nope")
	if exit != verbExitNotFound || env.Error == nil || env.Error.Code != "NOT_FOUND" {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
}

func TestMapSections(t *testing.T) {
	_, env, _ := runMap(t, smallHome(), "--section", "areas")
	areas := decodeInto[struct {
		Total int           `json:"total"`
		Rows  []areaSummary `json:"rows"`
	}](t, env.Result)
	order := []string{}
	for _, a := range areas.Rows {
		order = append(order, a.AreaID)
	}
	if areas.Total != 4 || strings.Join(order, ",") != "kitchen,living,bedroom,garage" || areas.Rows[1].Domains["sensor"] != 3 {
		t.Errorf("areas section %+v", areas)
	}
	_, env, _ = runMap(t, smallHome(), "--section", "automations", "--limit", "2")
	autos := decodeInto[struct {
		Total int      `json:"total"`
		Shown int      `json:"shown"`
		Rows  []mapRow `json:"rows"`
		More  string   `json:"more"`
	}](t, env.Result)
	if autos.Total != 6 || autos.Shown != 2 || autos.Rows[0].Name != "Broken" || autos.Rows[1].Issue != "restored" || autos.More == "" {
		t.Errorf("automations section %+v", autos)
	}
	_, env, _ = runMap(t, smallHome(), "--section", "integrations", "--limit", "1")
	if !strings.Contains(string(env.Result), `"problems_total":1`) || !strings.Contains(string(env.Result), `"more":"ha-nova map --section integrations --limit 200"`) {
		t.Errorf("integrations section %s", env.Result)
	}
	_, env, _ = runMap(t, smallHome(), "--section", "batteries")
	if !strings.Contains(string(env.Result), `"entity_id":"sensor.phone_battery","name":"Phone battery","level":"5%","low":true,"kind":"phone"`) {
		t.Errorf("batteries section %s", env.Result)
	}
}

func TestMapUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"--section", "areas", "--first-look"}, {"--section", "rooms"}, {"--entity", "kitchen"},
		{"--limit", "5"}, {"--section", "areas", "--limit", "0"}, {"extra"}} {
		paths, log := setupVerbRelayTest(t, smallHome().reply)
		exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, args) })
		if exit != verbExitError || env.Error == nil || env.Error.Code != "USAGE" || len(log.all()) != 0 {
			t.Errorf("%v: exit %d %+v, %d requests", args, exit, env.Error, len(log.all()))
		}
	}
}

func TestMapFailsWholeOnAFailedRead(t *testing.T) {
	f := smallHome()
	paths, _ := setupVerbRelayTest(t, func(r *http.Request, endpoint string, payload map[string]any) any {
		if payload["type"] == "config/device_registry/list" {
			return fakeRelayFailure{code: "UPSTREAM_WS_ERROR", message: "boom"}
		}
		return f.reply(r, endpoint, payload)
	})
	exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, nil) })
	if exit != verbExitError || env.Result != nil {
		t.Fatalf("exit %d result %s", exit, env.Result)
	}
}

func TestMapToleratesACoreWithoutFloors(t *testing.T) {
	f := smallHome()
	paths, _ := setupVerbRelayTest(t, func(r *http.Request, endpoint string, payload map[string]any) any {
		if payload["type"] == "config/floor_registry/list" {
			return fakeRelayFailure{code: "UPSTREAM_WS_ERROR", message: "unknown_command: Unknown command."}
		}
		return f.reply(r, endpoint, payload)
	})
	exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, nil) })
	if exit != 0 || decodeInto[mapSummary](t, env.Result).Counts["floors"] != 0 {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
}

// A big home stays inside the token budget: totals plus capped examples.
func TestMapSummaryStaysSmallOnABigHome(t *testing.T) {
	f := mapFixture{related: map[string]map[string][]string{}}
	domains := []string{"light", "switch", "sensor", "binary_sensor", "cover", "climate", "media_player", "number", "select", "button",
		"update", "event", "fan", "lock", "camera", "device_tracker", "text", "siren", "valve", "vacuum", "water_heater", "humidifier",
		"alarm_control_panel", "remote", "scene", "script", "input_boolean", "input_number", "input_select", "input_text", "counter", "timer",
		"weather", "sun", "zone", "person", "calendar", "todo", "image", "notify", "tts", "stt", "conversation", "wake_word", "lawn_mower", "date"}
	for i := range 8 {
		f.floors = append(f.floors, map[string]any{"floor_id": fmt.Sprintf("floor_%d", i), "name": fmt.Sprintf("Floor number %d", i), "level": i})
	}
	for i := range 60 {
		f.areas = append(f.areas, map[string]any{"area_id": fmt.Sprintf("area_%02d", i), "name": fmt.Sprintf("Wohnküche Erdgeschoß 客厅和餐厅 %02d", i), "floor_id": fmt.Sprintf("floor_%d", i%8)})
	}
	for i := range 150 { // 60 integration domains, 40 entries retrying setup
		state := "loaded"
		if i%150 < 40 {
			state = "setup_retry"
		}
		f.entries = append(f.entries, map[string]any{"entry_id": fmt.Sprintf("e%d", i), "domain": fmt.Sprintf("integration_domain_%02d", i%60), "state": state})
	}
	for i := range 400 {
		f.devices = append(f.devices, map[string]any{"id": fmt.Sprintf("d%d", i), "area_id": fmt.Sprintf("area_%02d", i%60), "config_entry_id": fmt.Sprintf("e%d", i%150)})
	}
	for i := range 5000 {
		id := fmt.Sprintf("%s.entity_number_%04d", domains[i%len(domains)], i)
		var extra map[string]any
		state, attrs := "on", map[string]any{"friendly_name": "Ein ziemlich langer Gerätename 长名称的设备 " + id}
		switch {
		case i%25 == 0:
			state = "unavailable"
		case i%100 == 1:
			state, attrs["device_class"], extra = "9", "battery", map[string]any{"entity_category": "diagnostic"}
		}
		f.entities = append(f.entities, ent(id, fmt.Sprintf("d%d", i%400), "x", fmt.Sprintf("e%d", i%150), extra))
		f.states = append(f.states, st(id, state, attrs))
	}
	for i := range 300 {
		id := fmt.Sprintf("automation.automation_number_%03d", i)
		f.entities = append(f.entities, ent(id, "", "automation", "", nil))
		f.states = append(f.states, st(id, "on", map[string]any{"friendly_name": "Automation with a long name " + id, "last_triggered": nil}))
	}
	exit, env, _ := runMap(t, f)
	if exit != 0 {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
	t.Logf("big-home summary: %d bytes", len(env.Result))
	if os.Getenv("MAP_DUMP") != "" {
		_ = os.WriteFile(os.Getenv("MAP_DUMP"), env.Result, 0o600)
	}
	if size := len(env.Result); size > 8<<10 {
		t.Fatalf("summary is %d bytes on a big home, budget 8 KiB", size)
	}
	s := decodeInto[mapSummary](t, env.Result)
	if s.Counts["entities"] != 5300 || s.Areas.Shown != mapAreaRows || len(s.More) != 5 || s.Integrations["problems_total"] != float64(40) {
		t.Errorf("counts %v shown %d more %v", s.Counts, s.Areas.Shown, s.More)
	}
	_, env, _ = runMap(t, f, "--first-look")
	t.Logf("big-home first look: %d bytes", len(env.Result))
	if size := len(env.Result); size > 2<<10 {
		t.Fatalf("first look is %d bytes, budget 2 KiB", size)
	}
}
