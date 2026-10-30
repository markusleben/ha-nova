package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Removing or reconfiguring the integration, subentry, or entity behind a
// switch that a switch_as_x lock or cover runs on takes that lock or cover
// out of service, like disabling it (TestRawIntegrationDeviceLessWrapperSources).
func TestRawSourceSwitchRemovalKeepsWrappedAccess(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "light.hall", "config_entry_id": "Ehue"},
				map[string]any{"entity_id": "switch.gate_relay", "config_entry_id": "Esource", "device_id": "dgate"},
				map[string]any{"entity_id": "cover.gate", "config_entry_id": "Ewrapper", "device_id": "dgate", "platform": "switch_as_x"}}
		},
	})
	c := startMCP(t, paths)
	mcpInit(c)
	for _, item := range []map[string]any{
		{"endpoint": "core", "payload": map[string]any{"method": "DELETE", "path": "/api/config/config_entries/entry/Esource"}},
		{"endpoint": "ws", "payload": map[string]any{"type": "config_entries/subentries/delete", "entry_id": "Esource", "subentry_id": "S1"}},
		{"endpoint": "core", "payload": map[string]any{"method": "POST", "path": "/api/config/config_entries/subentries/flow", "body": map[string]any{"handler": []any{"Esource", "device"}}}},
		{"endpoint": "core", "payload": map[string]any{"method": "POST", "path": "/api/config/config_entries/options/flow", "body": map[string]any{"handler": "Esource"}}},
		{"endpoint": "ws", "payload": map[string]any{"type": "config/entity_registry/remove", "entity_id": "switch.gate_relay"}},
	} {
		body, _ := json.Marshal([]any{item})
		exit, env := planRaw(t, paths, string(body))
		chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{item}}))
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "cover.gate") || code != "REFUSED" {
			t.Errorf("%s: exit %d CLI %+v chat %v", body, exit, env.Error, chat)
		}
	}
	exit, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"DELETE","path":"/api/config/config_entries/entry/Ehue"}}]`)
	if exit != verbExitConfirmation || env.Tier != "typed" {
		t.Errorf("an entry without access: exit %d %+v", exit, env.Error)
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning sent %v", got)
	}
}

// A new area or label on a device with a lock, like on the lock itself,
// can change what an area-targeted unlock acts on: typed.
func TestRawDeviceAreaOnAccessDeviceIsTyped(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "lock.front", "device_id": "dlock"}, map[string]any{"entity_id": "light.hall", "device_id": "dlight"}}
		},
	})
	for payload, want := range map[string]string{
		`{"type":"config/device_registry/update","device_id":"dlock","area_id":"garage"}`:    "typed",
		`{"type":"config/device_registry/update","device_id":"dlock","labels":["outside"]}`:  "typed",
		`{"type":"config/device_registry/update","device_id":"dlock","name_by_user":"Door"}`: "preview",
		`{"type":"config/device_registry/update","device_id":"dlight","area_id":"garage"}`:   "preview",
	} {
		exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":`+payload+`}]`)
		if exit != verbExitConfirmation || env.Tier != want {
			t.Errorf("%s: exit %d tier %s, want %s (%+v)", payload, exit, env.Tier, want, env.Error)
		}
	}
}

// A device delete whose registries can't be read after the guard plans
// nothing: its preview couldn't say what goes, and drift couldn't see more.
func TestRawDeviceDeleteUnreadableScopePlansNothing(t *testing.T) {
	reads := 0
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"config/device_registry/list": func(map[string]any) any {
			if reads++; reads > 1 {
				return map[string]any{}
			}
			return []any{map[string]any{"id": "d1", "config_entry_id": "E1"}}
		},
	})
	exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/device_registry/remove","device_id":"d1"}}]`)
	if exit == verbExitConfirmation || env.Plan != nil || env.Error == nil || !strings.Contains(env.Error.Message, "nothing was planned") {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("sent %v", got)
	}
}

// Two channels of one relay device: the wrapper names its own source, so
// removing the other channel is free and removing its own is refused.
func TestRawSourceSwitchOtherChannelStaysFree(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "switch.ch1", "config_entry_id": "Esource", "device_id": "drelay"},
				map[string]any{"entity_id": "switch.ch2", "config_entry_id": "Esource", "device_id": "drelay"},
				map[string]any{"entity_id": "cover.gate", "config_entry_id": "Ewrapper", "device_id": "drelay", "platform": "switch_as_x"}}
		},
		"config/entity_registry/get": func(body map[string]any) any {
			return map[string]any{"options": map[string]any{"switch_as_x": map[string]any{"entity_id": "switch.ch1"}}}
		},
	})
	for source, refused := range map[string]bool{"switch.ch1": true, "switch.ch2": false} {
		exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/entity_registry/remove","entity_id":"`+source+`"}}]`)
		if refused && (exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "cover.gate")) || !refused && exit != verbExitConfirmation {
			t.Errorf("%s: exit %d %+v", source, exit, env.Error)
		}
	}
}

// A rooms plan never moves a device with a lock, nor a hub whose child
// device without a room of its own has one: that move is a raw device
// update, which takes the user's own confirmation. A failed read refuses.
func TestRoomsPlanRefusesAccessDevices(t *testing.T) {
	list := any([]any{map[string]any{"entity_id": "lock.front", "device_id": "dlock"}, map[string]any{"entity_id": "light.hall", "device_id": "dlight"},
		map[string]any{"entity_id": "lock.child", "device_id": "dchild"}, map[string]any{"entity_id": "lock.placed", "device_id": "dplaced"}})
	reg := roomsRegistry{devices: map[string]struct{ area, name, disabled string }{"dchild": {name: "Child Lock"}, "dhub": {name: "Hub"}, "dplaced": {area: "hall"}},
		parent: map[string]string{"dchild": "dhub", "dplaced": "dhub2"}}
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{"config/entity_registry/list": func(map[string]any) any { return list }})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if err := roomsAccessRefusal(s, roomsRequest{Items: []roomsItem{{DeviceID: "dlight"}, {DeviceID: "dhub2"}}}, reg); err != nil {
		t.Fatalf("a light, a hub whose lock has its own room: %v", err)
	}
	if err := roomsAccessRefusal(s, roomsRequest{Items: []roomsItem{{DeviceID: "dlight"}, {DeviceID: "dlock"}}}, reg); err == nil || !strings.Contains(err.Error(), "lock.front") {
		t.Fatalf("a lock: %v", err)
	}
	if err := roomsAccessRefusal(s, roomsRequest{Items: []roomsItem{{DeviceID: "dhub"}}}, reg); err == nil || !strings.Contains(err.Error(), "“Child Lock” has lock.child (a lock, alarm panel, or door) and takes the room of “Hub”") {
		t.Fatalf("a hub whose lock takes its room: %v", err)
	}
	if err := roomsAccessRefusal(s, roomsRequest{Items: []roomsItem{{DeviceID: "dhub"}, {DeviceID: "dchild"}}}, reg); err == nil || !strings.HasPrefix(err.Error(), "a device in this plan has lock.child") {
		t.Fatalf("the lock itself in the plan: %v", err)
	}
	list = nil
	s2, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.close()
	if err := roomsAccessRefusal(s2, roomsRequest{Items: []roomsItem{{DeviceID: "dlight"}}}, reg); err == nil || !strings.Contains(err.Error(), "nothing was planned") {
		t.Fatalf("unreadable: %v", err)
	}
}
