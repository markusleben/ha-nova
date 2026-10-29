package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// The skill's typed flag only raises a step, and the device skill's Access
// Check is computed: a new id, area, or label carried onto a lock, alarm
// panel, or door (or a device with one), and a person's trackers.
func TestPlanRawTypedFlagAndAccessCheck(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "lock.front", "device_id": "dlock"}, map[string]any{"entity_id": "sensor.door_battery", "device_id": "dlock"},
				map[string]any{"entity_id": "light.desk", "device_id": "dlamp"}}
		},
	})
	for payload, tier := range map[string]string{
		`{"type":"input_boolean/update","input_boolean_id":"guest","name":"Guest"}`:                     "preview",
		`{"type":"config/entity_registry/update","entity_id":"lock.front","name":"Front door"}`:         "preview",
		`{"type":"config/entity_registry/update","entity_id":"lock.front","area_id":"hall"}`:            "typed",
		`{"type":"config/entity_registry/update","entity_id":"lock.front","new_entity_id":"lock.door"}`: "typed",
		`{"type":"config/entity_registry/update","entity_id":"sensor.door_battery","labels":["door"]}`:  "typed",
		`{"type":"config/entity_registry/update","entity_id":"light.desk","area_id":"office"}`:          "preview",
		`{"type":"person/update","person_id":"anna","device_trackers":["device_tracker.new_phone"]}`:    "typed",
		`{"type":"person/update","person_id":"anna","name":"Anna"}`:                                     "preview",
	} {
		if exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":`+payload+`}]`); exit != verbExitConfirmation || env.Tier != tier {
			t.Errorf("%s: exit %d tier %s, want %s", payload, exit, env.Tier, tier)
		}
	}
	for items, tier := range map[string]string{
		`[{"endpoint":"ws","payload":{"type":"input_boolean/update","input_boolean_id":"guest","name":"G"},"typed":true}]`:  "typed",
		`[{"endpoint":"ws","payload":{"type":"input_boolean/update","input_boolean_id":"guest","name":"G"},"typed":false}]`: "preview",
		`[{"endpoint":"ws","payload":{"type":"input_boolean/delete","input_boolean_id":"guest"},"typed":false}]`:            "typed",
	} {
		if exit, env := planRaw(t, paths, items); exit != verbExitConfirmation || env.Tier != tier {
			t.Errorf("%s: exit %d tier %s, want %s", items, exit, env.Tier, tier)
		}
	}
}

// A config copy a plan deletes must be listed, a copy gone since the
// preview is drift, and each delete is checked gone before the next.
func TestPlanRawSnapshotDeletes(t *testing.T) {
	var mu sync.Mutex
	files := map[string]int{"automations/a.json.gz": 10, "automations/b.json.gz": 10} // file → bytes
	sticky := false                                                                   // a delete Home Assistant answers but the copy stays
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"backups list": func(map[string]any) any {
			mu.Lock()
			defer mu.Unlock()
			var rows []any
			for file, bytes := range files {
				rows = append(rows, map[string]any{"file": file, "category": "automations", "bytes": bytes})
			}
			return rows
		},
		"backups delete": func(body map[string]any) any {
			mu.Lock()
			defer mu.Unlock()
			if !sticky {
				delete(files, body["file"].(string))
			}
			return map[string]any{"deleted": body["file"]}
		},
	})
	both := `[{"endpoint":"backups","payload":{"action":"delete","file":"automations/a.json.gz"}},{"endpoint":"backups","payload":{"action":"delete","file":"automations/b.json.gz"}}]`
	if exit, env := planRaw(t, paths, `[{"endpoint":"backups","payload":{"action":"delete","file":"automations/x.json.gz"}}]`); exit != verbExitRefused || !strings.Contains(env.Error.Message, "not in the snapshot store") {
		t.Fatalf("unlisted: exit %d %+v", exit, env.Error)
	}
	_, env := planRaw(t, paths, both)
	mu.Lock()
	files["automations/b.json.gz"] = 11
	mu.Unlock()
	if exit, drift := applyRaw(t, paths, env); exit != verbExitDrift || len(sent()) != 0 {
		t.Fatalf("a copy changed since the preview: exit %d %+v sent %v", exit, drift.Error, sent())
	}
	_, env = planRaw(t, paths, both)
	mu.Lock()
	delete(files, "automations/b.json.gz")
	mu.Unlock()
	if exit, gone := applyRaw(t, paths, env); exit != verbExitRefused || len(sent()) != 0 {
		t.Fatalf("a copy gone since the preview: exit %d %+v sent %v", exit, gone.Error, sent())
	}
	mu.Lock()
	files["automations/b.json.gz"], sticky = 10, true
	mu.Unlock()
	_, env = planRaw(t, paths, both)
	exit, applied := applyRaw(t, paths, env)
	if exit != verbExitUnverified || statuses(rawOutcomes(t, applied)) != "failed,not_sent" || len(sent()) != 1 || applied.Say != sayNotDone("it didn't go through") {
		t.Fatalf("a copy still listed: exit %d %+v %s sent %v say %q", exit, applied.Error, applied.Result, sent(), applied.Say)
	}
}

// Results count the steps that went through: a snapshot left out by
// --no-snapshot is not one. A rejected core step carries Home Assistant's
// reason.
func TestPlanRawCountsAndReasons(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"backups save": func(map[string]any) any { return fakeRelayFailure{"SNAPSHOT_STORE_FULL", "full"} },
		"POST /api/config/config_entries/flow/abc": func(map[string]any) any {
			return map[string]any{"status": 400, "body": map[string]any{"message": "Invalid host"}}
		},
	})
	_, env := planRaw(t, paths, `[{"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"auto-x","data":{}}},
		{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow/abc","body":{"host":"x","password":"hunter2","psk":"s3cret","advanced":{"password":"inner"}}}}]`)
	if strings.Contains(env.Say, "hunter2") || strings.Contains(env.Say, "s3cret") || strings.Contains(env.Say, "inner") || !strings.Contains(env.Say, "password set") ||
		!strings.Contains(env.Say, "psk set") || !strings.Contains(env.Say, "advanced set") || !strings.Contains(env.Say, "host “x”") {
		t.Fatalf("flow step say %q", env.Say)
	}
	if exit, failed := applyRaw(t, paths, env); exit != verbExitError || failed.Error.Code != "SNAPSHOT_FAILED" {
		t.Fatalf("snapshot: exit %d %+v", exit, failed.Error)
	}
	exit, applied := applyRaw(t, paths, env, "--no-snapshot")
	items := rawOutcomes(t, applied)
	if exit != verbExitUnverified || statuses(items) != "skipped,rejected" || !strings.Contains(items[1].Error, "Invalid host") ||
		applied.Say != sayNotDone("Home Assistant refused it") || strings.Contains(applied.Error.Message, "went through") {
		t.Fatalf("apply: exit %d %+v %s say %q", exit, applied.Error, applied.Result, applied.Say)
	}
	_, refused := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"get_states"}}]`)
	if !strings.Contains(refused.Error.Next, "read it with ha_read") || !strings.Contains(refused.Error.Next, "then plan") {
		t.Fatalf("read next %q", refused.Error.Next)
	}
}

// A helper whose state can't be read before its change stops apply before
// anything is sent; --no-snapshot goes on without undo, and says so.
func TestPlanRawHelperUndoFailsLoud(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"input_number/list": func(map[string]any) any { return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "boom"} },
	})
	_, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/create","name":"A"}},{"endpoint":"ws","payload":{"type":"input_number/update","input_number_id":"heat","max":35}}]`)
	exit, failed := applyRaw(t, paths, env)
	if exit != verbExitError || failed.Error.Code != "UNDO_FAILED" || len(sent()) != 0 || !strings.Contains(failed.Error.Next, "--no-snapshot") {
		t.Fatalf("undo failure: exit %d %+v sent %v", exit, failed.Error, sent())
	}
	exit, applied := applyRaw(t, paths, env, "--no-snapshot")
	if exit != 0 || !strings.HasSuffix(applied.Say, "No copy or undo was saved, as you chose.") || !strings.Contains(fmtNotices(applied.Notices), "not revertible") || len(sent()) != 2 {
		t.Fatalf("no-snapshot: exit %d say %q notices %v sent %v", exit, applied.Say, applied.Notices, sent())
	}
}

func fmtNotices(notices []verbNotice) string {
	raw, _ := json.Marshal(notices)
	return string(raw)
}

// Names, never ids alone, wherever one read gets them.
func TestPlanRawSayNames(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"person/list": func(map[string]any) any {
			return map[string]any{"storage": []any{map[string]any{"id": "anna", "name": "Anna"}}, "config": []any{}}
		},
		"hacs/repositories/list": func(map[string]any) any {
			return []any{map[string]any{"id": "123", "full_name": "piitaya/lovelace-mushroom"}}
		},
		"backup/info": func(map[string]any) any {
			return map[string]any{"backups": []any{map[string]any{"backup_id": "b1", "name": "Before update"}}}
		},
		"lovelace/resources": func(map[string]any) any { return []any{map[string]any{"id": "r1", "url": "/hacsfiles/mushroom.js"}} },
		"lovelace/dashboards/list": func(map[string]any) any {
			return []any{map[string]any{"id": "d1", "url_path": "home-x", "title": "Upstairs"}}
		},
		"config_entries/flow/progress": func(map[string]any) any {
			return []any{map[string]any{"flow_id": "f1", "handler": "hue"}}
		},
		"GET /api/calendars/calendar.family": func(map[string]any) any {
			return map[string]any{"status": 200, "body": []any{map[string]any{"uid": "e1", "summary": "Dentist", "start": map[string]any{"date": "2026-11-02"}}}}
		},
		"GET /api/states/calendar.family": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"entity_id": "calendar.family", "state": "off", "attributes": map[string]any{"friendly_name": "Family"}}}
		},
	})
	for payload, want := range map[string]string{
		`{"type":"person/update","person_id":"anna","name":"Anna B"}`:                                                                             "the person “Anna”",
		`{"type":"hacs/repository/download","repository":"123","version":"v4.1.0"}`:                                                               "“piitaya/lovelace-mushroom” and version “v4.1.0”",
		`{"type":"backup/delete","backup_id":"b1"}`:                                                                                               "the backup “Before update”",
		`{"type":"lovelace/resources/delete","resource_id":"r1"}`:                                                                                 "“/hacsfiles/mushroom.js”",
		`{"type":"lovelace/dashboards/update","dashboard_id":"d1","title":"Up"}`:                                                                  "dashboard “Upstairs”",
		`{"type":"calendar/event/delete","entity_id":"calendar.family","uid":"e1","recurrence_id":"20261102","recurrence_range":"THISANDFUTURE"}`: "the event “Dentist” (2 November 2026): the one on 2 November 2026 and every later one from the calendar “Family”",
	} {
		_, env := planRaw(t, paths, `[{"endpoint":"ws","payload":`+payload+`}]`)
		if !strings.Contains(env.Say, want) {
			t.Errorf("%s: say %q, want %q", payload, env.Say, want)
		}
	}
	for item, want := range map[string]string{
		`{"endpoint":"core","payload":{"method":"POST","path":"/api/config/config_entries/flow/f1","body":{"next_step_id":"manual"}}}`: "the setup of “hue” in progress: next step id “manual”",
		`{"endpoint":"backups","payload":{"action":"prune","keep_named":false}}`:                                                       "older than 30 days or beyond the newest 100, named copies too",
		`{"endpoint":"backups","payload":{"action":"prune"}}`:                                                                          "named copies are kept",
	} {
		_, env := planRaw(t, paths, "["+item+"]")
		if !strings.Contains(env.Say, want) || strings.Contains(env.Say, "keep_named") {
			t.Errorf("%s: say %q, want %q", item, env.Say, want)
		}
	}
}

func TestPlanRawSubentryReconfigureNamesTarget(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"config_entries/subentries/list": func(map[string]any) any {
			return []any{map[string]any{"subentry_id": "s1", "title": "Hall sensor"}, map[string]any{"subentry_id": "s2", "title": "Kitchen sensor"}}
		},
	})
	c := startMCP(t, paths)
	mcpInit(c)
	for id, name := range map[string]string{"s1": "Hall sensor", "s2": "Kitchen sensor", "unknown": "unknown"} {
		payload := map[string]any{"method": "POST", "path": "/api/config/config_entries/subentries/flow", "body": map[string]any{"handler": []any{"Ehue", "sensor"}, "subentry_id": id}}
		items := []any{map[string]any{"endpoint": "core", "payload": payload}}
		exit, env := planRaw(t, paths, string(mustJSON(items)))
		chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": items}))
		if exit != verbExitConfirmation || !strings.Contains(env.Say, name) || code != "" || !strings.Contains(fmt.Sprint(chat["say"]), name) {
			t.Errorf("subentry %s: exit %d CLI %s chat %v", id, exit, env.Say, chat)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning sent %v", got)
	}
}

// On the phone, every typed step comes first, by number, and survives the
// cut to the notification's length; the other steps are counted.
func TestPlanRawPhoneAskKeepsTypedSteps(t *testing.T) {
	paths, _ := rawRelay(t, nil)
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	items := []rawRequest{}
	for _, payload := range []string{`{"action":"save","category":"helpers","name":"auto-helpers","data":{}}`} {
		items = append(items, rawRequest{Endpoint: "backups", Payload: json.RawMessage(payload)})
	}
	for _, id := range []string{"guest_mode_living_room", "holiday_lights_garden", "night_mode_upstairs"} {
		items = append(items, rawRequest{Endpoint: "ws", Payload: json.RawMessage(`{"type":"input_boolean/delete","input_boolean_id":"` + id + `"}`)})
	}
	env := verbEnvelope{}
	_, plan, err := computeRawPlan(s, &env, items)
	if err != nil {
		t.Fatal(err)
	}
	ask := phonePlanAsk("en", sayAsk(newTierEngine(s), &env, plan, ""), false)
	for i, id := range []string{"guest_mode_living_room", "holiday_lights_garden", "night_mode_upstairs"} {
		if want := "Step " + string(rune('2'+i)) + ": Delete the toggle helper “" + id + "”"; !strings.Contains(ask.body, want) {
			t.Errorf("phone body lacks %q: %q", want, ask.body)
		}
	}
	if !strings.Contains(ask.body, "And 1 other step") || strings.Index(ask.body, "Step 2") > strings.Index(ask.body, "This deletes") {
		t.Fatalf("phone body %q", ask.body)
	}
}

// Decision 3: a HACS component's update.install installs third-party code
// like hacs/repository/download, so the call takes the code; other updates
// keep their tier.
func TestHACSUpdateInstallIsTyped(t *testing.T) {
	f := newFakeHA()
	f.ent("update.mushroom_update", "hacs", "on", map[string]any{"friendly_name": "Mushroom update"})
	f.ent("update.shelly_firmware", "shelly", "on", map[string]any{"friendly_name": "Shelly firmware"})
	f.services = append(f.defaultServices(), map[string]any{"domain": "update", "services": map[string]any{
		"install": map[string]any{"target": map[string]any{}}, "skip": map[string]any{"target": map[string]any{}}}})
	h := newWriteHarness(t, f)
	hacs := h.plan("update.install", "update.mushroom_update", "--skill", "ha-nova:updates")
	if hacs.Tier != "typed" || !strings.Contains(fmt.Sprint(hacs.Reasons), reasonHACSInstall) || !strings.Contains(hacs.Say, "installs code from HACS") {
		t.Fatalf("HACS update: tier %s reasons %v say %q", hacs.Tier, hacs.Reasons, hacs.Say)
	}
	other := h.plan("update.install", "update.shelly_firmware", "--skill", "ha-nova:updates")
	if other.Tier == "typed" || strings.Contains(fmt.Sprint(other.Reasons), reasonHACSInstall) {
		t.Fatalf("firmware update: tier %s reasons %v", other.Tier, other.Reasons)
	}
	if skip := h.plan("update.skip", "update.mushroom_update", "--skill", "ha-nova:updates"); strings.Contains(fmt.Sprint(skip.Reasons), reasonHACSInstall) {
		t.Fatalf("skip: %v", skip.Reasons)
	}
}

// PR 1's guard refuses a cover's class change, so a plan never carries
// one (a cover's class decides whether opening it needs the code).
func TestPlanRawRefusesCoverClassChange(t *testing.T) {
	paths, sent := rawRelay(t, nil)
	exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"cover.garage","device_class":"curtain"}}]`)
	if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "Show as") || len(sent()) != 0 {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
}
