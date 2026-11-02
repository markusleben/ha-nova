package main

import (
	"fmt"
	"strings"
	"testing"
)

// HA NOVA never stops or restarts the App it runs in, through
// hassio.addon_* or through that App's own switch: it would cut itself off
// from Home Assistant mid-call. Updating it (update.install) and controlling
// another App stay as they were. A switch it can't tell apart from the App's
// (the registry can't be read) is refused too.
func TestCallNeverStopsTheNOVAApp(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		refused bool
	}{
		{[]string{"hassio.addon_stop", "--skill", "ha-nova:service-call", "--set", "addon=2368fcfa_ha_nova_relay"}, true},
		{[]string{"hassio.addon_restart", "--skill", "ha-nova:service-call", "--set", "addon=local_ha_nova_relay"}, true},
		{[]string{"hassio.addon_restart", "--set", "addon=2368fcfa_ha_nova_relay"}, true},
		{[]string{"switch.turn_off", "switch.nova_relay"}, true},
		{[]string{"homeassistant.toggle", "switch.nova_relay"}, true},
		{[]string{"hassio.addon_restart", "--skill", "ha-nova:service-call", "--set", "addon=core_mosquitto"}, false},
		{[]string{"switch.turn_off", "switch.mosquitto"}, false},
		{appInstall, false},
	} {
		f := appUpdateHA()
		f.ent("switch.nova_relay", "hassio", "on", map[string]any{"_di": "app_device"})
		f.ent("switch.mosquitto", "hassio", "on", map[string]any{"_di": "mqtt_device"})
		f.devices = []map[string]any{
			{"id": "app_device", "identifiers": []any{[]any{"hassio", "2368fcfa_ha_nova_relay"}}},
			{"id": "mqtt_device", "identifiers": []any{[]any{"hassio", "core_mosquitto"}}},
		}
		exit, env, _, _ := runPlanForTest(t, f, tc.args...)
		own := exit == verbExitRefused && env.Tier == "refused" && env.Error != nil && strings.Contains(env.Error.Next, "its own App") &&
			strings.Contains(env.Error.Next, "Settings > Apps")
		if tc.refused != own || !tc.refused && (exit == verbExitRefused || env.Tier == "refused") {
			t.Errorf("%v: exit %d tier %q %+v %+v", tc.args, exit, env.Tier, env.Reasons, env.Error)
		}
	}
	for _, unread := range []func(f *fakeHA){
		func(f *fakeHA) { f.fail["config/entity_registry/list_for_display"] = true },
		func(f *fakeHA) { f.devices = nil },
		func(f *fakeHA) { f.devices = []map[string]any{{"id": "mqtt_device"}} },
	} {
		f := appUpdateHA()
		f.ent("switch.mosquitto", "hassio", "on", map[string]any{"_di": "mqtt_device"})
		unread(f)
		exit, env, _, _ := runPlanForTest(t, f, "switch.turn_off", "switch.mosquitto")
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Next, "couldn't tell from Home Assistant's registry") {
			t.Errorf("unreadable registry: exit %d %+v", exit, env.Error)
		}
	}
}

// What a call runs can stop the App too: a scene's members, or a script's
// or an automation's steps, through the App's switch or hassio.addon_*.
// Saving a draft with such a step is not refused: it runs later.
func TestCallNeverStopsTheNOVAAppThroughWhatItRuns(t *testing.T) {
	addonStop := map[string]any{"action": "hassio.addon_stop", "data": map[string]any{"addon": "2368fcfa_ha_nova_relay"}}
	app := func() *fakeHA {
		f := appUpdateHA()
		f.ent("switch.nova_relay", "hassio", "on", map[string]any{"_di": "app_device"})
		f.ent("switch.mosquitto", "hassio", "on", map[string]any{"_di": "mqtt_device"})
		f.devices = []map[string]any{
			{"id": "app_device", "identifiers": []any{[]any{"hassio", "2368fcfa_ha_nova_relay"}}},
			{"id": "mqtt_device", "identifiers": []any{[]any{"hassio", "core_mosquitto"}}},
		}
		return f
	}
	for _, tc := range []struct {
		name    string
		setup   func(f *fakeHA)
		args    []string
		refused bool
	}{
		{"scene.apply", func(f *fakeHA) {
			f.services = append(f.services, map[string]any{"domain": "scene", "services": map[string]any{"apply": map[string]any{}}})
		}, []string{"scene.apply", "--set", `entities={"switch.nova_relay":"off"}`}, true},
		{"stored scene", func(f *fakeHA) {
			f.ent("scene.away", "homeassistant", "unknown", nil)
			f.unique["scene.away"] = "s1"
			f.configs["/api/config/scene/config/s1"] = map[string]any{"entities": map[string]any{"switch.nova_relay": "off"}}
		}, []string{"scene.turn_on", "scene.away"}, true},
		{"script switch", func(f *fakeHA) { f.script("leave", step("switch.turn_off", "switch.nova_relay")) }, []string{"script.turn_on", "script.leave"}, true},
		{"script addon_stop", func(f *fakeHA) { f.script("leave", addonStop) }, []string{"script.turn_on", "script.leave"}, true},
		{"automation addon_restart", func(f *fakeHA) {
			f.automation("night", "n1", map[string]any{"actions": []any{map[string]any{"action": "hassio.addon_restart", "data": map[string]any{"addon": "local_ha_nova_relay"}}}})
		}, []string{"automation.trigger", "automation.night"}, true},
		{"unreadable state", func(f *fakeHA) { f.fail["/api/states"], f.fail["/api/states/switch.nova_relay"] = true, true }, []string{"switch.turn_off", "switch.nova_relay"}, true},
		{"scene member with unreadable state", func(f *fakeHA) {
			f.services = append(f.services, map[string]any{"domain": "scene", "services": map[string]any{"apply": map[string]any{}}})
			f.fail["/api/states"], f.fail["/api/states/switch.nova_relay"] = true, true
		}, []string{"scene.apply", "--set", `entities={"switch.nova_relay":"off"}`}, true},
		{"every switch", func(f *fakeHA) {
			f.script("leave", map[string]any{"action": "switch.turn_off", "target": map[string]any{"entity_id": "all"}})
		}, []string{"script.turn_on", "script.leave"}, true},
		{"every entity", func(f *fakeHA) {
			f.script("leave", map[string]any{"action": "homeassistant.turn_off", "data": map[string]any{"entity_id": "all"}})
		}, []string{"script.turn_on", "script.leave"}, true},
		{"a switch step without a target", func(f *fakeHA) { f.script("leave", map[string]any{"action": "switch.toggle"}) }, []string{"script.turn_on", "script.leave"}, true},
		{"the App's switch beside a templated area", func(f *fakeHA) {
			f.script("leave", map[string]any{"action": "switch.turn_off", "target": map[string]any{"entity_id": []any{"switch.nova_relay"}, "area_id": "{{ area }}"}})
		}, []string{"script.turn_on", "script.leave"}, true},
		{"a light on the App's switch", func(f *fakeHA) { f.ent("light.nova_lamp", "switch_as_x", "on", map[string]any{"_di": "app_device"}) },
			[]string{"light.turn_off", "light.nova_lamp"}, true},
		{"every light, one on the App's switch", func(f *fakeHA) {
			f.ent("light.nova_lamp", "switch_as_x", "on", map[string]any{"_di": "app_device"})
			f.script("leave", map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": "all"}})
		}, []string{"script.turn_on", "script.leave"}, true},
		{"a light on another App's switch", func(f *fakeHA) { f.ent("light.mqtt_lamp", "switch_as_x", "on", map[string]any{"_di": "mqtt_device"}) },
			[]string{"light.turn_off", "light.mqtt_lamp"}, false},
		{"a script named beside a templated area", func(f *fakeHA) {
			f.script("stop", addonStop)
			f.script("leave", map[string]any{"action": "script.turn_on", "target": map[string]any{"entity_id": "script.stop", "area_id": "{{ area }}"}})
		}, []string{"script.turn_on", "script.leave"}, true},
		{"deeper than the traversal goes", func(f *fakeHA) {
			f.script("s8", step("switch.turn_off", "switch.nova_relay"))
			for i := 7; i >= 0; i-- {
				f.script(fmt.Sprintf("s%d", i), step("script.turn_on", fmt.Sprintf("script.s%d", i+1)))
			}
		}, []string{"script.turn_on", "script.s0"}, true},
		{"every light", func(f *fakeHA) {
			f.script("leave", map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": "all"}})
		}, []string{"script.turn_on", "script.leave"}, false},
		{"another App", func(f *fakeHA) {
			f.script("leave", map[string]any{"action": "hassio.addon_restart", "data": map[string]any{"addon": "core_mosquitto"}}, step("switch.turn_off", "switch.mosquitto"))
		}, []string{"script.turn_on", "script.leave"}, false},
	} {
		f := app()
		tc.setup(f)
		exit, env, _, _ := runPlanForTest(t, f, tc.args...)
		own := exit == verbExitRefused && env.Tier == "refused" && env.Error != nil && strings.Contains(env.Error.Next, "its own App")
		if tc.refused != own || !tc.refused && (exit == verbExitRefused || env.Tier == "refused") {
			t.Errorf("%s: exit %d tier %q %+v %+v", tc.name, exit, env.Tier, env.Reasons, env.Error)
		}
	}
	f := appUpdateHA()
	f.ent("switch.mosquitto", "hassio", "on", map[string]any{"_di": "mqtt_device"})
	f.devices = []map[string]any{{"id": "mqtt_device", "identifiers": []any{[]any{"hassio", "core_mosquitto"}}}}
	f.script("leave", map[string]any{"action": "switch.turn_off", "target": map[string]any{"entity_id": "all"}})
	if exit, env, _, _ := runPlanForTest(t, f, "script.turn_on", "script.leave"); exit == verbExitRefused || env.Tier == "refused" {
		t.Errorf("every switch, none the App's: exit %d %+v", exit, env.Error)
	}
	h := newWriteHarness(t, app())
	draft := map[string]any{"alias": "Night", "triggers": []any{map[string]any{"trigger": "time", "at": "03:00:00"}},
		"actions": []any{step("switch.turn_off", "switch.nova_relay"), addonStop}}
	if exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, draft)); exit != verbExitConfirmation || env.Tier == "refused" || env.Plan == nil {
		t.Errorf("saving a draft that stops the App: exit %d tier %q %+v", exit, env.Tier, env.Error)
	}
}

// A switch HA NOVA can't place in the registry (no row, or a row without a
// platform) might be the App's: turning it off is refused, and so is a step
// on every switch while a row lacks its platform (the App's switch always
// has a row).
func TestCallRefusesASwitchMissingFromTheRegistry(t *testing.T) {
	everySwitch := []string{"script.turn_on", "script.leave"}
	for name, setup := range map[string]func(f *fakeHA) [][]string{
		"no row": func(f *fakeHA) [][]string { f.ent("switch.relay", "", "on", nil); return nil },
		"no platform": func(f *fakeHA) [][]string {
			f.ent("switch.relay", "", "on", nil)
			f.compact = append(f.compact, map[string]any{"ei": "switch.relay"})
			return [][]string{everySwitch}
		},
	} {
		f := appUpdateHA()
		f.script("leave", map[string]any{"action": "switch.turn_off", "target": map[string]any{"entity_id": "all"}})
		for _, args := range append(setup(f), []string{"switch.turn_off", "switch.relay"}) {
			exit, env, _, _ := runPlanForTest(t, f, args...)
			if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Next, "couldn't tell from Home Assistant's registry") {
				t.Errorf("%s %v: exit %d %+v", name, args, exit, env.Error)
			}
		}
	}
}
