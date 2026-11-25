package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// mar245Home: MAR-209's home (logbook open) with the maintainer's YAML
// thermostat: its heater a YAML template switch around a Tasmota plug, read
// from the /config tree ff serves (mar245Files, sanitized).
func mar245Home(t *testing.T, ff *fakeFiles) func(*fakeHA) {
	return func(f *fakeHA) {
		mar209Home(t, true)(f)
		f.config["components"] = append(f.config["components"].([]string), "generic_thermostat.climate", "template.switch", "tasmota.switch")
		for _, d := range []string{"generic_thermostat", "template", "tasmota"} {
			f.manifests = append(f.manifests, map[string]any{"domain": d, "is_built_in": true})
		}
		f.ent("climate.bath_thermostat", "", "heat", map[string]any{"friendly_name": "Bath Thermostat"}) // YAML, no unique_id: no registry row
		f.ent("switch.bath_heater_safe", "template", "off", map[string]any{"friendly_name": "Bath Heater Safe"})
		f.ent("switch.bath_plug", "tasmota", "off", map[string]any{"friendly_name": "Bath Plug", "_di": "dplug"})
		f.full = append(f.full,
			map[string]any{"entity_id": "switch.bath_heater_safe", "platform": "template", "unique_id": "bath_heater_safe", "config_entry_id": nil},
			map[string]any{"entity_id": "switch.bath_plug", "platform": "tasmota", "unique_id": "543204EB2BDC_switch_relay_0", "config_entry_id": "ETAS", "device_id": "dplug"})
		f.automation("plug_reconnect_sync", "plug_reconnect_sync", reconnectSyncConfig()) // the maintainer's own, sanitized
		f.related["entity:switch.bath_plug"] = []string{"automation.plug_reconnect_sync"}
		sources := map[string]any{"climate.bath_thermostat": map[string]any{"domain": "generic_thermostat"},
			"switch.bath_heater_safe": map[string]any{"domain": "template"}, "switch.bath_plug": map[string]any{"domain": "tasmota"}}
		f.extra = func(endpoint string, p map[string]any) (any, bool) {
			if endpoint == "ws" && p["type"] == "entity/source" {
				return sources, true
			}
			return ff.reply(endpoint, p)
		}
	}
}

// reconnectSyncConfig: an automation that puts the plug in step with the
// thermostat after the plug reconnects (the maintainer's home).
func reconnectSyncConfig() map[string]any {
	return map[string]any{
		"triggers": []any{map[string]any{"trigger": "state", "entity_id": "switch.bath_plug", "from": "unavailable"}},
		"actions": []any{map[string]any{"delay": map[string]any{"seconds": 5}}, map[string]any{
			"choose": []any{map[string]any{
				"conditions": []any{map[string]any{"condition": "state", "entity_id": "climate.bath_thermostat", "attribute": "hvac_action", "state": "heating"}},
				"sequence":   []any{step("switch.turn_on", "switch.bath_plug")}}},
			"default": []any{step("switch.turn_off", "switch.bath_plug")}}},
	}
}

// Only a sync after a reconnect leaves the plug quiet: every trigger or
// action that could fire on a call, or do more than switch the plug, is loud.
func TestReconnectSyncIsNarrow(t *testing.T) {
	trigger := func(change map[string]any) map[string]any {
		cfg := reconnectSyncConfig()
		for k, v := range change {
			cfg["triggers"].([]any)[0].(map[string]any)[k] = v
		}
		return cfg
	}
	action := func(steps ...any) map[string]any {
		cfg := reconnectSyncConfig()
		cfg["actions"] = steps
		return cfg
	}
	for name, tc := range map[string]struct {
		cfg   map[string]any
		id    string
		quiet bool
	}{
		"the maintainer's sync":                {reconnectSyncConfig(), "switch.bath_plug", true},
		"to on, a literal for":                 {trigger(map[string]any{"to": "on", "for": "00:00:10"}), "switch.bath_plug", true},
		"from unknown (available, optimistic)": {trigger(map[string]any{"from": []any{"unavailable", "unknown"}}), "switch.bath_plug", false},
		"a templated for":                      {trigger(map[string]any{"for": map[string]any{"seconds": "{{ 1 / 0 }}"}}), "switch.bath_plug", false},
		"a templated delay":                    {action(map[string]any{"delay": map[string]any{"seconds": "{{ 1 / 0 }}"}}), "switch.bath_plug", false},
		"an id in capitals":                    {trigger(map[string]any{"entity_id": "SWITCH.BATH_PLUG"}), "switch.bath_plug", false},
		"a disabled trigger options revive": {func() map[string]any {
			c := reconnectSyncConfig()
			c["triggers"] = append(c["triggers"].([]any), map[string]any{"trigger": "state", "enabled": false, "options": map[string]any{"enabled": true, "entity_id": "switch.bath_plug", "from": "off", "to": "on"}})
			return c
		}(), "switch.bath_plug", false},
		"a triggers wrapper with more keys": {func() map[string]any {
			c := reconnectSyncConfig()
			c["triggers"] = []any{map[string]any{"triggers": c["triggers"], "id": "x"}}
			return c
		}(), "switch.bath_plug", false},
		"an if and homeassistant.turn_off": {action(map[string]any{"if": []any{map[string]any{"condition": "template", "value_template": "{{ x }}"}}, "then": []any{step("homeassistant.turn_off", "switch.bath_plug")}}), "switch.bath_plug", true},
		"from unavailable or off":          {trigger(map[string]any{"from": []any{"unavailable", "off"}}), "switch.bath_plug", false},
		"no from":                          {trigger(map[string]any{"from": nil}), "switch.bath_plug", false},
		"a template in from":               {trigger(map[string]any{"from": "{{ 'unavailable' }}"}), "switch.bath_plug", false},
		"to unavailable":                   {trigger(map[string]any{"to": "unavailable"}), "switch.bath_plug", false},
		"to null":                          {trigger(map[string]any{"to": nil}), "switch.bath_plug", false},
		"an attribute":                     {trigger(map[string]any{"attribute": "current"}), "switch.bath_plug", false},
		"not_from":                         {trigger(map[string]any{"not_from": "on"}), "switch.bath_plug", false},
		"another entity too":               {trigger(map[string]any{"entity_id": []any{"switch.bath_plug", "switch.other"}}), "switch.bath_plug", false},
		"a device trigger":                 {trigger(map[string]any{"trigger": "device"}), "switch.bath_plug", false},
		"a second trigger on its state": {func() map[string]any {
			c := reconnectSyncConfig()
			c["triggers"] = append(c["triggers"].([]any), map[string]any{"trigger": "state", "entity_id": "switch.bath_plug"})
			return c
		}(), "switch.bath_plug", false},
		"an action on another entity":        {action(step("switch.turn_on", "switch.other")), "switch.bath_plug", false},
		"an action that unlocks":             {action(step("lock.unlock", "lock.front")), "switch.bath_plug", false},
		"an action with data":                {action(map[string]any{"action": "switch.turn_on", "target": map[string]any{"entity_id": "switch.bath_plug"}, "data": map[string]any{}}), "switch.bath_plug", false},
		"an area target":                     {action(map[string]any{"action": "switch.turn_on", "target": map[string]any{"entity_id": "switch.bath_plug", "area_id": "bath"}}), "switch.bath_plug", false},
		"a toggle":                           {action(step("switch.toggle", "switch.bath_plug")), "switch.bath_plug", false},
		"a wait":                             {action(map[string]any{"wait_template": "{{ x }}"}), "switch.bath_plug", false},
		"a script call":                      {action(map[string]any{"action": "script.turn_on", "target": map[string]any{"entity_id": "script.x"}}), "switch.bath_plug", false},
		"a choose step with an unknown step": {action(map[string]any{"choose": []any{map[string]any{"sequence": []any{map[string]any{"event": "x"}}}}}), "switch.bath_plug", false},
		"a thermostat, not its heater":       {reconnectSyncConfig(), "climate.bath_thermostat", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := reconnectSync(tc.cfg, tc.id); got != tc.quiet {
				t.Fatalf("reconnectSync = %v, want %v", got, tc.quiet)
			}
		})
	}
}

// helperCause: the plan names the YAML thermostat, with why.
func helperCause(detail string) func(*testing.T, verbTestEnvelope) {
	return func(t *testing.T, env verbTestEnvelope) {
		t.Helper()
		if !slices.ContainsFunc(env.Reasons, func(r tierReason) bool {
			return r.Code == reasonHelperCalls && r.EntityID == "climate.bath_thermostat" && (r.Detail == detail || strings.HasPrefix(r.Detail, detail+": "))
		}) {
			t.Fatalf("reasons %+v lack %s %s", env.Reasons, reasonHelperCalls, detail)
		}
		reachesWatchdog(true)(t, env)
	}
}

// MAR-245: with the YAML Home Assistant runs read, the maintainer's light
// asks nothing; every gap keeps the watchdog and names the thermostat.
func TestYAMLThermostatBoundsTheLight(t *testing.T) {
	light := []string{"light.turn_on", "light.kitchen"}
	files := func(change func(*fakeFiles)) func(*fakeHA) {
		ff := newFakeFiles(mar245Files())
		if change != nil {
			change(ff)
		}
		return mar245Home(t, ff)
	}
	heaterSwitch := func(turnOn string) func(*fakeFiles) {
		return func(ff *fakeFiles) {
			ff.files["/config/templates/00-existing.yaml"] = "- switch:\n  - name: Bath Heater Safe\n    unique_id: bath_heater_safe\n    turn_on: " + turnOn + "\n"
		}
	}
	plugOn := func(change func(*fakeHA)) func(*fakeHA) {
		return func(f *fakeHA) {
			files(nil)(f)
			change(f)
		}
	}
	runTierCases(t, []tierCase{
		{name: "a cover call still starts the watchdog", setup: files(nil), args: []string{"cover.close_cover", "cover.shutter_left"}, exit: 3, tier: "typed",
			reasons: []string{reasonUnresolvedTarget}, check: reachesWatchdog(true)},
		{name: "file access off", setup: files(func(ff *fakeFiles) { ff.off = true }), args: light, exit: 3, tier: "typed", check: helperCause(yamlFilesOff)},
		{name: "a heater file restored over the loaded one", setup: files(func(ff *fakeFiles) { ff.changed["/config/templates/00-existing.yaml"] = yamlTestAfter }),
			args: light, exit: 3, tier: "typed", check: helperCause(yamlChanged)},
		{name: "a second thermostat in YAML that isn't running", setup: files(func(ff *fakeFiles) {
			ff.files["/config/climate.yaml"] += "- {platform: generic_thermostat, heater: switch.bath_plug}\n"
		}), args: light, exit: 3, tier: "typed", check: helperCause("yaml_mismatch")},
		{name: "a legacy template switch with the heater's unique id", setup: files(func(ff *fakeFiles) {
			ff.files["/config/switch.yaml"] = "- platform: template\n  switches:\n    old:\n      unique_id: bath_heater_safe\n"
		}), args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a heater that opens the garage", setup: files(heaterSwitch("{action: cover.open_cover, target: {entity_id: cover.garage_door}}")),
			args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a heater action with data", setup: files(heaterSwitch("{action: switch.turn_on, target: {entity_id: switch.bath_plug}, data: {x: 1}}")),
			args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a heater in a trigger block", setup: files(func(ff *fakeFiles) {
			ff.files["/config/templates/00-existing.yaml"] = "- triggers: [{trigger: time, at: '07:00'}]\n  switch:\n  - unique_id: bath_heater_safe\n    turn_on: {action: switch.turn_on, target: {entity_id: switch.bath_plug}}\n"
		}), args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a heater that switches another domain", setup: files(heaterSwitch("{action: switch.turn_on, target: {entity_id: fan.bath}}")),
			args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a plug running a command", setup: plugOn(func(f *fakeHA) {
			f.ent("switch.bath_plug", "command_line", "off", map[string]any{"_di": "dplug"})
		}), args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a plug on a device with a lock", setup: plugOn(func(f *fakeHA) {
			f.ent("lock.bath_door", "tasmota", "locked", map[string]any{"_di": "dplug"})
		}), args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a plug an automation follows", setup: plugOn(func(f *fakeHA) {
			f.automation("plug_follow", "plug_follow", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "switch.bath_plug"}}, "actions": []any{step("lock.unlock", "lock.front")}})
			f.related["entity:switch.bath_plug"] = []string{"automation.plug_follow"}
		}), args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a reconnect sync whose run another automation follows", setup: plugOn(func(f *fakeHA) {
			f.automation("sync_follow", "sync_follow", map[string]any{"triggers": []any{map[string]any{"trigger": "event", "event_type": "automation_triggered"}}, "actions": []any{step("input_boolean.turn_on", "input_boolean.shutter_watchdog_retry_active")}})
			f.events = append(f.events, map[string]any{"event": "automation_triggered", "listener_count": 1.0})
		}), args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a reconnect sync whose state another automation follows", setup: plugOn(func(f *fakeHA) {
			f.automation("sync_state", "sync_state", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "automation.plug_reconnect_sync", "attribute": "last_triggered"}}, "actions": []any{step("lock.unlock", "lock.front")}})
			f.related["entity:automation.plug_reconnect_sync"] = []string{"automation.plug_reconnect_sync", "automation.sync_state"}
		}), args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a reconnect sync from unknown", setup: plugOn(func(f *fakeHA) {
			cfg := reconnectSyncConfig()
			cfg["triggers"].([]any)[0].(map[string]any)["from"] = "unknown"
			f.automation("plug_reconnect_sync", "plug_reconnect_sync", cfg)
		}), args: light, exit: 3, tier: "typed", check: helperCause("heater_actions")},
		{name: "a thermostat an automation follows", setup: plugOn(func(f *fakeHA) {
			f.automation("bath_follow", "bath_follow", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "climate.bath_thermostat"}}, "actions": []any{step("lock.unlock", "lock.front")}})
			f.related["entity:climate.bath_thermostat"] = []string{"automation.bath_follow"}
		}), args: light, exit: 3, tier: "typed", check: helperCause("helper_listened")},
		{name: "a template integration that isn't built in", setup: plugOn(func(f *fakeHA) {
			for i, m := range f.manifests {
				if m["domain"] == "template" {
					f.manifests[i] = map[string]any{"domain": "template", "is_built_in": false}
				}
			}
		}), args: light, exit: 3, tier: "typed"},
	})
}

// A YAML helpers read with no answer (here each request times out) is a
// failed read, as any read (#827): SCAN_FAILED of the helper, or the
// budget's LIMIT_CUT, never yaml_unread, so a cut check names the same gap
// wherever its reads ran out.
func TestYAMLHelpersWithNoAnswerAreAFailedRead(t *testing.T) {
	saved := relaySessionRequestTimeout
	relaySessionRequestTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relaySessionRequestTimeout = saved })
	runTierCases(t, []tierCase{{name: "the files read never answers", setup: func(f *fakeHA) {
		mar245Home(t, newFakeFiles(mar245Files()))(f)
		f.hang["/config"] = true
	}, args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", absent: []string{reasonHelperCalls}, check: func(t *testing.T, env verbTestEnvelope) {
		if !slices.ContainsFunc(env.Reasons, func(r tierReason) bool {
			return r.Code == reasonScanFailed && r.EntityID == "climate.bath_thermostat" && r.Detail == "yaml helpers"
		}) {
			t.Fatalf("reasons %+v lack the failed YAML helpers read", env.Reasons)
		}
	}}})
}
