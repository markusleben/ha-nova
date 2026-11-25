package main

import (
	"slices"
	"strings"
	"testing"
)

// The maintainer's home after 0.34 (MAR-262, MAR-263; sanitized, invented
// names): the YAML thermostat, its template heater and plug (mar245Home), the
// plug's device in an area on a floor that each hold a script, a valve
// automation on state triggers, and a panel blueprint instance on
// state_changed events whose filters name other entities, empty inputs, or a
// relay through a trigger variable.
func bathHome(t *testing.T, change func(*fakeHA)) func(*fakeHA) {
	return func(f *fakeHA) {
		mar245Home(t, newFakeFiles(mar245Files()))(f)
		f.devices = append(f.devices, map[string]any{"id": "dplug", "area_id": "bath"})
		f.areas = append(f.areas, map[string]any{"area_id": "bath", "floor_id": "upstairs"})
		f.script("morning_greeting", map[string]any{"delay": "00:00:05"}, step("light.turn_on", "light.hall"))
		f.script("boiler_restart", map[string]any{"delay": "00:01:00"}, step("switch.turn_on", "switch.boiler"))
		f.related["scripts:bath"] = []string{"script.morning_greeting"}
		f.related["scripts:upstairs"] = []string{"script.boiler_restart"}
		f.ent("climate.kitchen_valve", "homematic", "heat", nil)
		f.automation("valve_guard", "valve_guard", map[string]any{
			"triggers":   []any{map[string]any{"trigger": "state", "entity_id": []any{"climate.kitchen_valve"}, "to": "auto"}},
			"conditions": []any{map[string]any{"condition": "template", "value_template": "{{ trigger.to_state.attributes.model == 'RT' }}"}},
			"actions":    []any{map[string]any{"action": "climate.set_temperature", "target": map[string]any{"entity_id": "{{ trigger.entity_id }}"}, "data": map[string]any{"hvac_mode": "heat"}}},
		})
		f.automation("kitchen_panel", "kitchen_panel", map[string]any{"use_blueprint": map[string]any{"path": "panel/panel.yaml", "input": map[string]any{}}})
		f.substitute = map[string]any{"substituted_config": panelConfig(map[string]any{"relay_entity": "switch.panel_relay_1"})}
		if change != nil {
			change(f)
		}
	}
}

// panelConfig: a panel blueprint as Home Assistant substitutes it: one
// state_changed trigger per input (set, empty list, empty, null) and one on
// its relay through a trigger variable.
func panelConfig(vars map[string]any, extra ...map[string]any) map[string]any {
	changed := func(entity any) any {
		return map[string]any{"trigger": "event", "event_type": "state_changed", "event_data": map[string]any{"entity_id": entity}}
	}
	triggers := []any{changed("sensor.kitchen_temperature"), changed([]any{}), changed(""), changed(nil), changed("{{ relay_entity }}")}
	for _, t := range extra {
		triggers = append(triggers, t)
	}
	return map[string]any{"trigger_variables": vars, "triggers": triggers, "actions": []any{step("light.turn_on", "light.hall")}}
}

func quietCause(entity, detail string) func(*testing.T, verbTestEnvelope) {
	return func(t *testing.T, env verbTestEnvelope) {
		t.Helper()
		if !slices.ContainsFunc(env.Reasons, func(r tierReason) bool {
			return r.Code == reasonHelperCalls && r.EntityID == "climate.bath_thermostat" && strings.HasPrefix(r.Detail, detail) && strings.Contains(r.Detail, entity)
		}) {
			t.Fatalf("reasons %+v lack %s %q naming %s", env.Reasons, reasonHelperCalls, detail, entity)
		}
		reachesWatchdog(true)(t, env)
	}
}

// plugWatch: an automation tied to the plug whose trigger is elsewhere but
// whose actions wait on the plug, with runs in progress.
func plugWatch(runs int) func(*fakeHA) {
	return func(f *fakeHA) {
		f.automation("plug_watch", "plug_watch", map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.hall", "to": "on"}},
			"actions":  []any{map[string]any{"wait_template": "{{ is_state('switch.bath_plug', 'on') }}"}, step("lock.unlock", "lock.front")},
		})
		f.states["automation.plug_watch"]["attributes"].(map[string]any)["current"] = runs
		f.related["entity:switch.bath_plug"] = append(f.related["entity:switch.bath_plug"], "automation.plug_watch")
	}
}

func TestQuietChecksInTheMaintainersHome(t *testing.T) {
	light := []string{"light.turn_on", "light.kitchen"}
	panelWith := func(vars map[string]any, extra ...map[string]any) func(*fakeHA) {
		return bathHome(t, func(f *fakeHA) { f.substitute = map[string]any{"substituted_config": panelConfig(vars, extra...)} })
	}
	plugID := func(f *fakeHA) {
		for _, row := range f.full {
			if row["entity_id"] == "switch.bath_plug" {
				row["id"] = "4f1c0de2a9b84e7d9a1b2c3d4e5f6a7b"
			}
		}
	}
	runTierCases(t, []tierCase{
		{name: "the light asks nothing: scripts at rest, literal panel filters, a valve automation", setup: bathHome(t, nil), args: light, exit: 0, tier: "ordinary", check: reachesWatchdog(false)},
		{name: "a cover call still asks", setup: bathHome(t, nil), args: []string{"cover.close_cover", "cover.shutter_left"}, exit: 3, tier: "typed", check: reachesWatchdog(true)},
		{name: "the garage still asks", setup: bathHome(t, nil), args: []string{"cover.open_cover", "cover.garage_door"}, exit: 3, tier: "typed", reasons: []string{reasonAccessCover}},
		{name: "an unlock still asks", setup: bathHome(t, nil), args: []string{"lock.unlock", "lock.front"}, exit: 3, tier: "typed"},
		{name: "a running script in the plug's area", setup: bathHome(t, func(f *fakeHA) { f.states["script.morning_greeting"]["state"] = "on" }),
			args: light, exit: 3, tier: "typed", check: quietCause("script.morning_greeting (area bath)", "heater_actions: switch.bath_plug: running script")},
		{name: "a running script on the plug's floor", setup: bathHome(t, func(f *fakeHA) { f.states["script.boiler_restart"]["state"] = "on" }),
			args: light, exit: 3, tier: "typed", check: quietCause("script.boiler_restart (floor upstairs)", "heater_actions: switch.bath_plug: running script")},
		{name: "a script whose state can't be read", setup: bathHome(t, func(f *fakeHA) { // this home's census reads every state from one list (warmCensus): the script has none there
			f.fail["/api/states/script.boiler_restart"] = true
			delete(f.states, "script.boiler_restart")
		}),
			args: light, exit: 3, tier: "typed", check: quietCause("script.boiler_restart (floor upstairs)", "heater_actions: switch.bath_plug: state unread of")},
		{name: "an automation tied to the plug at rest", setup: bathHome(t, plugWatch(0)), args: light, exit: 0, tier: "ordinary", check: reachesWatchdog(false)},
		{name: "an automation tied to the plug while it runs", setup: bathHome(t, plugWatch(1)),
			args: light, exit: 3, tier: "typed", check: quietCause("automation.plug_watch (entity switch.bath_plug)", "heater_actions: switch.bath_plug: running automation")},
		{name: "a running wait on state_changed events", setup: bathHome(t, func(f *fakeHA) {
			f.script("hall_wait", map[string]any{"wait_for_trigger": []any{map[string]any{"trigger": "event", "event_type": "state_changed", "event_data": map[string]any{"entity_id": "light.hall"}}}})
			f.states["script.hall_wait"]["state"] = "on"
		}), args: light, exit: 3, tier: "typed", check: quietCause("a running wait (it may be on state_changed events)", "helper_listened: climate.bath_thermostat: couldn't rule out a reaction from")},
		{name: "a panel relay named by a templated trigger variable", setup: panelWith(map[string]any{"relay_entity": "{{ device_entities('panel') | first }}"}),
			args: light, exit: 3, tier: "typed", check: quietCause("automation.kitchen_panel (templated state_changed trigger)", "helper_listened: climate.bath_thermostat: couldn't rule out a reaction from")},
		{name: "a panel relay variable that isn't defined", setup: panelWith(map[string]any{}),
			args: light, exit: 3, tier: "typed", check: quietCause("automation.kitchen_panel (templated state_changed trigger)", "helper_listened: climate.bath_thermostat: couldn't rule out a reaction from")},
		{name: "a panel trigger with no entity filter", setup: panelWith(map[string]any{"relay_entity": "switch.panel_relay_1"}, map[string]any{"trigger": "event", "event_type": "state_changed"}),
			args: light, exit: 3, tier: "typed", check: quietCause("automation.kitchen_panel (state_changed trigger on every entity)", "helper_listened: climate.bath_thermostat: couldn't rule out a reaction from")},
		{name: "a panel trigger with a templated key", setup: panelWith(map[string]any{"relay_entity": "switch.panel_relay_1"},
			map[string]any{"trigger": "event", "event_type": "state_changed", "event_data": map[string]any{"{{ 'entity_id' }}": "switch.panel_relay_1"}}),
			args: light, exit: 3, tier: "typed", check: quietCause("automation.kitchen_panel (templated state_changed trigger)", "helper_listened: climate.bath_thermostat: couldn't rule out a reaction from")},
		{name: "a panel relay variable that names the plug", setup: panelWith(map[string]any{"relay_entity": "switch.bath_plug"}),
			args: light, exit: 3, tier: "typed", check: quietCause("state_changed trigger of automation.kitchen_panel", "heater_actions: switch.bath_plug")},
		{name: "a padded panel relay variable that names the plug", setup: panelWith(map[string]any{"relay_entity": " switch.bath_plug "}),
			args: light, exit: 3, tier: "typed", check: quietCause("state_changed trigger of automation.kitchen_panel", "heater_actions: switch.bath_plug")},
		{name: "a panel trigger whose options name the thermostat", setup: panelWith(map[string]any{"relay_entity": "switch.panel_relay_1"},
			map[string]any{"trigger": "event", "options": map[string]any{"event_type": "state_changed", "event_data": map[string]any{"entity_id": "climate.bath_thermostat"}}}),
			args: light, exit: 3, tier: "typed", check: quietCause("state_changed trigger of automation.kitchen_panel", "helper_listened: climate.bath_thermostat")},
		{name: "a padded state_changed type that names the thermostat", setup: panelWith(map[string]any{"relay_entity": "switch.panel_relay_1"},
			map[string]any{"trigger": "event", "event_type": " state_changed\n", "event_data": map[string]any{" entity_id": "climate.bath_thermostat "}}),
			args: light, exit: 3, tier: "typed", check: quietCause("state_changed trigger of automation.kitchen_panel", "helper_listened: climate.bath_thermostat")},
		{name: "a panel trigger on the reconnect sync's own entity", setup: panelWith(map[string]any{"relay_entity": "switch.panel_relay_1"},
			map[string]any{"trigger": "event", "event_type": "state_changed", "event_data": map[string]any{"entity_id": "automation.plug_reconnect_sync"}}),
			args: light, exit: 3, tier: "typed", check: quietCause("trigger of automation.plug_reconnect_sync", "heater_actions: switch.bath_plug")},
		{name: "a panel trigger that names the thermostat", setup: panelWith(map[string]any{"relay_entity": "switch.panel_relay_1"},
			map[string]any{"trigger": "event", "event_type": "state_changed", "event_data": map[string]any{"entity_id": "climate.bath_thermostat"}}),
			args: light, exit: 3, tier: "typed", check: quietCause("state_changed trigger of automation.kitchen_panel", "helper_listened: climate.bath_thermostat")},
		{name: "a state trigger that names the plug by its registry entry id (MAR-263)", setup: bathHome(t, func(f *fakeHA) {
			plugID(f)
			f.automation("plug_follow", "plug_follow", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "4f1c0de2a9b84e7d9a1b2c3d4e5f6a7b"}}, "actions": []any{step("lock.unlock", "lock.front")}})
			f.related["entity:switch.bath_plug"] = append(f.related["entity:switch.bath_plug"], "automation.plug_follow")
		}), args: light, exit: 3, tier: "typed", check: quietCause("trigger of automation.plug_follow", "heater_actions: switch.bath_plug")},
		{name: "an unreadable entity registry", setup: bathHome(t, func(f *fakeHA) { f.fail["config/entity_registry/list"] = true }), args: light, exit: 3, tier: "typed"},
	})
}

// Home Assistant trims an event trigger's rendered strings: a padded
// service-call filter still counts for the call it names.
func TestPaddedServiceCallFilter(t *testing.T) {
	h, f := callHome(t)
	listen(f, "padded", map[string]any{" domain ": " cover\n"}, step("lock.unlock", "lock.front"))
	if _, env := h.run(runPlanCommand, "call", "cover.close_cover", "cover.blind"); !slices.Contains(consumerList(env), "automation.padded") {
		t.Errorf("tier %q (reasons %s), consumers %v; want the padded listener reached", env.Tier, reasonCodes(env), consumerList(env))
	}
}

// MAR-263 on the main path: a state trigger may name the entity by its
// registry entry id; Home Assistant resolves it and search/related lists the
// automation under the entity, so its trigger must match too.
func TestStateTriggerByRegistryEntryID(t *testing.T) {
	runTierCases(t, []tierCase{
		{name: "an automation on the light's entry id that unlocks", setup: func(f *fakeHA) {
			f.full = append(f.full, map[string]any{"id": "9d2e7b1c4a5f4e3d8c7b6a5f4e3d2c1b", "entity_id": "light.kitchen", "platform": "hue"})
			f.automation("kitchen_follow", "kitchen_follow", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": []any{"9d2e7b1c4a5f4e3d8c7b6a5f4e3d2c1b"}, "to": "off"}},
				"actions": []any{step("lock.unlock", "lock.front")}})
			f.related["entity:light.kitchen"] = []string{"automation.kitchen_follow"}
		}, args: []string{"light.turn_off", "light.kitchen"}, exit: 3, tier: "typed", check: func(t *testing.T, env verbTestEnvelope) {
			if !slices.Contains(consumerList(env), "automation.kitchen_follow") {
				t.Fatalf("consumers %v lack automation.kitchen_follow", consumerList(env))
			}
		}},
	})
}

// literalEntity follows Home Assistant's event trigger: rendered once with
// the trigger variables, then compared for equality.
func TestLiteralEntity(t *testing.T) {
	vars := map[string]any{"relay": "switch.relay_1", "padded": " switch.relay_2\n", "dynamic": "{{ x }}", "list": []any{"switch.a"}}
	for name, tc := range map[string]struct {
		filter  map[string]any
		want    string
		literal bool
	}{
		"an entity":                     {map[string]any{"entity_id": "switch.a"}, "switch.a", true},
		"a padded entity":               {map[string]any{"entity_id": "\tswitch.a \u00a0"}, "switch.a", true},
		"an empty input":                {map[string]any{"entity_id": ""}, "", true},
		"an empty list":                 {map[string]any{"entity_id": []any{}}, "", true},
		"null":                          {map[string]any{"entity_id": nil}, "", true},
		"a template in a list":          {map[string]any{"entity_id": []any{"{{ relay }}"}}, "", true},
		"a literal trigger variable":    {map[string]any{"entity_id": "{{relay}}"}, "switch.relay_1", true},
		"a padded trigger variable":     {map[string]any{"entity_id": "{{ padded }}"}, "switch.relay_2", true},
		"no entity_id":                  {map[string]any{"new_state": "on"}, "", false},
		"no event_data":                 {nil, "", false},
		"a templated trigger variable":  {map[string]any{"entity_id": "{{ dynamic }}"}, "", false},
		"a list trigger variable":       {map[string]any{"entity_id": "{{ list }}"}, "", false},
		"an undefined trigger variable": {map[string]any{"entity_id": "{{ other }}"}, "", false},
		"an expression":                 {map[string]any{"entity_id": "{{ relay ~ '' }}"}, "", false},
		"a comment":                     {map[string]any{"entity_id": "{# x #}switch.a"}, "", false},
		"a templated key":               {map[string]any{"entity_id": "switch.a", "{{ 'entity_id' }}": "switch.b"}, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, literal := literalEntity(tc.filter, vars)
			if literal != tc.literal || got != tc.want {
				t.Fatalf("literalEntity = %v, %v; want %v, %v", got, literal, tc.want, tc.literal)
			}
		})
	}
}
