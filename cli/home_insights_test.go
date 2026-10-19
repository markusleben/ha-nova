package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// indexFixture builds the map index straight from a fixture (no Relay).
func indexFixture(t *testing.T, f mapFixture) *homeIndex {
	t.Helper()
	h := &homeData{floors: []floorRow{}}
	for _, p := range []struct{ src, dst any }{{f.areas, &h.areas}, {f.devices, &h.devices}, {f.entities, &h.entities},
		{f.entries, &h.entries}, {f.helpers, &h.helperEntries}, {f.states, &h.states}} {
		raw, _ := json.Marshal(p.src)
		if err := json.Unmarshal(raw, p.dst); err != nil {
			t.Fatal(err)
		}
	}
	return indexHome(h, time.Now())
}

func agedSt(id, state string, age time.Duration, attrs map[string]any) map[string]any {
	row := st(id, state, attrs)
	row["last_changed"] = ago(age)
	return row
}

// wowHome: rooms to suggest, an offline device, low batteries, broken and
// idle automations, a phone, a person, and a lock next to a light.
func wowHome() mapFixture {
	return mapFixture{
		areas: []map[string]any{{"area_id": "kitchen", "name": "Kitchen"}, {"area_id": "bedroom", "name": "Bedroom"},
			{"area_id": "guest", "name": "Guest Bedroom"}, {"area_id": "hall", "name": "Hall"}},
		devices: []map[string]any{
			{"id": "d_kitchen", "name": "Kitchen Lights", "config_entry_id": "e_hue"},
			{"id": "d_radio", "name": "Küchenradio", "config_entry_id": "e_hue"},
			{"id": "d_guest", "name": "Lamp", "name_by_user": "Guest Bedroom Lamp", "config_entry_id": "e_hue"},
			{"id": "d_both", "name": "Kitchen to Bedroom Switch", "config_entry_id": "e_hue"},
			{"id": "d_bridge", "name": "Hue Bridge", "config_entry_id": "e_hue"},
			{"id": "d_phone", "name": "Kitchen iPhone", "config_entry_id": "e_mobile"},
			{"id": "d_tracker", "name": "Kitchen Tag", "config_entry_id": "e_zha"},
			{"id": "d_service", "name": "Kitchen Weather", "config_entry_id": "e_met", "entry_type": "service"},
			{"id": "d_placed", "name": "Bedroom Lamp", "area_id": "bedroom", "config_entry_id": "e_hue"},
			{"id": "d_off", "name": "Kitchen Spare", "disabled_by": "user", "config_entry_id": "e_hue"},
			{"id": "d_dead", "name": "Garden Sensor", "area_id": "hall", "config_entry_id": "e_zha"},
			{"id": "d_half", "name": "Half Sensor", "area_id": "hall", "config_entry_id": "e_zha"},
			{"id": "d_fresh", "name": "Fresh Sensor", "area_id": "hall", "config_entry_id": "e_zha"},
			{"id": "d_lock", "name": "Front Door", "area_id": "hall", "config_entry_id": "e_zwave"},
		},
		entities: []map[string]any{
			ent("light.kitchen_lights", "d_kitchen", "hue", "e_hue", nil),
			ent("sensor.phone_battery", "d_phone", "mobile_app", "e_mobile", nil),
			ent("device_tracker.kitchen_tag", "d_tracker", "zha", "e_zha", nil),
			ent("sensor.dead_temp", "d_dead", "zha", "e_zha", nil),
			ent("sensor.dead_battery", "d_dead", "zha", "e_zha", map[string]any{"entity_category": "diagnostic"}),
			ent("sensor.half_a", "d_half", "zha", "e_zha", nil),
			ent("sensor.half_b", "d_half", "zha", "e_zha", nil),
			ent("sensor.fresh", "d_fresh", "zha", "e_zha", nil),
			ent("sensor.door_battery", "d_lock", "zwave_js", "e_zwave", map[string]any{"entity_category": "diagnostic"}),
			ent("sensor.remote_battery", "d_placed", "hue", "e_hue", map[string]any{"entity_category": "diagnostic"}),
			ent("lock.front_door", "d_lock", "zwave_js", "e_zwave", nil),
			ent("light.hall", "d_placed", "hue", "e_hue", map[string]any{"area_id": "hall"}),
			ent("light.bedroom", "d_placed", "hue", "e_hue", nil),
			ent("automation.broken", "", "automation", "", nil),
			ent("automation.never", "", "automation", "", nil),
			ent("automation.fine", "", "automation", "", nil),
		},
		entries: []map[string]any{{"entry_id": "e_hue", "domain": "hue"}, {"entry_id": "e_mobile", "domain": "mobile_app"},
			{"entry_id": "e_zha", "domain": "zha"}, {"entry_id": "e_met", "domain": "met"}, {"entry_id": "e_zwave", "domain": "zwave_js"}},
		states: []map[string]any{
			st("light.kitchen_lights", "on", map[string]any{"friendly_name": "Kitchen Lights"}),
			st("sensor.phone_battery", "3", map[string]any{"device_class": "battery", "friendly_name": "Kitchen iPhone battery"}),
			st("device_tracker.kitchen_tag", "home", map[string]any{"friendly_name": "Kitchen Tag"}),
			agedSt("sensor.dead_temp", "unavailable", 3*24*time.Hour, map[string]any{"friendly_name": "Garden temperature"}),
			agedSt("sensor.dead_battery", "unavailable", 2*24*time.Hour, map[string]any{"friendly_name": "Garden battery"}),
			agedSt("sensor.half_a", "unavailable", 5*24*time.Hour, nil),
			st("sensor.half_b", "12", nil),
			agedSt("sensor.fresh", "unavailable", 2*time.Hour, nil),
			st("sensor.door_battery", "12", map[string]any{"device_class": "battery", "friendly_name": "Front Door battery"}),
			st("sensor.remote_battery", "60", map[string]any{"device_class": "battery", "friendly_name": "Remote battery"}),
			st("lock.front_door", "locked", map[string]any{"friendly_name": "Front Door"}),
			st("light.hall", "off", map[string]any{"friendly_name": "Hall Light"}),
			st("light.bedroom", "off", map[string]any{"friendly_name": "Bedroom Light"}),
			st("automation.broken", "unavailable", map[string]any{"friendly_name": "Broken"}),
			st("automation.never", "on", map[string]any{"friendly_name": "Never", "last_triggered": nil}),
			st("automation.fine", "on", map[string]any{"friendly_name": "Fine", "last_triggered": ago(time.Hour)}),
			st("person.someone", "home", map[string]any{"friendly_name": "Someone"}),
			st("zone.home", "1", map[string]any{"friendly_name": "Home"}),
		},
	}
}

func TestInsightsRankFixedRules(t *testing.T) {
	exit, env, _ := runMap(t, wowHome(), "--insights")
	if exit != 0 {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
	r := decodeInto[insightsResult](t, env.Result)
	if got := r.Stats; got["rooms"] != 4 || got["devices"] != 13 || got["lights"] != 3 || got["locks"] != 1 || got["automations"] != 3 {
		t.Errorf("stats %v", got)
	}
	var kinds []string
	for _, in := range r.Insights {
		kinds = append(kinds, in.Kind)
	}
	if got := strings.Join(kinds, ","); got != "battery_low,no_room,automation_unavailable,device_offline,automation_never_ran" {
		t.Fatalf("ranked %s", got)
	}
	battery := r.Insights[0]
	// the phone's 3% is never a device battery; no linked phone: no reminder offered
	if battery.EntityID != "sensor.door_battery" || battery.Value != "12%" || battery.Count != 1 || battery.Action != "" {
		t.Errorf("battery %+v", battery)
	}
	// unavailable for days on every entity: offline; one live entity or 2 h: not
	if off := r.Insights[3]; off.DeviceID != "d_dead" || off.Name != "Garden Sensor" || off.Value != "2d" || off.Count != 1 || off.Area != "Hall" {
		t.Errorf("offline %+v", off)
	}
	if never := r.Insights[4]; never.EntityID != "automation.never" || never.Count != 1 {
		t.Errorf("never %+v", never)
	}
	// this fake answers none of the tier engine's reads: fail closed, no
	// template is offered (TestInsightsOfferOnlyPreviewTemplates covers both ways)
	if r.Automation != nil {
		t.Errorf("automation offered without a tier: %+v", r.Automation)
	}
	// the lock and the light share the hall: the made-for-you automation
	m := indexFixture(t, wowHome()).insights(nil, offerAll).Automation
	if m == nil || m.Template != templateDoorLight || m.Uses[0].EntityID != "lock.front_door" || m.Uses[1].EntityID != "light.hall" {
		t.Errorf("automation %+v", m)
	}
}

func TestRoomSuggestionsOnlyConfidentAndNeverPrivate(t *testing.T) {
	r := indexFixture(t, wowHome()).insights(nil, offerAll)
	rooms := r.Insights[1]
	got := map[string]string{}
	for _, s := range rooms.Suggestions {
		got[s.DeviceID] = s.AreaID + "/" + s.Confidence
	}
	want := map[string]string{"d_kitchen": "kitchen/high", "d_radio": "kitchen/medium", "d_guest": "guest/high"}
	if len(got) != len(want) || rooms.Action != "rooms" {
		t.Fatalf("suggestions %v action %q", got, rooms.Action)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s, want %s", k, got[k], v)
		}
	}
	// counted, never named: phones, trackers, and services carry no suggestion
	if rooms.Count != 7 {
		t.Errorf("devices without a room %d", rooms.Count)
	}
}

func TestSuggestRoomRules(t *testing.T) {
	var areas []foldedArea
	for _, n := range []string{"Küche", "Living Room", "Bad", "Guest Bedroom", "Bedroom", "Office", "Home Office"} {
		w := foldWords(n)
		areas = append(areas, foldedArea{id: n, name: n, words: w, kinds: roomKinds(w)})
	}
	for name, want := range map[string]string{
		"Küche Licht":                     "Küche/high",
		"KÜCHENRADIO":                     "Küche/medium",
		"Kitchen Speaker":                 "Küche/medium",
		"Living Room Lamp":                "Living Room/high",
		"Wohnzimmerlampe":                 "Living Room/medium",
		"Lounge spot":                     "Living Room/medium",
		"Bad Spiegel":                     "Bad/high",
		"Badezimmer Heizung":              "Bad/medium",
		"Guest Bedroom Lamp":              "Guest Bedroom/high",
		"Bedroom Lamp":                    "Bedroom/high",
		"Home Office Printer":             "Home Office/high",
		"Office Printer":                  "Office/high",
		"Kitchen Bedroom Link":            "",           // two rooms named: no guess
		"Badge reader":                    "",           // "bad" only as a whole word
		"Schlafzimmer Licht":              "",           // two bedroom-kind rooms: no guess
		"Hue color lamp 1":                "",           // nothing
		"Livingcolors":                    "",           // an English word never matches inside another
		"Officejet Pro":                   "",           // same
		"Flur Licht":                      "",           // no hall area
		"Kitchen and Küche":               "Küche/high", // both name the one kitchen
		"Guest Bedroom to Bedroom Switch": "",           // every occurrence counts
	} {
		area, confidence, ok := suggestRoom(name, areas)
		got := ""
		if ok {
			got = area.name + "/" + confidence
		}
		if got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

func TestInsightsNeverNamePresence(t *testing.T) {
	f := wowHome()
	_, env, _ := runMap(t, f, "--insights")
	out := string(env.Result)
	for _, leak := range []string{"person.", "Someone", "device_tracker", "Kitchen Tag", "zone.home", "iPhone", "d_phone", "d_tracker"} {
		if strings.Contains(out, leak) {
			t.Errorf("insights name %q: %s", leak, out)
		}
	}
	// the away template uses the person count and zone.home, and lists neither
	m, ok := indexFixture(t, f).matchTemplate(templateLightsOff, nil)
	raw, _ := json.Marshal(m)
	if !ok || strings.Contains(string(raw), "zone.") || strings.Contains(string(raw), "person") || strings.Contains(string(raw), "Someone") {
		t.Errorf("away uses %s", raw)
	}
}

// The battery reminder needs a linked phone; with one it is the battery
// row's action and never also the made-for-you automation.
func TestBatteryReminderOnlyWithLinkedPhone(t *testing.T) {
	x := indexFixture(t, wowHome())
	if _, ok := x.matchTemplate(templateBatteryReminder, nil); ok {
		t.Fatal("reminder without a phone")
	}
	if _, ok := x.matchTemplate(templateBatteryReminder, &phoneTarget{Service: "notify.bad", Name: "x"}); ok {
		t.Fatal("reminder to a service that is not a phone")
	}
	phone := &phoneTarget{Service: "mobile_app_pixel", Name: "Pixel"}
	r := x.insights(phone, offerAll)
	if r.Insights[0].Action != templateBatteryReminder || r.Automation == nil || r.Automation.Template == templateBatteryReminder {
		t.Fatalf("battery %+v automation %+v", r.Insights[0], r.Automation)
	}
	m, _ := x.matchTemplate(templateBatteryReminder, phone)
	for _, u := range m.Uses { // device batteries only: the phone's own battery and unavailable ones are no trigger
		if u.EntityID == "sensor.phone_battery" || u.EntityID == "sensor.dead_battery" {
			t.Errorf("uses %+v", u)
		}
	}
}

func templateJSON(t *testing.T, f mapFixture, id string, phone *phoneTarget) string {
	t.Helper()
	m, ok := indexFixture(t, f).matchTemplate(id, phone)
	if !ok {
		t.Fatalf("%s: no match", id)
	}
	raw, _ := json.Marshal(m.draft)
	return string(raw)
}

// Golden bodies: the exact pre-reviewed drafts. State triggers name from:
// (no fire on a return from unavailable) and keep a lock's unlocking step.
func TestTemplateGoldenDrafts(t *testing.T) {
	f := wowHome()
	want := map[string]string{
		templateDoorLight: `{"alias":"Light on when Front Door is unlocked after dark","description":"Made by HA NOVA setup from a pre-reviewed template.","mode":"single",` +
			`"triggers":[{"entity_id":"lock.front_door","from":["locked","unlocking"],"to":"unlocked","trigger":"state"}],` +
			`"conditions":[{"condition":"or","conditions":[{"after":"sunset","condition":"sun"},{"before":"sunrise","condition":"sun"}]}],` +
			`"actions":[{"action":"light.turn_on","target":{"entity_id":"light.hall"}}]}`,
		templateLightsOff: `{"alias":"Lights off when everyone has left","description":"Made by HA NOVA setup from a pre-reviewed template.","mode":"single",` +
			`"triggers":[{"below":1,"entity_id":"zone.home","for":{"minutes":10},"trigger":"numeric_state"}],"conditions":[],` +
			`"actions":[{"action":"light.turn_off","target":{"entity_id":["light.bedroom","light.hall","light.kitchen_lights"]}}]}`,
		templateBatteryReminder: `{"alias":"Remind me when a battery drops below 10%","description":"Made by HA NOVA setup from a pre-reviewed template.","mode":"queued",` +
			`"triggers":[{"below":10,"entity_id":["sensor.door_battery","sensor.remote_battery"],"trigger":"numeric_state"}],"conditions":[],` +
			`"actions":[{"action":"notify.mobile_app_pixel","data":{"message":"{{ trigger.to_state.name }} is at {{ trigger.to_state.state }}%. Time for a new battery.","title":"Low battery"}}]}`,
	}
	for id, golden := range want {
		if got := templateJSON(t, f, id, &phoneTarget{Service: "mobile_app_pixel", Name: "Pixel"}); got != golden {
			t.Errorf("%s:\n got %s\nwant %s", id, got, golden)
		}
		raw, _ := json.Marshal(json.RawMessage(want[id]))
		if notices := draftTriggerNotices(raw, nil); len(notices) != 0 {
			t.Errorf("%s: trigger notices %v", id, notices)
		}
	}
	// a door contact: from off, to on
	f.states = append(f.states, st("binary_sensor.back_door", "off", map[string]any{"device_class": "door", "friendly_name": "Back Door"}))
	f.entities = append(f.entities, ent("binary_sensor.back_door", "", "zha", "e_zha", map[string]any{"area_id": "bedroom"}))
	for i, s := range f.states {
		if s["entity_id"] == "lock.front_door" {
			f.states[i] = st("lock.front_door", "unavailable", nil) // not a usable trigger now
		}
	}
	if got := templateJSON(t, f, templateDoorLight, nil); !strings.Contains(got, `"triggers":[{"entity_id":"binary_sensor.back_door","from":"off","to":"on","trigger":"state"}]`) ||
		!strings.Contains(got, `"alias":"Light on when Back Door opens after dark"`) || !strings.Contains(got, `"entity_id":"light.bedroom"`) {
		t.Errorf("door contact: %s", got)
	}
}

func TestTemplatesNoMatch(t *testing.T) {
	// a lock with no light in its area and no shared place word
	f := wowHome()
	for i, e := range f.entities {
		if e["entity_id"] == "light.hall" {
			f.entities[i]["area_id"] = "bedroom"
		}
	}
	if _, ok := indexFixture(t, f).matchTemplate(templateDoorLight, nil); ok {
		t.Error("door light without a pair")
	}
	// names never pair (brand words: "IKEA … Sensor" and "IKEA … bulb")
	f.states = append(f.states, st("light.front_porch", "off", map[string]any{"friendly_name": "Front Porch"}))
	if m, ok := indexFixture(t, f).matchTemplate(templateDoorLight, nil); ok {
		t.Errorf("name pair %+v", m)
	}
	// away: no person, or more lights than one plan should name
	f = wowHome()
	f.states = f.states[:len(f.states)-2]
	f.states = append(f.states, st("zone.home", "0", nil))
	if _, ok := indexFixture(t, f).matchTemplate(templateLightsOff, nil); ok {
		t.Error("away without a person")
	}
	f = wowHome()
	for i := range lightsOffMax {
		f.states = append(f.states, st("light.extra_"+string(rune('a'+i%26))+string(rune('a'+i/26)), "off", nil))
	}
	if _, ok := indexFixture(t, f).matchTemplate(templateLightsOff, nil); ok {
		t.Error("away over the light cap")
	}
	// nothing at all: no automation is offered
	if r := indexFixture(t, mapFixture{}).insights(nil, offerAll); r.Automation != nil || len(r.Insights) != 0 {
		t.Errorf("empty home %+v", r)
	}
}

func offerAll(templateMatch) bool { return true }

// The page is offered a template only when its plan would be preview tier.
func TestInsightsOfferOnlyPreviewTemplates(t *testing.T) {
	for _, typed := range []bool{false, true} {
		f := newFakeHA()
		f.full = []map[string]any{{"entity_id": "lock.front", "platform": "zwave_js", "area_id": "hall"}, {"entity_id": "light.hall", "platform": "hue", "area_id": "hall"}}
		f.fail["config/entity_registry/list_for_display"] = typed // an unreadable registry makes any plan typed
		paths, _ := setupVerbRelayTest(t, f.reply)
		exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, []string{"--insights"}) })
		r := decodeInto[insightsResult](t, env.Result)
		if exit != 0 || (r.Automation != nil) == typed || (!typed && r.Automation.Template != templateDoorLight) {
			t.Errorf("typed %v: exit %d automation %+v", typed, exit, r.Automation)
		}
	}
}

// A device with a tracker (a car, a tag) is private everywhere: its battery
// is never named and its lock is never a trigger.
func TestPrivateDevicesNeverInBatteriesOrTriggers(t *testing.T) {
	f := wowHome()
	f.entities = append(f.entities, ent("sensor.tag_battery", "d_tracker", "zha", "e_zha", map[string]any{"entity_category": "diagnostic"}),
		ent("lock.car", "d_tracker", "tesla", "e_zha", map[string]any{"area_id": "hall"}),
		ent("light.car_cabin", "d_tracker", "tesla", "e_zha", map[string]any{"area_id": "hall"}))
	f.states = append(f.states, st("sensor.tag_battery", "4", map[string]any{"device_class": "battery", "friendly_name": "Kitchen Tag battery"}),
		st("lock.car", "locked", map[string]any{"friendly_name": "Model Y"}), st("light.car_cabin", "off", map[string]any{"friendly_name": "Cabin"}))
	x := indexFixture(t, f)
	phone := &phoneTarget{Service: "mobile_app_pixel", Name: "Pixel"}
	r := x.insights(phone, offerAll)
	m, _ := x.matchTemplate(templateBatteryReminder, phone)
	raw, _ := json.Marshal([]any{r, m})
	away, _ := x.matchTemplate(templateLightsOff, nil)
	raw, _ = json.Marshal([]any{r, m, away})
	for _, leak := range []string{"tag_battery", "Kitchen Tag", "lock.car", "Model Y", "car_cabin", "Cabin"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("names %q: %s", leak, raw)
		}
	}
	if r.Insights[0].EntityID != "sensor.door_battery" || r.Automation.Uses[0].EntityID != "lock.front_door" {
		t.Errorf("battery %+v automation %+v", r.Insights[0], r.Automation)
	}
}

// "Remind me" only where it can fire: the shown battery is covered and
// still above 10% (numeric_state fires on crossing). A person stuck at
// unknown never moves zone.home, so no away template.
func TestTemplatesOnlyWhereTheyCanFire(t *testing.T) {
	f := wowHome()
	for i, s := range f.states {
		switch s["entity_id"] {
		case "sensor.door_battery":
			f.states[i] = st("sensor.door_battery", "7", map[string]any{"device_class": "battery", "friendly_name": "Front Door battery"})
		case "person.someone":
			f.states[i] = st("person.someone", "unknown", nil)
		}
	}
	x := indexFixture(t, f)
	r := x.insights(&phoneTarget{Service: "mobile_app_pixel", Name: "Pixel"}, offerAll)
	if r.Insights[0].Kind != "battery_low" || r.Insights[0].Action != "" {
		t.Errorf("battery %+v", r.Insights[0])
	}
	if _, ok := x.matchTemplate(templateLightsOff, nil); ok {
		t.Error("away with only an unknown person")
	}
}

// Codex review of #751: one person without a location blocks the away
// template; a battery row without the action leaves the reminder to
// "Made for your home".
func TestAwayNeedsEveryPersonAndReminderFallsBack(t *testing.T) {
	f := wowHome()
	f.states = append(f.states, st("person.other", "unknown", nil))
	if _, ok := indexFixture(t, f).matchTemplate(templateLightsOff, nil); ok {
		t.Error("away while a person's location is unknown")
	}
	for i, s := range f.states {
		switch s["entity_id"] {
		case "sensor.door_battery": // shown, but already below 10%: the row gets no action
			f.states[i] = st("sensor.door_battery", "7", map[string]any{"device_class": "battery", "friendly_name": "Front Door battery"})
		case "lock.front_door": // no door-light pair
			f.states[i] = st("lock.front_door", "unavailable", nil)
		}
	}
	r := indexFixture(t, f).insights(&phoneTarget{Service: "mobile_app_pixel", Name: "Pixel"}, offerAll)
	if r.Insights[0].Action != "" || r.Automation == nil || r.Automation.Template != templateBatteryReminder {
		t.Errorf("battery %+v automation %+v", r.Insights[0], r.Automation)
	}
}
