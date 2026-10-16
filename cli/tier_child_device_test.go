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

// A pre-2026.8 merged device id resolves to its split devices (Home
// Assistant still acts on them); a child device shares its parent's access
// siblings, a via_device_id hub link never does.
func TestCompositeIdsAndFamilySiblings(t *testing.T) {
	splitGate := func(f *fakeHA) {
		f.devices = []map[string]any{{"id": "dev_split1", "parent_device_id": nil}, {"id": "dev_split2", "parent_device_id": nil}}
		f.splits = map[string]any{"old_composite": map[string]any{"split_ids": []any{"dev_split1", "dev_split2"}, "primary_id": "dev_split1"}}
		f.ent("button.gate_open", "shelly", "unknown", map[string]any{"_di": "dev_split1"})
		f.ent("cover.gate", "shelly", "closed", map[string]any{"_di": "dev_split1", "device_class": "gate"})
		f.ent("lock.gate", "nuki", "locked", map[string]any{"_di": "dev_split2"})
	}
	consumer := func(step map[string]any) func(*fakeHA) {
		return func(f *fakeHA) {
			f.ent("input_boolean.guest", "input_boolean", "off", nil)
			f.related["entity:input_boolean.guest"] = []string{"automation.guest"}
			f.automation("guest", "g1", map[string]any{
				"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}},
				"actions":  []any{step},
			})
		}
	}
	press := map[string]any{"action": "button.press", "target": map[string]any{"device_id": "old_composite"}}
	guest := []string{"input_boolean.turn_on", "input_boolean.guest"}
	family := func(coverOn, actorOn string) func(*fakeHA) {
		return func(f *fakeHA) {
			f.devices = []map[string]any{{"id": "dev_parent", "parent_device_id": nil}, {"id": "dev_child", "parent_device_id": "dev_parent"}}
			f.ent("cover.gate", "shelly", "closed", map[string]any{"_di": coverOn, "device_class": "gate"})
			f.ent("switch.gate_relay", "shelly", "off", map[string]any{"_di": actorOn})
		}
	}
	runTierCases(t, []tierCase{
		{name: "consumer pressing an old merged device id reaches the split's gate button: typed",
			setup: func(f *fakeHA) { splitGate(f); consumer(press)(f) },
			args:  guest, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
		{name: "integration service on an old merged device id sees the split's lock: typed",
			setup: func(f *fakeHA) {
				splitGate(f)
				consumer(map[string]any{"action": "nuki.lock_n_go", "target": map[string]any{"device_id": "old_composite"}})(f)
			},
			args: guest, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccess}},
		{name: "--device with an old merged device id freezes the split devices' entities",
			setup: splitGate, args: []string{"button.press", "--device", "old_composite"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
		{name: "a core without split support: an unknown device id stays a no-op",
			setup: func(f *fakeHA) { splitGate(f); f.splitsUnknown = true; consumer(press)(f) },
			args:  guest, exit: 0, tier: "ordinary", absent: []string{reasonScanFailed}},
		{name: "an unreadable split list is typed",
			setup: func(f *fakeHA) {
				splitGate(f)
				f.fail["config/device_registry/list_composite_splits"] = true
				consumer(press)(f)
			},
			args: guest, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "relay on a child, gate cover on its parent: typed",
			setup: family("dev_parent", "dev_child"), args: []string{"switch.turn_on", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
		{name: "relay on a parent, gate cover on its child: typed",
			setup: family("dev_child", "dev_parent"), args: []string{"switch.turn_on", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
		{name: "relay on one child, gate cover on another child of the same parent: typed",
			setup: func(f *fakeHA) {
				family("dev_child", "dev_child2")(f)
				f.devices = append(f.devices, map[string]any{"id": "dev_child2", "parent_device_id": "dev_parent"})
			},
			args: []string{"switch.turn_on", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
		{name: "a hub link (via_device_id) shares no siblings: ordinary",
			setup: func(f *fakeHA) {
				f.devices = []map[string]any{{"id": "dev_hub", "via_device_id": nil}, {"id": "dev_node", "via_device_id": "dev_hub"}}
				f.ent("lock.node", "zwave_js", "locked", map[string]any{"_di": "dev_node"})
				f.ent("switch.hub_led", "zwave_js", "off", map[string]any{"_di": "dev_hub"})
			},
			args: []string{"switch.turn_on", "switch.hub_led"}, exit: 0, tier: "ordinary", absent: []string{reasonDeviceAccessSibling}},
		{name: "an unreadable device registry is typed for an opening call",
			setup: func(f *fakeHA) {
				f.ent("switch.relay", "shelly", "off", map[string]any{"_di": "dev_x"})
				f.fail["config/device_registry/list"] = true
			},
			args: []string{"switch.turn_on", "switch.relay"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
	})
}

// An integration service acts on its target's own device: an access entity
// elsewhere in the parent/child family does not make it typed. The
// core-service sibling guard still spans the family.
func TestIntegrationServiceScopeIsTheOwnDevice(t *testing.T) {
	tree := func(lockOn string) func(*fakeHA) {
		return func(f *fakeHA) {
			f.devices = []map[string]any{{"id": "dev_parent", "parent_device_id": nil},
				{"id": "dev_child1", "parent_device_id": "dev_parent"}, {"id": "dev_child2", "parent_device_id": "dev_parent"}}
			f.ent("lock.door", "acme", "locked", map[string]any{"_di": lockOn})
			f.ent("switch.relay", "acme", "off", map[string]any{"_di": "dev_child2"})
		}
	}
	integration := []string{"acme.pulse", "switch.relay"}
	runTierCases(t, []tierCase{
		{name: "integration call on a child, lock on a sibling child: its normal tier",
			setup: tree("dev_child1"), args: integration, exit: 3, tier: "preview", reasons: []string{reasonIntegrationSvc}, absent: []string{reasonDeviceAccess}},
		{name: "integration call on a child, lock on the parent: its normal tier",
			setup: tree("dev_parent"), args: integration, exit: 3, tier: "preview", reasons: []string{reasonIntegrationSvc}, absent: []string{reasonDeviceAccess}},
		{name: "integration call on a child whose own device has the lock: typed",
			setup: tree("dev_child2"), args: integration, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccess}},
		{name: "core opening call on a child, lock on the parent: typed",
			setup: tree("dev_parent"), args: []string{"switch.turn_on", "switch.relay"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
		{name: "core opening call on a child, lock on a sibling child: typed",
			setup: tree("dev_child1"), args: []string{"switch.turn_on", "switch.relay"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
	})
}
