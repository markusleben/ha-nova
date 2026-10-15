package main

import (
	"strings"
	"testing"
)

// Device siblings (spec §2.2 step 2): a core-service call on an entity whose
// device also carries a lock, alarm panel, or access cover may be the opener.
// Press, switch on/toggle, and cover open are typed; other actuation previews.
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

		// Preview: other actuation on such a device.
		{name: "lamp on the gate device turned on: preview", setup: gate, args: []string{"light.turn_on", "light.gate_lamp"}, exit: 3, tier: "preview", reasons: typed},
		{name: "relay switched off: preview", setup: gate, args: []string{"switch.turn_off", "switch.gate_relay"}, exit: 3, tier: "preview", reasons: typed},
		{name: "light off on a lock's device: preview, not typed",
			setup: func(f *fakeHA) {
				f.ent("light.ring", "nuki", "on", map[string]any{"_di": "dev_nuki"})
				f.ent("lock.nuki", "nuki", "locked", map[string]any{"_di": "dev_nuki"})
			},
			args: []string{"light.turn_off", "light.ring"}, exit: 3, tier: "preview", reasons: typed},
		{name: "scene member switches the relay off: preview",
			setup: with(gate, func(f *fakeHA) {
				f.ent("scene.leave", "homeassistant", "unknown", nil)
				f.unique["scene.leave"] = "s2"
				f.configs["/api/config/scene/config/s2"] = map[string]any{"entities": map[string]any{"switch.gate_relay": "off"}}
			}),
			args: []string{"scene.turn_on", "scene.leave"}, exit: 3, tier: "preview", reasons: typed},

		// Unaffected.
		{name: "refreshing the relay stays ordinary", setup: gate, args: []string{"homeassistant.update_entity", "switch.gate_relay"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "closing the gate itself stays ordinary", setup: gate, args: []string{"cover.close_cover", "cover.gate"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "locking a lock that shares a device with a light stays ordinary",
			setup: func(f *fakeHA) {
				f.ent("light.ring", "nuki", "on", map[string]any{"_di": "dev_nuki"})
				f.ent("lock.nuki", "nuki", "unlocked", map[string]any{"_di": "dev_nuki"})
			},
			args: []string{"lock.lock", "lock.nuki"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "relay next to a shutter only stays ordinary",
			setup: func(f *fakeHA) {
				f.ent("switch.relay", "shelly", "off", map[string]any{"_di": "dev_blind"})
				f.ent("cover.blind", "shelly", "open", map[string]any{"_di": "dev_blind", "device_class": "shutter"})
			},
			args: []string{"switch.turn_on", "switch.relay"}, exit: 0, tier: "ordinary", absent: typed},
		{name: "integration service on the device keeps DEVICE_ACCESS",
			setup: gate, args: []string{"shelly.trigger", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccess}, absent: typed},
	}
	runTierCases(t, cases)
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
		{name: "an area of only categorized entities finds nothing",
			setup: func(f *fakeHA) {
				f.ent("switch.child_lock", "miele", "off", map[string]any{"_ec": 0})
				f.related["area:kitchen"] = []string{"switch.child_lock"}
			},
			args: []string{"switch.turn_on", "--area", "kitchen"}, exit: 5, tier: ""},
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
