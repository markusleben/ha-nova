package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every write a raw plan rates typed is refused by ha-nova relay before it
// is sent, naming the plan; every other allowlisted write passes this rule.
// The tier is the plan's own (rawItemTier, read live here), so relay keeps
// no list of its own (docs/work/2026-11-03-relay-refuses-code-writes-spec.md).
func TestRelayRefusesWhatAPlanTypes(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"config/device_registry/list": func(map[string]any) any { return []any{map[string]any{"id": "demo"}} },
	})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	refused := 0
	check := func(endpoint string, payload map[string]any, args ...string) {
		t.Helper()
		level, _ := rawItemTier(newRawNamer(s, nil), rawRequest{Endpoint: endpoint}, payload)
		before := len(sent())
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, args) })
		ours := strings.Contains(stderr, "only a plan asks them")
		switch {
		case level == tierTyped:
			refused++
			if exit != verbExitRefused || !ours || !strings.Contains(stderr, "ha-nova plan raw --file <file>, show the user its say, and follow its next_step; nothing was sent") || len(sent()) != before {
				t.Errorf("%v: exit %d, stderr %q, sent %v; want refused before sending", args, exit, stderr, sent()[before:])
			}
			exact, _ := json.Marshal(payload)
			if endpoint == "core" && !strings.Contains(stderr, `"payload":`+string(exact)+"}]") || strings.Contains(args[len(args)-1], "collect_events") && !strings.Contains(stderr, "<the message itself") {
				t.Errorf("%v: stderr %q; want the request to plan", args, stderr)
			}
		case ours:
			t.Errorf("%v: a %s write refused as typed: %q", args, level, stderr)
		}
	}
	for wsType, what := range rawWSAllowed {
		if what == rawWSRead {
			continue
		}
		payload := map[string]any{"type": wsType}
		if wsType == "config/device_registry/remove" {
			payload["device_id"] = "demo"
		}
		body, _ := json.Marshal(payload)
		check("ws", payload, "ws", "--data", string(body))
		envelope, _ := json.Marshal(map[string]any{"message": payload, "collect_events": map[string]any{"max_events": 1}})
		check("ws", payload, "ws", "--data", string(envelope))
	}
	for _, tc := range []struct{ method, path string }{{"delete", "/api/config/config_entries/entry/0123abcd"}, {"DELETE", "/api/config/scene/config/evening"},
		{"DELETE", "/api/config/config_entries/flow/0123abcd"}, {"POST", "/api/config/core/check_config"}} {
		check("core", map[string]any{"method": strings.ToUpper(tc.method), "path": tc.path}, "core", "--method", tc.method, "--path", tc.path)
	}
	for _, body := range []string{`{"action":"delete_file","path":"packages/a.yaml"}`, `{"action":"write_file","path":"packages/a.yaml","content":"a: 1"}`} {
		payload, _ := decodeObject([]byte(body))
		check("files", payload, "files", "--data", body)
	}
	for _, body := range []string{`{"action":"delete","file":"scenes/a.json.gz"}`, `{"action":"save","category":"scenes","name":"auto-a","data":{}}`} {
		payload, _ := decodeObject([]byte(body))
		check("backups", payload, "backups", "--data", body)
	}
	if refused < 40 {
		t.Fatalf("only %d typed writes checked; the allowlist has more", refused)
	}
}

// A write typed without a read is refused with no request at all; one typed
// only by a read it needs opens a session and reads live, where a failed
// read still refuses and a clean one sends.
func TestRelayTypedReadsOnlyWhenTheTierNeedsIt(t *testing.T) {
	var fail bool
	paths, calls := readRelay(t, func(endpoint string, body map[string]any) any {
		switch {
		case fail:
			return fakeRelayFailure{code: "UPSTREAM_UNAVAILABLE", message: "down"}
		case body["type"] == "config/entity_registry/list":
			return []any{map[string]any{"entity_id": "lock.front", "device_id": "dlock"}, map[string]any{"entity_id": "light.desk", "device_id": "dlamp"}}
		case body["type"] == "config/device_registry/list":
			return []any{map[string]any{"id": "dlock"}, map[string]any{"id": "dlamp"}}
		}
		return nil
	})
	run := func(body string) (int, string) {
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", body}) })
		return exit, stderr
	}
	if exit, stderr := run(`{"type":"calendar/event/delete","entity_id":"calendar.family","uid":"e1"}`); exit != verbExitRefused ||
		!strings.Contains(stderr, "refused raw calendar/event/delete (a safety rule, not a setup or connection problem): this deletes something in Home Assistant") || len(calls()) != 0 {
		t.Fatalf("static delete: exit %d, stderr %q, %d requests; want refused with none", exit, stderr, len(calls()))
	}
	if exit, stderr := run(`{"type":"config/entity_registry/update","entity_id":"lock.front","area_id":"hall"}`); exit != verbExitRefused || !strings.Contains(stderr, "onto a lock") {
		t.Fatalf("lock move: exit %d, stderr %q; want refused", exit, stderr)
	}
	reads := len(calls())
	if reads == 0 {
		t.Fatal("the lock move was refused without reading the registry")
	}
	if exit, _ := run(`{"type":"config/entity_registry/update","entity_id":"light.desk","area_id":"office"}`); exit != 0 || calls()[len(calls())-1].endpoint != "ws" ||
		!strings.Contains(calls()[len(calls())-1].body, "light.desk") {
		t.Fatalf("light move: exit %d, last call %+v; want sent", exit, calls()[len(calls())-1])
	}
	fail = true
	before := len(calls())
	if exit, stderr := run(`{"type":"config/entity_registry/update","entity_id":"light.desk","area_id":"hall"}`); exit == 0 || !strings.Contains(stderr, "nothing was sent") {
		t.Fatalf("unreadable registry: exit %d, stderr %q; want nothing sent", exit, stderr)
	}
	for _, c := range calls()[before:] {
		if strings.Contains(c.body, "light.desk") {
			t.Fatalf("sent %s with an unreadable registry", c.body)
		}
	}
}

// A person's trackers: a create follows what it names; an update replaces
// the whole list (Home Assistant empties it when the key is missing), so
// it is typed exactly when the set it leaves differs from person/list's.
// The plan's tier and relay's refusal agree. A plan holds a copy of Anna
// first: an update that drops her trackers needs one (helperKept). A
// person the list doesn't hold is NOT_FOUND, and an unreadable list plans
// nothing.
func TestPersonTrackersTierByTheSetTheyLeave(t *testing.T) {
	personItems := func(payload string) string { // Anna's copy first
		item := `{"endpoint":"ws","payload":` + payload + `}`
		if !strings.Contains(payload, `"person_id":"anna"`) {
			return "[" + item + "]"
		}
		return `[{"endpoint":"backups","payload":{"action":"save","category":"persons","name":"auto-anna","data":{"id":"anna","name":"Anna","device_trackers":["device_tracker.anna_phone","device_tracker.anna_watch"]}}},` + item + "]"
	}
	broken := false
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any { // each person's entity (#946)
			return []any{map[string]any{"entity_id": "person.anna", "platform": "person", "unique_id": "anna"}, map[string]any{"entity_id": "person.ben", "platform": "person", "unique_id": "ben"}}
		},
		"person/list": func(map[string]any) any {
			if broken {
				return map[string]any{"storage": "unreadable"}
			}
			return map[string]any{"storage": []any{
				map[string]any{"id": "anna", "name": "Anna", "device_trackers": []any{"device_tracker.anna_phone", "device_tracker.anna_watch"}},
				map[string]any{"id": "ben", "name": "Ben", "device_trackers": []any{}},
				map[string]any{"id": "cleo", "name": "Cleo"},
			}, "config": []any{}}
		},
	})
	for payload, tier := range map[string]string{
		`{"type":"person/update","person_id":"anna","device_trackers":["Device_Tracker.Anna_Watch","device_tracker.anna_phone"]}`:                      "preview",
		`{"type":"person/update","person_id":"anna","name":"Anna B","device_trackers":["device_tracker.anna_phone","device_tracker.anna_watch"]}`:      "preview",
		`{"type":"person/update","person_id":"anna","name":"Anna B"}`:                                                                                  "typed", // empties both
		`{"type":"person/update","person_id":"anna","device_trackers":["device_tracker.anna_phone"]}`:                                                  "typed",
		`{"type":"person/update","person_id":"anna","device_trackers":["device_tracker.anna_phone","device_tracker.anna_watch","device_tracker.new"]}`: "typed",
		`{"type":"person/update","person_id":"ben","name":"Benjamin"}`:                                                                                 "preview",
		`{"type":"person/update","person_id":"ben","device_trackers":["device_tracker.ben_phone"]}`:                                                    "typed",
		`{"type":"person/update","person_id":"cleo","name":"Cleo B","device_trackers":[]}`:                                                             "typed", // listed without trackers: unknown
		`{"type":"person/update","person_id":"ben","device_trackers":"device_tracker.ben_phone"}`:                                                      "typed", // not a list
		`{"type":"person/create","name":"Dana"}`:                                                                                                       "preview",
		`{"type":"person/create","name":"Dana","device_trackers":[]}`:                                                                                  "preview",
		`{"type":"person/create","name":"Dana","device_trackers":["device_tracker.dana_phone"]}`:                                                       "typed",
	} {
		if exit, env := planRaw(t, paths, personItems(payload)); exit != verbExitConfirmation || env.Tier != tier {
			t.Errorf("plan %s: exit %d tier %s, want %s", payload, exit, env.Tier, tier)
		}
		before := len(sent())
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", payload}) })
		if refused := exit == verbExitRefused && strings.Contains(stderr, "which devices a person follows") && len(sent()) == before; refused != (tier == "typed") {
			t.Errorf("relay %s: exit %d, stderr %q; want refused %v", payload, exit, stderr, tier == "typed")
		}
	}
	if stderr := captureStderr(t, func() {
		runRelayCommand(paths, []string{"ws", "--data", `{"type":"person/update","person_id":"anna","name":"Anna B"}`})
	}); !strings.Contains(stderr, "without device_trackers empties them: to keep them, resend the list person/list shows") {
		t.Errorf("rename without trackers: stderr %q; want how to keep them", stderr)
	}
	if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"person/update","person_id":"zed","name":"Zed"}}]`); exit != verbExitNotFound || env.Error == nil || env.Error.Code != "NOT_FOUND" {
		t.Errorf("unknown person: exit %d %+v, want NOT_FOUND", exit, env.Error)
	}
	if stderr := captureStderr(t, func() {
		runRelayCommand(paths, []string{"ws", "--data", `{"type":"person/update","person_id":"zed","name":"Zed"}`})
	}); !strings.Contains(stderr, "which devices a person follows") {
		t.Errorf("relay, unknown person: stderr %q; want typed", stderr)
	}
	broken = true
	if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"person/update","person_id":"ben","name":"Benjamin"}}]`); exit != verbExitError || env.Plan != nil || env.Error == nil || !strings.Contains(env.Error.Message, "cannot read the person ben") {
		t.Errorf("unreadable person list: exit %d %+v, want nothing planned", exit, env.Error)
	}
}

// Home Assistant drops a tracker from a person only when it is removed: a
// rename leaves the person following an id the next tracker given it takes
// over, and a disabled tracker stops counting. Renaming or disabling one a
// person follows, or renaming one onto such an id, is typed on plan and
// relay; other trackers, other fields, and a re-enable are not.
func TestTrackerRenameTypedWhenAPersonFollowsIt(t *testing.T) {
	var broken any
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"person/list": func(map[string]any) any {
			if broken != nil {
				return broken
			}
			return map[string]any{
				"storage": []any{map[string]any{"id": "anna", "device_trackers": []any{"device_tracker.anna_phone"}}},
				"config":  []any{map[string]any{"id": "ben", "device_trackers": []any{"device_tracker.ben_watch"}}},
			}
		},
		"config/entity_registry/list": func(map[string]any) any { return []any{} },
		"config/entity_registry/get": func(body map[string]any) any {
			return map[string]any{"id": "e-" + body["entity_id"].(string), "entity_id": body["entity_id"], "name": nil}
		},
		"config/device_registry/list": func(map[string]any) any { return []any{} },
	})
	for payload, tier := range map[string]string{
		`{"type":"config/entity_registry/update","entity_id":"device_tracker.anna_phone","new_entity_id":"device_tracker.anna_phone_old"}`: "typed",
		`{"type":"config/entity_registry/update","entity_id":"device_tracker.new_phone","new_entity_id":"device_tracker.Anna_Phone"}`:      "typed",
		`{"type":"config/entity_registry/update","entity_id":"device_tracker.ben_watch","disabled_by":"user"}`:                             "typed",
		`{"type":"config/entity_registry/update","entity_id":"device_tracker.ben_watch","disabled_by":null}`:                               "preview",
		`{"type":"config/entity_registry/update","entity_id":"device_tracker.anna_phone","name":"Phone"}`:                                  "ordinary", // a rename with its undo (calmNames)
		`{"type":"config/entity_registry/update","entity_id":"device_tracker.guest","new_entity_id":"device_tracker.guest_old"}`:           "preview",
		`{"type":"config/entity_registry/update","entity_id":"sensor.anna_phone","new_entity_id":"sensor.phone"}`:                          "preview",
	} {
		if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":`+payload+`}]`); exit != map[bool]int{true: verbExitOK, false: verbExitConfirmation}[tier == "ordinary"] || env.Tier != tier {
			t.Errorf("plan %s: exit %d tier %s, want %s", payload, exit, env.Tier, tier)
		}
		before := len(sent())
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", payload}) })
		if refused := exit == verbExitRefused && strings.Contains(stderr, "which devices a person follows") && len(sent()) == before; refused != (tier == "typed") {
			t.Errorf("relay %s: exit %d, stderr %q; want refused %v", payload, exit, stderr, tier == "typed")
		}
	}
	for _, list := range []any{
		map[string]any{"config": "unreadable"},
		map[string]any{},
		map[string]any{"storage": nil, "config": []any{}},
		map[string]any{"storage": []any{nil}, "config": []any{}},
		map[string]any{"storage": []any{map[string]any{"id": "anna"}}, "config": []any{}},
	} {
		broken = list
		if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"device_tracker.guest","new_entity_id":"device_tracker.guest_old"}}]`); exit != verbExitConfirmation || env.Tier != "typed" {
			t.Errorf("person list %v: exit %d tier %s, want typed", list, exit, env.Tier)
		}
	}
}

// A calendar update is typed only by the event it reads (calendarShortens),
// so relay reads the calendar live: an update that drops dates is refused,
// one that keeps them is sent, and an unreadable calendar refuses.
func TestRelayTiersACalendarUpdateOnTheLiveEvent(t *testing.T) {
	paths, sent, setRule := calendarRelay(t, "FREQ=WEEKLY;COUNT=5")
	update := func(rule string) string {
		return `{"type":"calendar/event/update","entity_id":"calendar.family",` + calendarFuture + `,"event":{"summary":"Dentist","dtstart":"` + calendarAt + `","dtend":"2026-11-09T15:00:00+01:00","rrule":"` + rule + `"}}`
	}
	for _, tc := range []struct {
		name, live, rule string
		refused          bool
	}{
		{name: "a lower COUNT", live: "FREQ=WEEKLY;COUNT=5", rule: "FREQ=WEEKLY;COUNT=3", refused: true},
		{name: "a higher COUNT", live: "FREQ=WEEKLY;COUNT=5", rule: "FREQ=WEEKLY;COUNT=8"},
		{name: "an unreadable calendar", rule: "FREQ=WEEKLY;COUNT=8", refused: true},
	} {
		setRule(tc.live)
		before := len(sent())
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", update(tc.rule)}) })
		refused := exit == verbExitRefused && strings.Contains(stderr, "refused raw calendar/event/update") && len(sent()) == before
		if refused != tc.refused || !refused && (exit != 0 || len(sent()) != before+1) {
			t.Errorf("%s: exit %d, stderr %q, sent %v; want refused %v", tc.name, exit, stderr, sent()[before:], tc.refused)
		}
	}
}

// Disabling a device or an integration entry disables its entities, so it
// is typed on plan and relay when one of them is a device tracker a person
// follows; a re-enable, another device or entry, and a rename are not.
func TestDisablingATrackersOwnerTypedWhenAPersonFollowsIt(t *testing.T) {
	people := any(map[string]any{"storage": []any{map[string]any{"id": "anna", "device_trackers": []any{"device_tracker.anna_phone", "device_tracker.Anna_Watch"}}}, "config": []any{}})
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"person/list": func(map[string]any) any { return people },
		"config/entity_registry/list": func(map[string]any) any {
			return []any{
				map[string]any{"entity_id": "device_tracker.anna_phone", "device_id": "dphone", "config_entry_id": "emobile"},
				map[string]any{"entity_id": "device_tracker.anna_watch", "device_id": "dwatch", "config_entry_id": "ewatch"},
				map[string]any{"entity_id": "sensor.anna_phone_battery", "device_id": "dphone", "config_entry_id": "emobile"},
				map[string]any{"entity_id": "device_tracker.guest", "device_id": "dguest", "config_entry_id": "eguest"},
				map[string]any{"entity_id": "light.desk", "device_id": "dlamp", "config_entry_id": "ehue"},
			}
		},
		"config/device_registry/list": func(map[string]any) any {
			return []any{map[string]any{"id": "dphone"}, map[string]any{"id": "dguest"}, map[string]any{"id": "dlamp"}, map[string]any{"id": "dhub"},
				map[string]any{"id": "dwatch", "parent_device_id": "dhub"}}
		},
	})
	for payload, tier := range map[string]string{
		`{"type":"config/device_registry/update","device_id":"dphone","disabled_by":"user"}`:   "typed",
		`{"type":"config_entries/disable","entry_id":"emobile","disabled_by":"user"}`:          "typed",
		`{"type":"config/device_registry/update","device_id":"dphone","disabled_by":null}`:     "preview",
		`{"type":"config_entries/disable","entry_id":"emobile","disabled_by":null}`:            "preview",
		`{"type":"config/device_registry/update","device_id":"dphone","name_by_user":"Pixel"}`: "ordinary", // a rename with its undo (calmNames)
		`{"type":"config/device_registry/update","device_id":"dguest","disabled_by":"user"}`:   "preview",
		`{"type":"config/device_registry/update","device_id":"dlamp","disabled_by":"user"}`:    "preview",
		`{"type":"config_entries/disable","entry_id":"ehue","disabled_by":"user"}`:             "preview",
		`{"type":"config/device_registry/update","device_id":"dhub","disabled_by":"user"}`:     "typed", // its child device's tracker
		`{"type":"config_entries/disable","entry_id":"ewatch","disabled_by":"user"}`:           "typed", // followed in another case
	} {
		if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":`+payload+`}]`); exit != map[bool]int{true: verbExitOK, false: verbExitConfirmation}[tier == "ordinary"] || env.Tier != tier {
			t.Errorf("plan %s: exit %d tier %s, want %s", payload, exit, env.Tier, tier)
		}
		before := len(sent())
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", payload}) })
		if refused := exit == verbExitRefused && strings.Contains(stderr, "which devices a person follows") && len(sent()) == before; refused != (tier == "typed") || !refused && exit != 0 {
			t.Errorf("relay %s: exit %d, stderr %q; want refused %v", payload, exit, stderr, tier == "typed")
		}
	}
	people = map[string]any{"storage": "unreadable"}
	if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config_entries/disable","entry_id":"eguest","disabled_by":"user"}}]`); exit != verbExitConfirmation || env.Tier != "typed" {
		t.Errorf("unreadable person list: exit %d tier %s, want typed", exit, env.Tier)
	}
}
