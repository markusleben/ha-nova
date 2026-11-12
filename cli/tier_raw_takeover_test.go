package main

import (
	"strings"
	"testing"
)

// The raw writes skills marked typed are rated by the CLI itself, the same
// on plan raw, ha-nova relay, and the App's gate, with no mark in the request
// (docs/work/2026-11-12-raw-typed-parity-spec.md).
func TestRawTypedParity(t *testing.T) {
	unlocks := []any{step("lock.unlock", "lock.front")}
	byTime := []any{map[string]any{"trigger": "time", "at": "07:00:00"}}
	porch := func(f *fakeHA) {
		f.areas = []map[string]any{{"area_id": "porch", "floor_id": "ground", "labels": []any{"outside"}}}
	}
	for _, tc := range []struct {
		name, payload string
		setup         func(f *fakeHA) // the home where it is typed
		other         func(f *fakeHA) // a home where the same write keeps its preview
	}{
		{"a takeover rename onto an id an automation's condition names", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","new_entity_id":"binary_sensor.porch"}`,
			func(f *fakeHA) {
				f.automation("opener", "opener", map[string]any{"triggers": byTime, "conditions": []any{map[string]any{"condition": "state", "entity_id": "binary_sensor.porch", "state": "on"}}, "actions": unlocks})
				f.related["entity:binary_sensor.porch"] = []string{"automation.opener"}
			},
			func(f *fakeHA) {
				f.automation("lamp", "lamp", map[string]any{"triggers": byTime, "conditions": []any{map[string]any{"condition": "state", "entity_id": "binary_sensor.porch", "state": "on"}}, "actions": []any{step("light.turn_on", "light.hall")}})
				f.related["entity:binary_sensor.porch"] = []string{"automation.lamp"}
			}},
		{"a takeover rename onto an id a script names", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","new_entity_id":"binary_sensor.porch"}`,
			func(f *fakeHA) {
				f.script("door", map[string]any{"wait_template": "{{ is_state('binary_sensor.porch', 'on') }}"}, step("lock.unlock", "lock.front"))
				f.related["scripts:binary_sensor.porch"] = []string{"script.door"}
			}, nil},
		{"a takeover rename onto a group member's id", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","new_entity_id":"binary_sensor.porch"}`,
			func(f *fakeHA) {
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "group.doors", "to": "on"}}, "actions": unlocks})
				f.relatedGroups = map[string][]string{"binary_sensor.porch": {"group.doors"}}
				f.related["entity:group.doors"] = []string{"automation.opener"}
			}, nil},
		{"a takeover rename onto an id a script names, whose call an automation listens for", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","new_entity_id":"binary_sensor.porch"}`,
			func(f *fakeHA) {
				f.script("hall", map[string]any{"wait_template": "{{ is_state('binary_sensor.porch', 'on') }}"}, step("light.turn_on", "light.hall"))
				f.related["scripts:binary_sensor.porch"] = []string{"script.hall"}
				listen(f, "watch", map[string]any{"domain": "light"}, step("lock.unlock", "lock.front"))
			},
			func(f *fakeHA) {
				f.script("hall", map[string]any{"wait_template": "{{ is_state('binary_sensor.porch', 'on') }}"}, step("light.turn_on", "light.hall"))
				f.related["scripts:binary_sensor.porch"] = []string{"script.hall"}
			}},
		{"an entity moved into an area a trigger targets", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","area_id":"porch"}`,
			func(f *fakeHA) {
				porch(f)
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"area_id": "porch"}}}, "actions": unlocks})
				f.related["area-automation:porch"] = []string{"automation.opener"}
			},
			func(f *fakeHA) { // there for what it does, not what triggers it
				porch(f)
				f.automation("opener", "opener", map[string]any{"triggers": byTime, "actions": []any{map[string]any{"action": "light.turn_on", "target": map[string]any{"area_id": "porch"}}, step("lock.unlock", "lock.front")}})
				f.related["area-automation:porch"] = []string{"automation.opener"}
			}},
		{"an entity moved into an area whose label a trigger targets", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","area_id":"porch"}`,
			func(f *fakeHA) {
				porch(f)
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"label_id": "outside"}}}, "actions": unlocks})
				f.related["label:outside"] = []string{"automation.opener"}
			}, nil},
		{"a device label a trigger targets", `{"type":"config/device_registry/update","device_id":"dmotion","labels":["ring"]}`,
			func(f *fakeHA) {
				f.devices = append(f.devices, map[string]any{"id": "dmotion"})
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"label_id": "ring"}}}, "actions": unlocks})
				f.related["label:ring"] = []string{"automation.opener"}
			}, func(f *fakeHA) { f.devices = append(f.devices, map[string]any{"id": "dmotion"}) }},
		{"a device moved into an area a trigger's floor covers", `{"type":"config/device_registry/update","device_id":"dmotion","area_id":"porch"}`,
			func(f *fakeHA) {
				porch(f)
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"floor_id": "ground"}}}, "actions": unlocks})
				f.related["floor:ground"] = []string{"automation.opener"}
			}, nil},
		{"an entity's cleared area: it sits where its device does", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.motion","area_id":null}`,
			func(f *fakeHA) {
				porch(f)
				f.devices = append(f.devices, map[string]any{"id": "dmotion", "area_id": "porch"})
				for _, row := range f.compact {
					if row["ei"] == "binary_sensor.motion" {
						row["ai"] = "hall" // its own area until now
					}
				}
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"area_id": "porch"}}}, "actions": unlocks})
				f.related["area-automation:porch"] = []string{"automation.opener"}
			}, nil},
		{"a child device's cleared area: it sits where its parent does", `{"type":"config/device_registry/update","device_id":"dchild","area_id":null}`,
			func(f *fakeHA) {
				porch(f)
				f.devices = []map[string]any{{"id": "dhub", "area_id": "porch"}, {"id": "dchild", "parent_device_id": "dhub", "area_id": "hall"}}
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"area_id": "porch"}}}, "actions": unlocks})
				f.related["area-automation:porch"] = []string{"automation.opener"}
			},
			func(f *fakeHA) { // it already sits there
				porch(f)
				f.devices = []map[string]any{{"id": "dhub", "area_id": "porch"}, {"id": "dchild", "parent_device_id": "dhub"}}
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"area_id": "porch"}}}, "actions": unlocks})
				f.related["area-automation:porch"] = []string{"automation.opener"}
			}},
		{"a takeover rename onto an id a group in a group holds", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","new_entity_id":"binary_sensor.porch"}`,
			func(f *fakeHA) {
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "group.house", "to": "on"}}, "actions": unlocks})
				f.relatedGroups = map[string][]string{"binary_sensor.porch": {"group.doors"}, "group.doors": {"group.house"}}
				f.related["entity:group.house"] = []string{"automation.opener"}
			}, nil},
		{"an area put on a floor a trigger targets", `{"type":"config/area_registry/update","area_id":"porch","floor_id":"ground"}`,
			func(f *fakeHA) {
				f.areas = []map[string]any{{"area_id": "porch"}}
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"floor_id": "ground"}}}, "actions": unlocks})
				f.related["floor:ground"] = []string{"automation.opener"}
			},
			func(f *fakeHA) { // already on it
				porch(f)
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"floor_id": "ground"}}}, "actions": unlocks})
				f.related["floor:ground"] = []string{"automation.opener"}
			}},
		{"a label added beside one it already has", `{"type":"config/device_registry/update","device_id":"dmotion","labels":["ring","favorite"]}`,
			func(f *fakeHA) {
				f.devices = append(f.devices, map[string]any{"id": "dmotion", "labels": []any{"favorite"}})
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"label_id": "ring"}}}, "actions": unlocks})
				f.related["label:ring"] = []string{"automation.opener"}
			},
			func(f *fakeHA) { // the label the trigger targets is already there
				f.devices = append(f.devices, map[string]any{"id": "dmotion", "labels": []any{"ring"}})
				f.automation("opener", "opener", map[string]any{"triggers": []any{map[string]any{"trigger": "binary_sensor.turned_on", "target": map[string]any{"label_id": "ring"}}}, "actions": unlocks})
				f.related["label:ring"] = []string{"automation.opener"}
			}},
	} {
		for _, typed := range []bool{true, false} {
			f := listeningHome()
			switch {
			case typed:
				tc.setup(f)
			case tc.other != nil:
				tc.other(f)
			}
			want := map[bool]string{true: "typed", false: "preview"}[typed]
			paths, _, sent := listeningRelay(t, f)
			if exit, env := planRaw(t, paths, "["+rawItem("", tc.payload)+"]"); exit != verbExitConfirmation || env.Tier != want || typed != strings.Contains(reasonCodes(env), reasonAccessLock) {
				t.Errorf("%s (typed %v): plan exit %d tier %s reasons %s %+v", tc.name, typed, exit, env.Tier, reasonCodes(env), env.Error)
			}
			check, err := gateVerdict("ws", []byte(tc.payload), func() (*relaySession, error) { return newRelaySession(paths, verbOptions{}) })
			if err != nil || check.Verdict != map[bool]gateDecision{true: gateTyped, false: gatePass}[typed] {
				t.Errorf("%s (typed %v): gate %+v %v", tc.name, typed, check, err)
			}
			before := sent()
			exit := 0
			captureStderr(t, func() {
				captureStdout(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", tc.payload}) })
			})
			if typed != (exit == verbExitRefused) || typed != (sent() == before) {
				t.Errorf("%s (typed %v): relay exit %d, sent %d", tc.name, typed, exit, sent()-before)
			}
		}
	}
}

// A read the rating needs that fails is typed on every route.
func TestRawTypedParityFailsClosed(t *testing.T) {
	for _, payload := range []string{
		`{"type":"config/entity_registry/update","entity_id":"binary_sensor.side_door","new_entity_id":"binary_sensor.porch"}`,
		`{"type":"config/device_registry/update","device_id":"dmotion","labels":["ring"]}`,
	} {
		f := listeningHome()
		f.devices = append(f.devices, map[string]any{"id": "dmotion"})
		f.fail["search/related"] = true
		paths, _, _ := listeningRelay(t, f)
		if exit, env := planRaw(t, paths, "["+rawItem("", payload)+"]"); exit != verbExitConfirmation || env.Tier != "typed" {
			t.Errorf("%s: plan exit %d tier %s", payload, exit, env.Tier)
		}
		if check, _ := gateVerdict("ws", []byte(payload), func() (*relaySession, error) { return newRelaySession(paths, verbOptions{}) }); check.Verdict != gateTyped {
			t.Errorf("%s: gate %+v", payload, check)
		}
	}
	f := listeningHome()
	f.fail["config/area_registry/list"] = true
	paths, _, _ := listeningRelay(t, f)
	if exit, env := planRaw(t, paths, "["+rawItem("", `{"type":"config/device_registry/update","device_id":"dmotion","area_id":"porch"}`)+"]"); exit != verbExitConfirmation || env.Tier != "typed" {
		t.Errorf("an unreadable area list: plan exit %d tier %s", exit, env.Tier)
	}
}
