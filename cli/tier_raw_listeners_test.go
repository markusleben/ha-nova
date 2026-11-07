package main

import (
	"cmp"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// #946: a raw write that changes what a trigger sees is tiered by the
// automations that listen to it, as a call is (tier_raw_listeners.go).

// listeningHome: a calendar, a helper, a zone, a person with a GPS phone, a
// renamed and a disabled sensor, and a motion sensor on its own device and
// integration entry; lock.front is the lock a listener could unlock.
func listeningHome() *fakeHA {
	f := newFakeHA()
	f.ent("calendar.family", "local_calendar", "off", nil)
	f.ent("input_boolean.guest", "input_boolean", "off", nil)
	f.ent("zone.work", "zone", "0", nil)
	f.ent("person.anna", "person", "home", nil)
	f.ent("device_tracker.anna_phone", "mobile_app", "home", map[string]any{"latitude": 52.5, "longitude": 13.4})
	f.ent("binary_sensor.side_door", "zha", "off", nil)
	f.ent("binary_sensor.motion", "hue", "off", map[string]any{"_di": "dmotion"})
	f.ent("zone.home", "", "1", nil)
	f.ent("sensor.outside", "met", "12", map[string]any{"unit_of_measurement": "°C"})
	f.full = []map[string]any{
		{"entity_id": "sensor.phone_battery", "platform": "mobile_app", "unique_id": "pb", "config_entry_id": "Ephone", "disabled_by": "user"},
		{"entity_id": "device_tracker.anna_phone", "platform": "mobile_app", "unique_id": "dt", "config_entry_id": "Ephone"},
		{"entity_id": "scene.welcome", "platform": "homeassistant", "unique_id": "welcome"},
		{"entity_id": "input_boolean.guest", "platform": "input_boolean", "unique_id": "guest"},
		{"entity_id": "zone.work", "platform": "zone", "unique_id": "work"},
		{"entity_id": "binary_sensor.side_door", "platform": "zha", "unique_id": "z1"},
		{"entity_id": "binary_sensor.doorbell", "platform": "esphome", "unique_id": "b1", "device_id": "dbell", "disabled_by": "user"},
		{"entity_id": "binary_sensor.motion", "platform": "hue", "unique_id": "m1", "device_id": "dmotion", "config_entry_id": "Emotion"},
		{"entity_id": "binary_sensor.hub_child", "platform": "zwave_js", "unique_id": "hc", "device_id": "dchild"},
	}
	f.devices = []map[string]any{{"id": "dhub"}, {"id": "dchild", "parent_device_id": "dhub"}}
	return f
}

var listenedWrites = []struct {
	name, payload, related string
	trigger                map[string]any
	endpoint               string // "" is ws
}{
	{"a recurring calendar event", `{"type":"calendar/event/create","entity_id":"calendar.family","event":{"summary":"Cleaner","dtstart":"2026-11-06T09:00:00","dtend":"2026-11-06T10:00:00","rrule":"FREQ=WEEKLY"}}`,
		"entity:calendar.family", map[string]any{"trigger": "calendar", "event": "start", "entity_id": "calendar.family"}, ""},
	{"a calendar event update", `{"type":"calendar/event/update","entity_id":"Calendar.Family","uid":"u1","event":{"summary":"Cleaner"}}`,
		"entity:calendar.family", map[string]any{"trigger": "calendar", "event": "start", "entity_id": "calendar.family"}, ""},
	{"a helper update", `{"type":"input_boolean/update","input_boolean_id":"guest","name":"Guest"}`,
		"entity:input_boolean.guest", map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}, ""},
	{"a zone move", `{"type":"zone/update","zone_id":"work","name":"Work","latitude":52.6,"longitude":13.4,"radius":100}`,
		"entity:zone.work", map[string]any{"trigger": "zone", "entity_id": "person.bob", "zone": "zone.work", "event": "enter"}, ""},
	{"a new zone", `{"type":"zone/create","name":"Gym","latitude":52.5,"longitude":13.4}`,
		"entity:device_tracker.anna_phone", map[string]any{"trigger": "state", "entity_id": "device_tracker.anna_phone"}, ""},
	{"a new home location", `{"type":"config/core/update","latitude":52.6,"longitude":13.4}`,
		"entity:person.anna", map[string]any{"trigger": "state", "entity_id": "person.anna", "to": "not_home"}, ""},
	{"an entity rename", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","new_entity_id":"binary_sensor.porch"}`,
		"entity:binary_sensor.porch", map[string]any{"trigger": "state", "entity_id": "binary_sensor.porch", "to": "on"}, ""},
	{"a disabled entity's enable", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.doorbell","disabled_by":null}`,
		"device:dbell", map[string]any{"trigger": "device", "device_id": "dbell", "domain": "binary_sensor", "type": "turned_on"}, ""},
	{"a device rename", `{"type":"config/device_registry/update","device_id":"dmotion","name_by_user":"Hall motion"}`,
		"entity:binary_sensor.motion", map[string]any{"trigger": "state", "entity_id": "binary_sensor.motion"}, ""},
	{"an integration enable", `{"type":"config_entries/disable","entry_id":"Emotion","disabled_by":null}`,
		"entity:binary_sensor.motion", map[string]any{"trigger": "state", "entity_id": "binary_sensor.motion"}, ""},
	{"an enable that reloads the phone's entry", `{"type":"config/entity_registry/update","entity_id":"sensor.phone_battery","disabled_by":null}`,
		"entity:person.anna", map[string]any{"trigger": "state", "entity_id": "person.anna", "to": "home"}, ""},
	{"an integration rename", `{"type":"config_entries/update","entry_id":"Emotion","title":"Hall"}`,
		"entity:binary_sensor.motion", map[string]any{"trigger": "state", "entity_id": "binary_sensor.motion"}, ""},
	{"a subentry rename", `{"type":"config_entries/subentries/update","entry_id":"Emotion","subentry_id":"s1","title":"Hall"}`,
		"entity:binary_sensor.motion", map[string]any{"trigger": "state", "entity_id": "binary_sensor.motion"}, ""},
	{"a hub enable", `{"type":"config/device_registry/update","device_id":"dhub","disabled_by":null}`,
		"entity:binary_sensor.hub_child", map[string]any{"trigger": "state", "entity_id": "binary_sensor.hub_child", "to": "on"}, ""},
	{"an options flow", `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"Emotion"}}`,
		"entity:binary_sensor.motion", map[string]any{"trigger": "state", "entity_id": "binary_sensor.motion"}, "core"},
	{"a scene save", `{"method":"POST","path":"/api/config/scene/config/evening","body":{"name":"Evening","entities":{"light.hall":{"state":"on"}}}}`,
		"entity:scene.welcome", map[string]any{"trigger": "state", "entity_id": "scene.welcome"}, "core"},
}

// rawItem: one plan item of the write.
func rawItem(endpoint, payload string) string {
	return `{"endpoint":"` + cmp.Or(endpoint, "ws") + `","payload":` + payload + "}"
}

// Each write is typed when an automation that references what it changes
// unlocks a lock (ACCESS_LOCK through that automation), and keeps its
// preview, after the same scan, without one.
func TestRawWriteListenersRaiseTheTier(t *testing.T) {
	for _, tc := range listenedWrites {
		for _, listening := range []bool{true, false} {
			f := listeningHome()
			if listening {
				f.automation("opener", "opener", map[string]any{"triggers": []any{tc.trigger}, "actions": []any{step("lock.unlock", "lock.front")}})
				f.related[tc.related] = []string{"automation.opener"}
			}
			paths, searched, _ := listeningRelay(t, f)
			exit, env := planRaw(t, paths, "["+rawItem(tc.endpoint, tc.payload)+"]")
			want := map[bool]string{true: "typed", false: "preview"}[listening]
			if exit != verbExitConfirmation || env.Tier != want || listening != strings.Contains(reasonCodes(env), reasonAccessLock) {
				t.Errorf("%s (listening %v): exit %d tier %s reasons %s %+v", tc.name, listening, exit, env.Tier, reasonCodes(env), env.Error)
			}
			if listening && !strings.Contains(env.Say, "unlock") {
				t.Errorf("%s: say %q names no unlock", tc.name, env.Say)
			}
			item, _ := strings.CutPrefix(tc.related, "entity:")
			if item, _ = strings.CutPrefix(item, "device:"); !searched(item) {
				t.Errorf("%s (listening %v): never searched what relates to %s", tc.name, listening, item)
			}
		}
	}
}

// listeningRelay serves f to plans and relay alike (a plan names its items
// with collection lists, which the read verbs' oracle doesn't allow):
// searched says whether search/related was asked about item, sent counts the
// writes sent.
func listeningRelay(t *testing.T, f *fakeHA) (paths runtimePaths, searched func(item string) bool, sent func() int) {
	t.Helper()
	paths, calls := readRelay(t, func(endpoint string, body map[string]any) any {
		return f.reply(httptest.NewRequest("POST", "/"+endpoint, nil), endpoint, body)
	})
	return paths, func(item string) bool {
			for _, c := range calls() {
				var body map[string]any
				if _ = json.Unmarshal([]byte(c.body), &body); body["type"] == "search/related" && body["item_id"] == item {
					return true
				}
			}
			return false
		}, func() (n int) {
			for _, c := range calls() {
				if !rawRelayRead(c.endpoint, []byte(c.body)) {
					n++
				}
			}
			return n
		}
}

// Fail closed: a relation, state list, or registry that can't be read, and
// a helper the registry doesn't list, make the write typed. A write that
// changes no state (an area move, an unnamed calendar) reads nothing for it.
func TestRawWriteListenersFailClosed(t *testing.T) {
	for _, tc := range []struct{ name, payload, fail, code string }{
		{"related", listenedWrites[0].payload, "search/related", reasonScanFailed},
		{"states", listenedWrites[4].payload, "/api/states", reasonScanFailed},
		{"registry", listenedWrites[2].payload, "config/entity_registry/list", reasonScanFailed},
		{"compact registry", listenedWrites[0].payload, "config/entity_registry/list_for_display", reasonScanFailed},
		{"unlisted helper", `{"type":"input_number/update","input_number_id":"volume","name":"Volume"}`, "", reasonUnresolvedTarget},
	} {
		f := listeningHome()
		f.fail[tc.fail] = tc.fail != ""
		paths, _, _ := listeningRelay(t, f)
		if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":`+tc.payload+`}]`); exit != verbExitConfirmation || env.Tier != "typed" || !strings.Contains(reasonCodes(env), tc.code) {
			t.Errorf("%s: exit %d tier %s reasons %s %+v", tc.name, exit, env.Tier, reasonCodes(env), env.Error)
		}
	}
	f := listeningHome()
	f.related["entity:binary_sensor.side_door"] = []string{"automation.opener"}
	f.unique["binary_sensor.side_door"] = "side_door" // its registry entry, which a labels or aliases update reads (helperKept)
	paths, searched, _ := listeningRelay(t, f)
	for _, payload := range []string{`{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","area_id":"hall","labels":[],"aliases":["Side"]}`,
		`{"type":"calendar/event/create","event":{"summary":"x"}}`, `{"type":"config/core/update","currency":"EUR"}`} {
		if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":`+payload+`}]`); exit != verbExitConfirmation || env.Tier != "preview" {
			t.Errorf("%s: exit %d tier %s reasons %s", payload, exit, env.Tier, reasonCodes(env))
		}
	}
	if searched("binary_sensor.side_door") {
		t.Fatal("a write that changes no state was scanned")
	}
}

// Each item keeps its own tier, and a second item on the same entity gets
// the class the first one's scan found.
func TestRawWriteListenersPerItem(t *testing.T) {
	f := listeningHome()
	f.automation("opener", "opener", map[string]any{"triggers": []any{listenedWrites[0].trigger}, "actions": []any{step("lock.unlock", "lock.front")}})
	f.related["entity:calendar.family"] = []string{"automation.opener"}
	paths, _, _ := listeningRelay(t, f)
	items := "[" + strings.Join([]string{`{"endpoint":"ws","payload":` + listenedWrites[2].payload + "}", `{"endpoint":"ws","payload":` + listenedWrites[0].payload + "}",
		`{"endpoint":"ws","payload":` + listenedWrites[1].payload + "}"}, ",") + "]"
	_, env := planRaw(t, paths, items)
	var result rawPlanResult
	_ = json.Unmarshal(env.Result, &result)
	var tiers []string
	for _, item := range result.Items {
		tiers = append(tiers, item.Tier)
	}
	if got := env.Tier + ": " + strings.Join(tiers, ","); got != "typed: preview,typed,typed" {
		t.Fatalf("tiers %s", got)
	}
}

// A new person who follows a tracker is typed by its payload
// (person_follows); the plan also names what listens to the zone it counts in.
func TestRawNewPersonNamesZoneListeners(t *testing.T) {
	f := listeningHome()
	f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "numeric_state", "entity_id": "zone.home", "above": 0}},
		"actions": []any{step("alarm_control_panel.alarm_disarm", "alarm_control_panel.home")}})
	f.related["entity:zone.home"] = []string{"automation.opener"}
	paths, _, _ := listeningRelay(t, f)
	_, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"person/create","name":"Kids","device_trackers":["device_tracker.ipad"]}}]`)
	if env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonAccessAlarm) {
		t.Fatalf("tier %s reasons %s", env.Tier, reasonCodes(env))
	}
}

// The configs a listener scan read are in the plan's basis: a listener that
// starts unlocking after the preview is drift, and nothing is sent.
func TestRawWriteListenerChangedBeforeApplyIsDrift(t *testing.T) {
	f := listeningHome()
	f.automation("opener", "opener", map[string]any{"triggers": []any{listenedWrites[0].trigger}, "actions": []any{step("light.turn_on", "light.hall")}})
	f.related["entity:calendar.family"] = []string{"automation.opener"}
	paths, _, sent := listeningRelay(t, f)
	_, env := planRaw(t, paths, `[{"endpoint":"ws","payload":`+listenedWrites[0].payload+`}]`)
	if env.Tier != "preview" || env.Plan == nil {
		t.Fatalf("tier %s reasons %s %+v", env.Tier, reasonCodes(env), env.Error)
	}
	f.mu.Lock()
	f.configs["/api/config/automation/config/opener"] = map[string]any{"triggers": []any{listenedWrites[0].trigger}, "actions": []any{step("lock.unlock", "lock.front")}}
	f.mu.Unlock()
	if exit, applied := applyRaw(t, paths, env); exit != verbExitDrift || applied.Status != "drift" || sent() != 0 {
		t.Fatalf("exit %d status %s reasons %s, %d sent", exit, applied.Status, reasonCodes(applied), sent())
	}
}

// ha-nova relay ws sends at once, so a write whose listener a plan rates
// typed is refused there (a delete and a unit change too), as the body or an envelope's
// message, and so is one whose listeners can't be read; nothing is sent.
// Without such a listener the write is sent, and a read is never checked.
func TestRelayRefusesAWriteWhoseListenerIsTyped(t *testing.T) {
	f := listeningHome()
	f.automation("opener", "opener", map[string]any{"triggers": []any{listenedWrites[2].trigger}, "actions": []any{step("lock.unlock", "lock.front")}})
	f.related["entity:input_boolean.guest"] = []string{"automation.opener"}
	f.automation("gate", "gate", map[string]any{"triggers": []any{listenedWrites[8].trigger}, "actions": []any{step("lock.unlock", "lock.front")}})
	f.related["entity:binary_sensor.motion"] = []string{"automation.gate"}
	f.automation("warm", "warm", map[string]any{"triggers": []any{map[string]any{"trigger": "numeric_state", "entity_id": "sensor.outside", "above": 40}}, "actions": []any{step("lock.unlock", "lock.front")}})
	f.related["entity:sensor.outside"] = []string{"automation.warm"}
	paths, _, sent := listeningRelay(t, f)
	var exit int
	stderr := captureStderr(t, func() {
		exit = runRelayCommand(paths, []string{"core", "--method", "POST", "--path", "/api/config/config_entries/options/flow", "--body", `{"handler":"Emotion"}`})
	})
	if exit != verbExitRefused || !strings.Contains(stderr, "refused raw POST /api/config/config_entries/options/flow") || !strings.Contains(stderr, `"endpoint":"core"`) {
		t.Errorf("options flow: exit %d, stderr %q; want a refusal naming the core plan item", exit, stderr)
	}
	for _, body := range []string{listenedWrites[2].payload, `{"message":` + listenedWrites[2].payload + `,"collect_events":{"max_events":1}}`,
		`{"type":"input_boolean/delete","input_boolean_id":"guest"}`, `{"type":"config/core/update","unit_system":"us_customary"}`, listenedWrites[4].payload} {
		if body == listenedWrites[4].payload {
			f.fail["/api/states"] = true
		}
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", body}) })
		if exit != verbExitRefused || !strings.Contains(stderr, "ha-nova plan raw --file") || !strings.Contains(stderr, "nothing was sent") {
			t.Errorf("%s: exit %d, stderr %q; want a refusal naming the plan", body, exit, stderr)
		}
	}
	if n := sent(); n != 0 {
		t.Fatalf("%d refused writes were sent", n)
	}
	f.fail["/api/states"] = false
	if stderr := captureStderr(t, func() {
		runRelayCommand(paths, []string{"ws", "--data", `{"type":"config_entries/subentries/list","entry_id":"Emotion"}`})
	}); strings.Contains(stderr, "refused") {
		t.Errorf("a read was checked: %q", stderr)
	}
	for _, body := range []string{listenedWrites[0].payload, listenedWrites[4].payload} {
		if exit := runRelayCommand(paths, []string{"ws", "--data", body}); exit != 0 {
			t.Errorf("%s: exit %d, want sent", body, exit)
		}
	}
	if n := sent(); n != 2 {
		t.Fatalf("sent %d, want the 2 writes nothing typed listens to", n)
	}
}
