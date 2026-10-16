package main

import (
	"strings"
	"testing"
)

// Device rows in the production shape of config/device_registry/list (Home
// Assistant 2026.9+): a child device row carries parent_device_id; a regular
// device row carries via_device_id (a hub link, not a child).
func parentWithGateChild(f *fakeHA) {
	f.devices = []map[string]any{
		{"id": "dev_parent", "name": "Gate controller", "via_device_id": nil, "config_entries": []any{"e1"}, "area_id": "yard"},
		{"id": "dev_child", "parent_device_id": "dev_parent", "config_entry_id": "e1", "config_subentry_id": nil,
			"area_id": nil, "name": "Gate channel", "name_by_user": nil, "labels": []any{}, "identifiers": []any{}, "disabled_by": nil},
	}
	f.ent("button.gate_open", "shelly", "unknown", map[string]any{"_di": "dev_child"})
	f.ent("cover.gate", "shelly", "closed", map[string]any{"_di": "dev_child", "device_class": "gate"})
	f.ent("sensor.controller_temp", "shelly", "31", map[string]any{"_di": "dev_parent"})
}

func TestChildDevicesAndStoredDeviceTargets(t *testing.T) {
	members := func(want string) func(*testing.T, verbTestEnvelope) {
		return func(t *testing.T, env verbTestEnvelope) {
			var got []string
			for _, m := range env.Members {
				got = append(got, m.EntityID)
			}
			if strings.Join(got, ",") != want {
				t.Fatalf("members %v, want %s", got, want)
			}
		}
	}
	runTierCases(t, []tierCase{
		{name: "automation pressing a parent device reaches the child's gate button: typed",
			setup: func(f *fakeHA) {
				parentWithGateChild(f)
				f.automation("arrive", "a1", map[string]any{"actions": []any{
					map[string]any{"action": "button.press", "target": map[string]any{"device_id": "dev_parent"}}}})
			},
			args: []string{"automation.trigger", "automation.arrive"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling},
			check: members("button.gate_open")},
		{name: "consumer opening covers of a parent device reaches the child's gate: typed",
			setup: func(f *fakeHA) {
				parentWithGateChild(f)
				f.ent("input_boolean.guest", "input_boolean", "off", nil)
				f.related["entity:input_boolean.guest"] = []string{"automation.guest"}
				f.automation("guest", "g1", map[string]any{
					"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}},
					"actions":  []any{map[string]any{"action": "cover.open_cover", "target": map[string]any{"device_id": "dev_parent"}}},
				})
			},
			args: []string{"input_boolean.turn_on", "input_boolean.guest"}, exit: 3, tier: "typed", reasons: []string{reasonAccessCover}},
		{name: "a hub link (via_device_id) is not a child: the node's button is not pressed",
			setup: func(f *fakeHA) {
				f.devices = []map[string]any{{"id": "dev_hub", "via_device_id": nil}, {"id": "dev_node", "via_device_id": "dev_hub"}}
				f.ent("button.gate_open", "zwave_js", "unknown", map[string]any{"_di": "dev_node"})
				f.ent("lock.gate", "zwave_js", "locked", map[string]any{"_di": "dev_node"})
				f.script("hub", map[string]any{"action": "button.press", "target": map[string]any{"device_id": "dev_hub"}})
			},
			args: []string{"script.turn_on", "script.hub"}, exit: 3, tier: "preview", reasons: []string{reasonStoredMembers}, absent: []string{reasonDeviceAccessSibling}},
		{name: "a stored service call on a device skips its hidden and categorized entities",
			setup: func(f *fakeHA) {
				f.ent("light.desk", "hue", "on", map[string]any{"_di": "dev_desk"})
				f.ent("light.desk_raw", "hue", "on", map[string]any{"_di": "dev_desk", "_hb": true})
				f.ent("light.desk_status", "hue", "on", map[string]any{"_di": "dev_desk", "_ec": 1})
				f.script("off", map[string]any{"action": "light.turn_off", "target": map[string]any{"device_id": "dev_desk"}})
			},
			args: []string{"script.turn_on", "script.off"}, exit: 3, tier: "preview", check: members("light.desk")},
		{name: "a device-form step on the same device keeps its hidden entity",
			setup: func(f *fakeHA) {
				f.ent("light.desk_raw", "hue", "on", map[string]any{"_di": "dev_desk", "_hb": true})
				f.script("off", map[string]any{"device_id": "dev_desk", "domain": "light", "type": "turn_off", "entity_id": "7c1d"})
			},
			args: []string{"script.turn_on", "script.off"}, exit: 3, tier: "preview", check: members("light.desk_raw")},
		{name: "an integration step on a device keeps its hidden entities",
			setup: func(f *fakeHA) {
				f.ent("light.desk_raw", "hue", "on", map[string]any{"_di": "dev_desk", "_hb": true})
				f.script("hue", map[string]any{"action": "hue.activate_scene", "target": map[string]any{"device_id": "dev_desk"}})
			},
			args: []string{"script.turn_on", "script.hue"}, exit: 3, tier: "preview", check: members("light.desk_raw")},
	})
}
