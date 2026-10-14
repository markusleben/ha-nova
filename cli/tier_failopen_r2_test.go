package main

import (
	"strings"
	"testing"
)

// Round-2 regressions (fail-open hunt + codex review of d2dd8779).
func TestPlanCallFailOpenRegressionsRound2(t *testing.T) {
	unlockOn := func(f *fakeHA, trigger string) {
		f.related["entity:"+trigger] = []string{"automation.door"}
		f.automation("door", "d1", map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": trigger}},
			"actions":  []any{step("lock.unlock", "lock.front")},
		})
	}
	guest := func(f *fakeHA, actions ...any) {
		f.ent("input_boolean.guest", "input_boolean", "off", nil)
		f.related["entity:input_boolean.guest"] = []string{"automation.guest"}
		f.automation("guest", "g1", map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}},
			"actions":  actions,
		})
	}
	lockNode := func(f *fakeHA) {
		f.ent("lock.side", "zwave_js", "locked", map[string]any{"_di": "dev_lock"})
		f.ent("sensor.side_battery", "zwave_js", "90", map[string]any{"_di": "dev_lock"})
	}
	helper := []string{"input_boolean.turn_on", "input_boolean.guest"}
	apply := func(v string) map[string]any {
		return map[string]any{"action": "scene.apply", "data": map[string]any{"entities": map[string]any{"group.doors": v}}}
	}
	cases := []tierCase{
		{name: "A integration service on a sibling entity of the lock's device", setup: lockNode,
			args: []string{"zwave_js.set_value", "sensor.side_battery", "--set", "command_class=98", "--set", "value=0"}, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccess}},
		{name: "A stored integration service on a sibling entity", setup: func(f *fakeHA) {
			lockNode(f)
			guest(f, map[string]any{"action": "zwave_js.set_value", "target": map[string]any{"entity_id": "sensor.side_battery"}})
		}, args: helper, exit: 3, tier: "typed", reasons: []string{reasonDeviceAccess}},
		{name: "B a harmless target never lowers an integration service below preview",
			args: []string{"hue.activate_scene", "light.kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonIntegrationSvc}},
		{name: "C consumer of the expanded group entity itself", setup: func(f *fakeHA) {
			f.ent("light.down", "group", "on", map[string]any{"entity_id": []any{"light.kitchen"}})
			unlockOn(f, "light.down")
		}, args: []string{"light.turn_off", "light.down"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock},
			check: func(t *testing.T, env verbTestEnvelope) {
				if strings.Join(env.Action.EntityIDs, ",") != "light.kitchen" {
					t.Fatalf("frozen ids %v: the group must stay out of the payload", env.Action.EntityIDs)
				}
			}},
		{name: "C2 group containing the acted leaf, found via search/related", setup: func(f *fakeHA) {
			f.ent("group.downstairs", "group", "on", map[string]any{"entity_id": []any{"light.kitchen"}})
			f.relatedGroups = map[string][]string{"light.kitchen": {"group.downstairs"}}
			unlockOn(f, "group.downstairs")
		}, args: []string{"light.turn_off", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "D switch_as_x lock: turning on the wrapped switch unlocks", setup: func(f *fakeHA) {
			f.ent("lock.gate", "switch_as_x", "locked", nil)
			f.ent("switch.gate_relay", "shelly", "off", nil)
		}, args: []string{"switch.turn_on", "switch.gate_relay"}, exit: 3, tier: "typed", reasons: []string{reasonSwitchAsX}},
		{name: "D switch_as_x on another device is not attributed", setup: func(f *fakeHA) {
			f.ent("lock.gate", "switch_as_x", "locked", map[string]any{"_di": "dev_gate"})
			f.ent("switch.lamp", "shelly", "off", map[string]any{"_di": "dev_lamp"})
		}, args: []string{"switch.turn_on", "switch.lamp"}, exit: 0, tier: "ordinary"},
		{name: "E rest platform switch (user-authored request)", setup: func(f *fakeHA) { f.ent("switch.gate", "rest", "off", nil) },
			args: []string{"switch.turn_on", "switch.gate"}, exit: 3, tier: "typed", reasons: []string{reasonUnreadCommand}},
		{name: "E telnet platform switch", setup: func(f *fakeHA) { f.ent("switch.door", "telnet", "off", nil) },
			args: []string{"switch.turn_on", "switch.door"}, exit: 3, tier: "typed", reasons: []string{reasonUnreadCommand}},
		{name: "F homeassistant.toggle on a siren is disruptive", setup: func(f *fakeHA) { f.ent("siren.yard", "zha", "off", nil) },
			args: []string{"homeassistant.toggle", "siren.yard"}, exit: 3, tier: "preview", reasons: []string{reasonDisruptive}},
		{name: "G pyscript service (user-authored python)", setup: func(f *fakeHA) { f.entries = append(f.entries, map[string]any{"domain": "pyscript"}) },
			args: []string{"pyscript.open_garage"}, exit: 3, tier: "typed", reasons: []string{reasonUnreadCommand}},
		{name: "G netdaemon service", args: []string{"netdaemon.open_gate", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonUnreadCommand}},
		{name: "H automation_triggered listener of a consumer run", setup: func(f *fakeHA) {
			guest(f, step("light.turn_on", "light.hall"))
			f.automation("door", "d1", map[string]any{
				"triggers": []any{map[string]any{"trigger": "event", "event_type": "automation_triggered", "event_data": map[string]any{"entity_id": "automation.guest"}}},
				"actions":  []any{step("lock.unlock", "lock.front")},
			})
			f.events = append(f.events, map[string]any{"event": "automation_triggered", "listener_count": 1})
		}, args: helper, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "H unexplained script_started listener is preview", setup: func(f *fakeHA) {
			f.script("leave", step("light.turn_off", "light.hall"))
			f.events = []map[string]any{{"event": "script_started", "listener_count": 1}}
		}, args: []string{"script.turn_on", "script.leave"}, exit: 3, tier: "preview", reasons: []string{reasonRunListenerUnexpl}},
		{name: "I custom cover device_class is access-capable", setup: func(f *fakeHA) {
			f.ent("cover.gate", "custom_gate", "closed", map[string]any{"device_class": "garage_door"})
		}, args: []string{"cover.open_cover", "cover.gate"}, exit: 3, tier: "typed", reasons: []string{reasonAccessCover}},
		{name: "J delayed unlock in a consumer", setup: func(f *fakeHA) {
			guest(f, map[string]any{"delay": "00:05:00"}, map[string]any{"wait_template": "{{ true }}", "continue_on_timeout": true},
				map[string]any{"action": "lock.unlock", "target": map[string]any{"entity_id": "lock.front"}, "continue_on_error": true})
		}, args: helper, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "K a consumer that is switched off still counts", setup: func(f *fakeHA) {
			guest(f, step("lock.unlock", "lock.front"))
			f.states["automation.guest"]["state"] = "off"
		}, args: helper, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "L script.<name> whose variables pick the lock", setup: func(f *fakeHA) {
			f.script("leave", map[string]any{"action": "lock.unlock", "target": map[string]any{"entity_id": "{{ door }}"}})
		}, args: []string{"script.leave", "--set", "door=lock.front"}, exit: 3, tier: "typed", reasons: []string{reasonUnresolvedTarget}},
		{name: "M scene-group dedupe keys on the requested state", setup: func(f *fakeHA) {
			f.ent("group.doors", "group", "locked", map[string]any{"entity_id": []any{"lock.front"}})
			guest(f, apply("locked"), apply("unlocked"))
		}, args: helper, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "--device keeps a typed gap when the registry read failed", setup: func(f *fakeHA) {
			f.fail["config/entity_registry/list_for_display"] = true
		}, args: []string{"light.turn_off", "--device", "dev1"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed, reasonUnresolvedTarget}},
		{name: "a template-triggered consumer adds the templates note", setup: func(f *fakeHA) {
			f.ent("input_boolean.guest", "input_boolean", "off", nil)
			f.related["entity:input_boolean.guest"] = []string{"automation.tpl"}
			f.automation("tpl", "t1", map[string]any{
				"triggers": []any{map[string]any{"trigger": "template", "value_template": "{{ is_state('input_boolean.guest', 'on') }}"}},
				"actions":  []any{step("light.turn_on", "light.hall")},
			})
		}, args: helper, exit: 0, tier: "ordinary", check: func(t *testing.T, env verbTestEnvelope) {
			if env.Coverage.Notes[0] != noteTemplates {
				t.Fatalf("notes %v", env.Coverage.Notes)
			}
		}},
	}
	runTierCases(t, cases)
}

func TestPlanCallRefusesTargetInServiceData(t *testing.T) {
	paths, log := setupVerbRelayTest(t, newFakeHA().reply)
	exit, env := runVerbForTest(t, func() int {
		return runPlanCommand(paths, []string{"call", "script.leave", "--set", "target=lock.front"})
	})
	if exit != verbExitError || env.Error == nil || env.Error.Code != "USAGE" || len(log.all()) != 0 {
		t.Fatalf("exit %d env %+v", exit, env)
	}
}
