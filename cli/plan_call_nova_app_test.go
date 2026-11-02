package main

import (
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
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Next, "couldn't read Home Assistant's registry") {
			t.Errorf("unreadable registry: exit %d %+v", exit, env.Error)
		}
	}
}
