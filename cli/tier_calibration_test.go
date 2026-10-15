package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Single-entity expansion (spec I5 exception): an area or device that
// resolves to exactly one entity of the service's own domain is classified
// as the direct call on that entity. Every neighbouring case stays strict.
func TestSingleEntityExpansionIsTheDirectCall(t *testing.T) {
	area := func(ids ...string) func(*fakeHA) {
		return func(f *fakeHA) { f.related["area:kitchen"] = ids }
	}
	cases := []tierCase{
		// Calibrated.
		{name: "area with one light: ordinary like naming it",
			setup: area("light.kitchen", "switch.kettle"),
			args:  []string{"light.turn_off", "--area", "kitchen"}, exit: 0, tier: "ordinary", absent: []string{reasonExpanded},
			check: func(t *testing.T, env verbTestEnvelope) {
				if len(env.Resolved) != 1 || env.Resolved[0].By != "area" || strings.Join(env.Action.EntityIDs, ",") != "light.kitchen" {
					t.Fatalf("resolved %+v action %+v", env.Resolved, env.Action)
				}
			}},
		{name: "device with one switch: ordinary like naming it",
			setup: func(f *fakeHA) {
				f.ent("switch.coffee", "shelly", "on", map[string]any{"_di": "dev_coffee"})
				f.ent("sensor.coffee_power", "shelly", "3", map[string]any{"_di": "dev_coffee"})
			},
			args: []string{"switch.turn_off", "--device", "dev_coffee"}, exit: 0, tier: "ordinary", absent: []string{reasonExpanded}},
		{name: "entity assigned to the area itself: ordinary",
			setup: func(f *fakeHA) {
				f.ent("light.island", "hue", "on", map[string]any{"_ai": "kitchen"})
				area("light.island")(f)
			},
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 0, tier: "ordinary"},
		{name: "closing the one garage door in an area grants nothing: ordinary",
			setup: func(f *fakeHA) {
				f.ent("cover.garage", "ratgdo", "open", map[string]any{"device_class": "garage"})
				f.related["area:garage"] = []string{"cover.garage"}
			},
			args: []string{"cover.close_cover", "--area", "garage"}, exit: 0, tier: "ordinary"},

		// Strict neighbours: scope.
		{name: "two lights stay preview",
			setup: area("light.kitchen", "light.hall"),
			args:  []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "generic homeassistant service over an area stays preview",
			setup: func(f *fakeHA) { f.ent("switch.fridge", "tplink", "on", nil); area("switch.fridge")(f) },
			args:  []string{"homeassistant.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a single group in the area expands to leaves: preview",
			setup: func(f *fakeHA) {
				f.ent("light.all", "group", "on", map[string]any{"entity_id": []any{"light.kitchen"}})
				area("light.all")(f)
			},
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a light plus a group holding it: preview",
			setup: func(f *fakeHA) { // the group sorts after the light, so the light is frozen by area
				f.ent("light.zz_all", "group", "on", map[string]any{"entity_id": []any{"light.kitchen"}})
				area("light.kitchen", "light.zz_all")(f)
			},
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a skipped leaf without state keeps the preview",
			setup: area("light.kitchen", "light.ghost"),
			args:  []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded},
			check: func(t *testing.T, env verbTestEnvelope) {
				if !strings.Contains(strings.Join(env.Coverage.Notes, ","), noteSkippedTargets) {
					t.Fatalf("notes %+v", env.Coverage.Notes)
				}
			}},
		{name: "area plus device selectors stay preview",
			setup: func(f *fakeHA) {
				f.ent("light.desk", "hue", "on", map[string]any{"_di": "dev_desk"})
				area("light.desk")(f)
			},
			args: []string{"light.turn_off", "--area", "kitchen", "--device", "dev_desk"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},

		{name: "two switches on one device stay preview",
			setup: func(f *fakeHA) {
				f.ent("switch.gang_1", "shelly", "on", map[string]any{"_di": "dev_2gang"})
				f.ent("switch.gang_2", "shelly", "on", map[string]any{"_di": "dev_2gang"})
			},
			args: []string{"switch.turn_off", "--device", "dev_2gang"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "generic homeassistant service over a device stays preview",
			setup: func(f *fakeHA) { f.ent("switch.plug", "tplink", "on", map[string]any{"_di": "dev_plug"}) },
			args:  []string{"homeassistant.turn_off", "--device", "dev_plug"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a group entity on the device expands to leaves: preview",
			setup: func(f *fakeHA) {
				f.ent("light.room", "hue", "on", map[string]any{"_di": "dev_bridge", "entity_id": []any{"light.kitchen"}})
			},
			args: []string{"light.turn_off", "--device", "dev_bridge"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a button on a gate device is typed (it may be the opener)",
			setup: func(f *fakeHA) {
				f.ent("button.gate_trigger", "shelly", "unknown", map[string]any{"_di": "dev_gate"})
				f.ent("cover.gate", "shelly", "closed", map[string]any{"_di": "dev_gate", "device_class": "gate"})
			},
			args: []string{"button.press", "--device", "dev_gate"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccessSibling}},
		{name: "a shutter on the same device is no access entity: ordinary",
			setup: func(f *fakeHA) {
				f.ent("light.blind_led", "shelly", "on", map[string]any{"_di": "dev_blind"})
				f.ent("cover.blind", "shelly", "open", map[string]any{"_di": "dev_blind", "device_class": "shutter"})
				area("light.blind_led")(f)
			},
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 0, tier: "ordinary"},
		{name: "the one light in an area whose device also has a lock stays preview",
			setup: func(f *fakeHA) {
				f.ent("light.door_ring", "nuki", "on", map[string]any{"_di": "dev_nuki"})
				f.ent("lock.nuki", "nuki", "locked", map[string]any{"_di": "dev_nuki"})
				area("light.door_ring")(f)
			},
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonDeviceAccessSibling}},
		{name: "an unreadable sibling cover state keeps the preview",
			setup: func(f *fakeHA) {
				f.ent("light.porch", "shelly", "on", map[string]any{"_di": "dev_porch"})
				f.ent("cover.porch_gate", "shelly", "closed", map[string]any{"_di": "dev_porch", "device_class": "shutter"})
				f.fail["/api/states/cover.porch_gate"] = true
				area("light.porch")(f)
			},
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonDeviceAccessSibling}},

		// Strict neighbours: what acting on the one entity reaches.
		{name: "opening the one garage door in an area stays typed",
			setup: func(f *fakeHA) {
				f.ent("cover.garage", "ratgdo", "closed", map[string]any{"device_class": "garage"})
				f.related["area:garage"] = []string{"cover.garage"}
			},
			args: []string{"cover.open_cover", "--area", "garage"}, exit: 3, tier: "typed", reasons: []string{reasonAccessCover}},
		{name: "unlocking the one lock in an area stays typed",
			setup: func(f *fakeHA) { f.related["area:door"] = []string{"lock.front"} },
			args:  []string{"lock.unlock", "--area", "door"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "a template light in the area stays typed",
			setup: func(f *fakeHA) { f.ent("light.tpl", "template", "off", nil); area("light.tpl")(f) },
			args:  []string{"light.turn_on", "--area", "kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonTemplateAction}},
		{name: "a helper whose consumer unlocks stays typed",
			setup: func(f *fakeHA) {
				f.ent("input_boolean.guest", "input_boolean", "off", nil)
				f.related["entity:input_boolean.guest"] = []string{"automation.guest"}
				f.automation("guest", "g1", map[string]any{
					"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}},
					"actions":  []any{step("lock.unlock", "lock.front")},
				})
				area("input_boolean.guest")(f)
			},
			args: []string{"input_boolean.turn_on", "--area", "kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "the one script in an area is still a stored-member run",
			setup: func(f *fakeHA) { f.script("night", step("light.turn_off", "light.hall")); area("script.night")(f) },
			args:  []string{"script.turn_on", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonStoredMembers}},
		{name: "the one scene in an area is still a stored-member run",
			setup: func(f *fakeHA) {
				f.ent("scene.cosy", "homeassistant", "unknown", nil)
				f.unique["scene.cosy"] = "c1"
				f.configs["/api/config/scene/config/c1"] = map[string]any{"entities": map[string]any{"lock.front": "unlocked"}}
				area("scene.cosy")(f)
			},
			args: []string{"scene.turn_on", "--area", "kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonStoredMembers, reasonAccessLock}},
		{name: "an integration service on the one device entity stays preview",
			setup: func(f *fakeHA) { f.ent("light.bulb", "zha", "on", map[string]any{"_di": "dev_bulb"}) },
			args:  []string{"zha.set_lock_user_code", "--device", "dev_bulb"}, exit: 3, tier: "preview", reasons: []string{reasonIntegrationSvc}},
		{name: "the switch behind an access wrapper stays typed",
			setup: func(f *fakeHA) {
				f.ent("switch.relay", "shelly", "off", map[string]any{"_di": "dev_gate"})
				f.ent("cover.gate", "switch_as_x", "closed", map[string]any{"_di": "dev_gate", "device_class": "gate"})
			},
			args: []string{"switch.turn_on", "--device", "dev_gate"}, exit: 3, tier: "typed", reasons: []string{reasonSwitchAsX}},
		{name: "a failed area read stays typed",
			setup: func(f *fakeHA) { f.fail["search/related"] = true },
			args:  []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
	}
	runTierCases(t, cases)
}

// The calibrated area call executes on `call` like a named one: one POST
// with exactly the frozen entity, verified, no plan stored.
func TestSingleEntityAreaCallRunsOnce(t *testing.T) {
	f := newFakeHA()
	f.related["area:kitchen"] = []string{"light.kitchen"}
	h := newWriteHarness(t, f)
	exit, env := h.run(runCallCommand, "light.turn_off", "--area", "kitchen")
	posts := h.posts()
	if exit != 0 || env.Status != "verified" || env.Tier != "ordinary" || len(posts) != 1 || len(h.planFiles()) != 0 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(posts))
	}
	if body, _ := json.Marshal(posts[0].payload["body"]); string(body) != `{"entity_id":["light.kitchen"]}` {
		t.Fatalf("payload %s", body)
	}
}

// A config write acting on a template switch stays typed: its action is unread.
func TestWriteOnTemplateSwitchStaysTyped(t *testing.T) {
	f := newFakeHA()
	f.ent("switch.tpl_porch", "template", "off", nil)
	h := newWriteHarness(t, f)
	draft := map[string]any{"alias": "Porch", "triggers": []any{map[string]any{"trigger": "sun", "event": "sunset"}},
		"actions": []any{step("switch.turn_on", "switch.tpl_porch")}}
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, draft))
	if exit != 3 || env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonTemplateAction) {
		t.Fatalf("exit %d tier %s reasons %s", exit, env.Tier, reasonCodes(env))
	}
}

// Sweep: an integration service with --match searches every domain (its
// target may be a lock), unchanged by the single-expansion rule.
func TestIntegrationServiceMatchesAcrossDomains(t *testing.T) {
	runTierCases(t, []tierCase{{name: "zwave_js.set_value --match a lock",
		setup: func(f *fakeHA) { f.ent("lock.front", "zwave_js", "locked", map[string]any{"friendly_name": "Front"}) },
		args:  []string{"zwave_js.set_value", "--match", "front", "--set", "value=0"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}}})
}

// I3: a stored single-entity plan drifts when a second entity joins the area
// before apply; nothing is sent.
func TestSingleEntityAreaPlanDriftsWhenTheAreaGrows(t *testing.T) {
	f := newFakeHA()
	f.related["area:kitchen"] = []string{"light.kitchen"}
	h := newWriteHarness(t, f)
	planned := h.plan("light.turn_off", "--area", "kitchen")
	if planned.Tier != "ordinary" {
		t.Fatalf("plan tier %s", planned.Tier)
	}
	f.related["area:kitchen"] = []string{"light.kitchen", "light.hall"}
	if exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID); exit != 2 || env.Tier != "preview" || len(h.posts()) != 0 {
		t.Fatalf("apply after growth: exit %d tier %s posts %d", exit, env.Tier, len(h.posts()))
	}
}
