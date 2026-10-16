package main

import (
	"strings"
	"testing"
)

// Device siblings (spec §2.2 step 2): a core-service call on an entity whose
// device also carries a lock, alarm panel, or access cover may be the opener.
// Press, switch on/toggle, and cover open are typed; every other call keeps
// its own tier.
func TestDeviceAccessSiblingOnEveryPath(t *testing.T) {
	gate := func(f *fakeHA) { // a relay board driving a gate: relay, button, light, gate cover
		f.ent("switch.gate_relay", "shelly", "off", map[string]any{"_di": "dev_gate", "friendly_name": "Gate relay"})
		f.ent("button.gate_open", "shelly", "unknown", map[string]any{"_di": "dev_gate"})
		f.ent("light.gate_lamp", "shelly", "off", map[string]any{"_di": "dev_gate"})
		f.ent("cover.gate", "shelly", "closed", map[string]any{"_di": "dev_gate", "device_class": "gate"})
	}
	with := func(steps ...func(*fakeHA)) func(*fakeHA) {
		return func(f *fakeHA) {
			for _, s := range steps {
				s(f)
			}
		}
	}
	typed := []string{reasonDeviceAccessSibling}
	cases := []tierCase{
		// Typed: the call can energize the opener.
		{name: "button press named directly", setup: gate, args: []string{"button.press", "button.gate_open"}, exit: 3, tier: "typed", reasons: typed},
		{name: "relay switched on", setup: gate, args: []string{"switch.turn_on", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "relay toggled", setup: gate, args: []string{"switch.toggle", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "generic homeassistant.turn_on on the relay", setup: gate, args: []string{"homeassistant.turn_on", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "generic homeassistant.toggle on the relay", setup: gate, args: []string{"homeassistant.toggle", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "a shutter raised by position on a lock's device",
			setup: func(f *fakeHA) {
				f.ent("cover.hatch", "shelly", "closed", map[string]any{"_di": "dev_door", "device_class": "shutter"})
				f.ent("lock.door", "shelly", "locked", map[string]any{"_di": "dev_door"})
			},
			args: []string{"cover.set_cover_position", "cover.hatch", "--set", "position=40"}, exit: 3, tier: "typed", reasons: typed},
		{name: "a shutter closed on a lock's device: ordinary",
			setup: func(f *fakeHA) {
				f.ent("cover.hatch", "shelly", "open", map[string]any{"_di": "dev_door", "device_class": "shutter"})
				f.ent("lock.door", "shelly", "locked", map[string]any{"_di": "dev_door"})
			},
			args: []string{"cover.close_cover", "cover.hatch"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "a switch_as_x light wrapping the gate relay, turned on",
			setup: with(gate, func(f *fakeHA) { f.ent("light.gate_relay", "switch_as_x", "off", map[string]any{"_di": "dev_gate"}) }),
			args:  []string{"light.turn_on", "light.gate_relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "a switch_as_x fan wrapping the gate relay, toggled via homeassistant",
			setup: with(gate, func(f *fakeHA) { f.ent("fan.gate_relay", "switch_as_x", "off", map[string]any{"_di": "dev_gate"}) }),
			args:  []string{"homeassistant.toggle", "fan.gate_relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "relay via --match", setup: gate, args: []string{"switch.turn_on", "--match", "gate relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "relay via the one-switch device", setup: gate, args: []string{"switch.turn_on", "--device", "dev_gate"}, exit: 3, tier: "typed", reasons: typed, absent: []string{reasonExpanded}},
		{name: "relay via the one-switch area",
			setup: with(gate, func(f *fakeHA) { f.related["area:yard"] = []string{"switch.gate_relay", "cover.gate"} }),
			args:  []string{"switch.turn_on", "--area", "yard"}, exit: 3, tier: "typed", reasons: typed},
		{name: "door buzzer next to a lock",
			setup: func(f *fakeHA) {
				f.ent("switch.buzzer", "zwave_js", "off", map[string]any{"_di": "dev_door"})
				f.ent("lock.door", "zwave_js", "locked", map[string]any{"_di": "dev_door"})
			},
			args: []string{"switch.turn_on", "switch.buzzer"}, exit: 3, tier: "typed", reasons: typed},
		{name: "relay next to an alarm panel",
			setup: func(f *fakeHA) {
				f.ent("switch.hub_out", "ajax", "off", map[string]any{"_di": "dev_hub"})
				f.ent("alarm_control_panel.home", "ajax", "armed_away", map[string]any{"_di": "dev_hub"})
			},
			args: []string{"switch.turn_on", "switch.hub_out"}, exit: 3, tier: "typed", reasons: typed},
		{name: "sibling cover without a class counts as access",
			setup: func(f *fakeHA) {
				f.ent("switch.relay", "shelly", "off", map[string]any{"_di": "dev_x"})
				f.ent("cover.thing", "shelly", "closed", map[string]any{"_di": "dev_x"})
			},
			args: []string{"switch.turn_on", "switch.relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "disabled sibling cover still counts (the hardware still opens)",
			setup: func(f *fakeHA) {
				f.ent("switch.relay", "shelly", "off", map[string]any{"_di": "dev_x"})
				f.full = append(f.full, map[string]any{"entity_id": "cover.garage", "platform": "shelly", "device_id": "dev_x", "disabled_by": "user"})
			},
			args: []string{"switch.turn_on", "switch.relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "unreadable sibling cover state counts as access",
			setup: func(f *fakeHA) {
				f.ent("switch.relay", "shelly", "off", map[string]any{"_di": "dev_x"})
				f.ent("cover.shed", "shelly", "closed", map[string]any{"_di": "dev_x", "device_class": "shutter"})
				f.fail["/api/states/cover.shed"] = true
			},
			args: []string{"switch.turn_on", "switch.relay"}, exit: 3, tier: "typed", reasons: typed},
		{name: "a shutter opened on a lock's device",
			setup: func(f *fakeHA) {
				f.ent("cover.hatch", "shelly", "closed", map[string]any{"_di": "dev_door", "device_class": "shutter"})
				f.ent("lock.door", "shelly", "locked", map[string]any{"_di": "dev_door"})
			},
			args: []string{"cover.open_cover", "cover.hatch"}, exit: 3, tier: "typed", reasons: typed},
		{name: "script member presses the gate button",
			setup: with(gate, func(f *fakeHA) { f.script("arrive", step("button.press", "button.gate_open")) }),
			args:  []string{"script.turn_on", "script.arrive"}, exit: 3, tier: "typed", reasons: []string{reasonStoredMembers, reasonDeviceAccessSibling}},
		{name: "script device-form press on the gate device",
			setup: with(gate, func(f *fakeHA) {
				f.script("arrive", map[string]any{"device_id": "dev_gate", "domain": "button", "type": "press", "entity_id": "1a2b"})
			}),
			args: []string{"script.arrive"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
		{name: "automation run switches the relay on",
			setup: with(gate, func(f *fakeHA) {
				f.automation("arrive", "a1", map[string]any{"actions": []any{step("switch.turn_on", "switch.gate_relay")}})
			}),
			args: []string{"automation.trigger", "automation.arrive"}, exit: 3, tier: "typed", reasons: typed},
		{name: "automation consumer of a helper presses the button",
			setup: with(gate, func(f *fakeHA) {
				f.ent("input_boolean.guest", "input_boolean", "off", nil)
				f.related["entity:input_boolean.guest"] = []string{"automation.guest"}
				f.automation("guest", "g1", map[string]any{
					"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}},
					"actions":  []any{step("button.press", "button.gate_open")},
				})
			}),
			args: []string{"input_boolean.turn_on", "input_boolean.guest"}, exit: 3, tier: "typed", reasons: typed},
		{name: "scene member turns the relay on",
			setup: with(gate, func(f *fakeHA) {
				f.ent("scene.arrive", "homeassistant", "unknown", nil)
				f.unique["scene.arrive"] = "s1"
				f.configs["/api/config/scene/config/s1"] = map[string]any{"entities": map[string]any{"switch.gate_relay": "on"}}
			}),
			args: []string{"scene.turn_on", "scene.arrive"}, exit: 3, tier: "typed", reasons: typed},
		{name: "group leaf is the relay",
			setup: with(gate, func(f *fakeHA) {
				f.ent("switch.outside", "group", "off", map[string]any{"entity_id": []any{"switch.gate_relay"}})
			}),
			args: []string{"switch.turn_on", "switch.outside"}, exit: 3, tier: "typed", reasons: typed},

		// Every other call on such a device keeps its own tier.
		{name: "lamp on the gate device turned on: ordinary", setup: gate, args: []string{"light.turn_on", "light.gate_lamp"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "relay switched off: ordinary", setup: gate, args: []string{"switch.turn_off", "switch.gate_relay"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "generic homeassistant.turn_off on the relay: ordinary", setup: gate, args: []string{"homeassistant.turn_off", "switch.gate_relay"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "light off on a lock's device: ordinary",
			setup: func(f *fakeHA) {
				f.ent("light.ring", "nuki", "on", map[string]any{"_di": "dev_nuki"})
				f.ent("lock.nuki", "nuki", "locked", map[string]any{"_di": "dev_nuki"})
			},
			args: []string{"light.turn_off", "light.ring"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "scene member switches the relay off: the run's own preview only",
			setup: with(gate, func(f *fakeHA) {
				f.ent("scene.leave", "homeassistant", "unknown", nil)
				f.unique["scene.leave"] = "s2"
				f.configs["/api/config/scene/config/s2"] = map[string]any{"entities": map[string]any{"switch.gate_relay": "off"}}
			}),
			args: []string{"scene.turn_on", "scene.leave"}, exit: 3, tier: "preview", reasons: []string{reasonStoredMembers}, absent: typed},

		// Unaffected.
		{name: "refreshing the relay stays ordinary", setup: gate, args: []string{"homeassistant.update_entity", "switch.gate_relay"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "closing the gate itself stays ordinary", setup: gate, args: []string{"cover.close_cover", "cover.gate"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "locking a lock that shares a device with a light stays ordinary",
			setup: func(f *fakeHA) {
				f.ent("light.ring", "nuki", "on", map[string]any{"_di": "dev_nuki"})
				f.ent("lock.nuki", "nuki", "unlocked", map[string]any{"_di": "dev_nuki"})
			},
			args: []string{"lock.lock", "lock.nuki"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "a switch_as_x light turned off on the gate device: ordinary",
			setup: with(gate, func(f *fakeHA) { f.ent("light.gate_relay", "switch_as_x", "on", map[string]any{"_di": "dev_gate"}) }),
			args:  []string{"light.turn_off", "light.gate_relay"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "a switch_as_x light on a device without an access entity: ordinary",
			setup: func(f *fakeHA) {
				f.ent("switch.lamp_plug", "tplink", "off", map[string]any{"_di": "dev_plug"})
				f.ent("light.lamp_plug", "switch_as_x", "off", map[string]any{"_di": "dev_plug"})
			},
			args: []string{"light.turn_on", "light.lamp_plug"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "relay next to a shutter only stays ordinary",
			setup: func(f *fakeHA) {
				f.ent("switch.relay", "shelly", "off", map[string]any{"_di": "dev_blind"})
				f.ent("cover.blind", "shelly", "open", map[string]any{"_di": "dev_blind", "device_class": "shutter"})
			},
			args: []string{"switch.turn_on", "switch.relay"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "a config button (ping) on a lock's device: ordinary",
			setup: func(f *fakeHA) {
				f.ent("button.lock_ping", "zwave_js", "unknown", map[string]any{"_di": "dev_door", "_ec": 1})
				f.ent("lock.door", "zwave_js", "locked", map[string]any{"_di": "dev_door"})
			},
			args: []string{"button.press", "button.lock_ping"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "a config button on an ESPHome gate board stays typed (user-authored)",
			setup: func(f *fakeHA) {
				f.ent("button.gate_pulse", "esphome", "unknown", map[string]any{"_di": "dev_esp", "_ec": 1})
				f.ent("cover.gate", "esphome", "closed", map[string]any{"_di": "dev_esp", "device_class": "gate"})
			},
			args: []string{"button.press", "button.gate_pulse"}, exit: 3, tier: "typed", reasons: []string{reasonUserButton}},
		{name: "integration service on the device keeps DEVICE_ACCESS",
			setup: gate, args: []string{"shelly.trigger", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccess}, absent: typed},
	}
	runTierCases(t, cases)
}

// Stored integration calls aimed at a device or an area act on the device
// itself, and a device target reaches its child devices (Home Assistant
// 2026.9+); neither depends on which entities are visible.
func TestStoredDeviceTargetsReachTheDevice(t *testing.T) {
	hiddenLock := func(f *fakeHA) {
		f.ent("lock.zw", "zwave_js", "locked", map[string]any{"_di": "dev_zw", "_hb": true})
		f.ent("sensor.zw_battery", "zwave_js", "90", map[string]any{"_di": "dev_zw", "_ec": 1})
	}
	setValue := func(target map[string]any) map[string]any {
		return map[string]any{"action": "zwave_js.set_value", "target": target, "data": map[string]any{"value": 0}}
	}
	runTierCases(t, []tierCase{
		{name: "integration service over an area with a hidden lock is typed",
			setup: func(f *fakeHA) {
				hiddenLock(f)
				f.related["area:hall"] = []string{"lock.zw", "sensor.zw_battery"}
				f.related["area-devices:hall"] = []string{"dev_zw"}
				f.script("hall", setValue(map[string]any{"area_id": "hall"}))
			},
			args: []string{"script.turn_on", "script.hall"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccess}},
		{name: "integration service on a device whose lock is disabled is typed",
			setup: func(f *fakeHA) {
				f.full = append(f.full, map[string]any{"entity_id": "lock.zw", "platform": "zwave_js", "device_id": "dev_zw", "disabled_by": "user"})
				f.script("node", setValue(map[string]any{"device_id": "dev_zw"}))
			},
			args: []string{"script.turn_on", "script.node"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccess}},
		{name: "a stored press on a parent device reaches the child's gate button",
			setup: func(f *fakeHA) {
				f.ent("button.gate_open", "shelly", "unknown", map[string]any{"_di": "dev_child"})
				f.ent("cover.gate", "shelly", "closed", map[string]any{"_di": "dev_child", "device_class": "gate"})
				f.devices = []map[string]any{{"id": "dev_parent"}, {"id": "dev_child", "via_device_id": "dev_parent"}}
				f.script("open", map[string]any{"action": "button.press", "target": map[string]any{"device_id": "dev_parent"}})
			},
			args: []string{"script.turn_on", "script.open"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
		{name: "--device on a parent freezes the child's entities too",
			setup: func(f *fakeHA) {
				f.ent("light.parent", "hue", "on", map[string]any{"_di": "dev_parent"})
				f.ent("light.child", "hue", "on", map[string]any{"_di": "dev_child"})
				f.devices = []map[string]any{{"id": "dev_child", "via_device_id": "dev_parent"}}
			},
			args: []string{"light.turn_off", "--device", "dev_parent"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded},
			check: func(t *testing.T, env verbTestEnvelope) {
				if len(env.Resolved) != 2 {
					t.Fatalf("resolved %+v", env.Resolved)
				}
			}},
		{name: "an unreadable device registry is typed",
			setup: func(f *fakeHA) {
				f.ent("switch.out", "shelly", "on", map[string]any{"_di": "dev_2"})
				f.fail["config/device_registry/list"] = true
			},
			args: []string{"switch.turn_off", "--device", "dev_2"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
	})
}

// A config write whose action presses a gate device's button is typed.
func TestWriteThatPressesAGateButtonIsTyped(t *testing.T) {
	f := newFakeHA()
	f.ent("button.gate_open", "shelly", "unknown", map[string]any{"_di": "dev_gate"})
	f.ent("cover.gate", "shelly", "closed", map[string]any{"_di": "dev_gate", "device_class": "gate"})
	h := newWriteHarness(t, f)
	draft := map[string]any{"alias": "Arrive", "triggers": []any{map[string]any{"trigger": "zone", "entity_id": "person.me", "zone": "zone.home", "event": "enter"}},
		"actions": []any{step("button.press", "button.gate_open")}}
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, draft))
	if exit != 3 || env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonDeviceAccessSibling) {
		t.Fatalf("exit %d tier %s reasons %s", exit, env.Tier, reasonCodes(env))
	}
}

// Area and device expansion acts on what Home Assistant's own targeting
// does: no hidden or categorized entity, and for an area none assigned to
// another area (helpers/target.py async_extract_referenced_entity_ids).
func TestExpansionSkipsWhatHomeAssistantSkips(t *testing.T) {
	resolved := func(want string) func(*testing.T, verbTestEnvelope) {
		return func(t *testing.T, env verbTestEnvelope) {
			var got []string
			for _, r := range env.Resolved {
				got = append(got, r.EntityID)
			}
			if strings.Join(got, ",") != want {
				t.Fatalf("resolved %v, want %s", got, want)
			}
		}
	}
	cases := []tierCase{
		{name: "a hidden light in the area is skipped",
			setup: func(f *fakeHA) {
				f.ent("light.raw", "shelly", "on", map[string]any{"_hb": true})
				f.related["area:kitchen"] = []string{"light.kitchen", "light.raw"}
			},
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 0, tier: "ordinary", check: resolved("light.kitchen")},
		{name: "a categorized switch in the area is skipped",
			setup: func(f *fakeHA) {
				f.ent("switch.child_lock", "miele", "off", map[string]any{"_ec": 0})
				f.ent("switch.kettle", "tplink", "off", nil)
				f.related["area:kitchen"] = []string{"switch.child_lock", "switch.kettle"}
			},
			args: []string{"switch.turn_on", "--area", "kitchen"}, exit: 0, tier: "ordinary", check: resolved("switch.kettle")},
		{name: "an area of only categorized entities finds nothing but names them",
			setup: func(f *fakeHA) {
				f.ent("switch.child_lock", "miele", "off", map[string]any{"_ec": 0})
				f.related["area:kitchen"] = []string{"switch.child_lock"}
			},
			args: []string{"switch.turn_on", "--area", "kitchen"}, exit: 5, tier: "",
			check: func(t *testing.T, env verbTestEnvelope) {
				if env.Error == nil || len(env.Error.Candidates) != 1 || env.Error.Candidates[0].EntityID != "switch.child_lock" || env.Error.Next == "" {
					t.Fatalf("error %+v", env.Error)
				}
			}},
		{name: "an integration service over an area keeps hidden entities",
			setup: func(f *fakeHA) {
				f.ent("light.raw", "shelly", "on", map[string]any{"_hb": true})
				f.related["area:kitchen"] = []string{"light.raw"}
			},
			args: []string{"shelly.blink", "--area", "kitchen"}, exit: 3, tier: "preview", check: resolved("light.raw")},
		{name: "a device entity assigned to another area is skipped",
			setup: func(f *fakeHA) {
				f.ent("light.porch", "hue", "on", map[string]any{"_ai": "garden"})
				f.related["area:kitchen"] = []string{"light.kitchen", "light.porch"}
			},
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 0, tier: "ordinary", check: resolved("light.kitchen")},
		{name: "a hidden switch on the device is skipped",
			setup: func(f *fakeHA) {
				f.ent("switch.out", "shelly", "on", map[string]any{"_di": "dev_2"})
				f.ent("switch.raw", "shelly", "on", map[string]any{"_di": "dev_2", "_hb": true})
			},
			args: []string{"switch.turn_off", "--device", "dev_2"}, exit: 0, tier: "ordinary", check: resolved("switch.out")},
		{name: "a device entity in its own area is still the device's",
			setup: func(f *fakeHA) { f.ent("switch.out", "shelly", "on", map[string]any{"_di": "dev_2", "_ai": "garden"}) },
			args:  []string{"switch.turn_off", "--device", "dev_2"}, exit: 0, tier: "ordinary", check: resolved("switch.out")},
		{name: "a hidden entity named directly is still acted on",
			setup: func(f *fakeHA) { f.ent("light.raw", "shelly", "on", map[string]any{"_hb": true}) },
			args:  []string{"light.turn_off", "light.raw"}, exit: 0, tier: "ordinary", check: resolved("light.raw")},
		{name: "a device-form step keeps its hidden entity (its consumer unlocks)",
			setup: func(f *fakeHA) {
				f.ent("switch.raw", "shelly", "off", map[string]any{"_di": "dev_raw", "_hb": true})
				f.related["entity:switch.raw"] = []string{"automation.unlock"}
				f.automation("unlock", "u1", map[string]any{
					"triggers": []any{map[string]any{"trigger": "state", "entity_id": "switch.raw"}},
					"actions":  []any{step("lock.unlock", "lock.front")},
				})
				f.script("go", map[string]any{"device_id": "dev_raw", "domain": "switch", "type": "turn_on", "entity_id": "9f8e"})
			},
			args: []string{"script.turn_on", "script.go"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "a stored area step skips the hidden light",
			setup: func(f *fakeHA) {
				f.ent("light.raw", "shelly", "on", map[string]any{"_hb": true})
				f.related["area:kitchen"] = []string{"light.kitchen", "light.raw"}
				f.script("night", map[string]any{"action": "light.turn_off", "target": map[string]any{"area_id": "kitchen"}})
			},
			args: []string{"script.turn_on", "script.night"}, exit: 3, tier: "preview",
			check: func(t *testing.T, env verbTestEnvelope) {
				if len(env.Members) != 1 || env.Members[0].EntityID != "light.kitchen" {
					t.Fatalf("members %+v", env.Members)
				}
			}},
	}
	runTierCases(t, cases)
}
