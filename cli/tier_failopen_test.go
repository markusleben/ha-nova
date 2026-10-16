package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Regressions from the adversarial review of PR2: each row was a fail-open
// (or a wrong exit) before the fix.
func TestPlanCallFailOpenRegressions(t *testing.T) {
	guest := func(f *fakeHA, actions ...any) { // helper whose consumer automation runs `actions`
		f.ent("input_boolean.guest", "input_boolean", "off", nil)
		f.related["entity:input_boolean.guest"] = []string{"automation.guest"}
		f.automation("guest", "g1", map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}},
			"actions":  actions,
		})
	}
	garage := func(f *fakeHA) { f.ent("cover.garage", "ratgdo", "closed", map[string]any{"device_class": "garage"}) }
	helper := []string{"input_boolean.turn_on", "input_boolean.guest"}
	typed := func(name string, setup func(*fakeHA), args []string, reasons ...string) tierCase {
		return tierCase{name: name, setup: setup, args: args, exit: 3, tier: "typed", reasons: reasons}
	}
	setPosition := func(p float64) map[string]any {
		return map[string]any{"action": "cover.set_cover_position", "target": map[string]any{"entity_id": "cover.garage"}, "data": map[string]any{"position": p}}
	}
	cases := []tierCase{
		typed("stored scene.apply unlocking a lock is expanded", func(f *fakeHA) {
			guest(f, map[string]any{"action": "scene.apply", "data": map[string]any{"entities": map[string]any{"lock.front": "unlocked"}}})
		}, helper, reasonAccessLock),
		typed("stored scene.apply inside a script run", func(f *fakeHA) {
			f.script("leave", map[string]any{"action": "scene.apply", "data": map[string]any{"entities": map[string]any{"lock.front": "unlocked"}}})
		}, []string{"script.turn_on", "script.leave"}, reasonAccessLock),
		typed("visit dedupe keys on data: position 0 then 100 on a garage", func(f *fakeHA) {
			garage(f)
			guest(f, setPosition(0), setPosition(100))
		}, helper, reasonAccessCover),
		typed("data_template overrides data", func(f *fakeHA) {
			garage(f)
			guest(f, map[string]any{"action": "cover.set_cover_position", "target": map[string]any{"entity_id": "cover.garage"},
				"data": map[string]any{"position": 0.0}, "data_template": map[string]any{"position": 100.0}})
		}, helper, reasonAccessCover),
		typed("scene member group 'unlocked' reproduces unlock on its leaves", func(f *fakeHA) {
			f.ent("group.doors", "group", "locked", map[string]any{"entity_id": []any{"lock.front"}})
			guest(f, map[string]any{"scene": "scene.x"})
			f.ent("scene.x", "homeassistant", "unknown", nil)
			f.unique["scene.x"] = "x1"
			f.configs["/api/config/scene/config/x1"] = map[string]any{"entities": map[string]any{"group.doors": "unlocked"}}
		}, helper, reasonAccessLock),
		typed("whole-target template on a nested script run", func(f *fakeHA) {
			f.script("evil", step("lock.unlock", "lock.front"))
			f.script("leave", map[string]any{"action": "script.turn_on", "target": "{{ {'entity_id': 'script.evil'} }}"})
		}, []string{"script.turn_on", "script.leave"}, reasonUnresolvedTarget),
		typed("whole-target template in a consumer", func(f *fakeHA) {
			guest(f, map[string]any{"action": "script.turn_on", "target": "{{ {'entity_id': 'script.x'} }}"})
		}, helper, reasonUnresolvedTarget),
		typed("templated data payload", func(f *fakeHA) {
			guest(f, map[string]any{"action": "cover.set_cover_position", "target": map[string]any{"entity_id": "cover.blind"}, "data": "{{ payload }}"})
		}, helper, reasonUnresolvedTarget),
		typed("templated area_id on homeassistant.toggle", func(f *fakeHA) {
			guest(f, map[string]any{"action": "homeassistant.toggle", "target": map[string]any{"area_id": "{{ 'garage' }}"}})
		}, helper, reasonUnresolvedTarget),
		typed("templated device_id on button.press", func(f *fakeHA) {
			guest(f, map[string]any{"action": "button.press", "target": map[string]any{"device_id": "{{ d }}"}})
		}, helper, reasonUnresolvedTarget),
		typed("integration service on a lock (zwave_js.set_value)", nil,
			[]string{"zwave_js.set_value", "lock.front", "--set", "command_class=98", "--set", "property=targetMode", "--set", "value=0"}, reasonAccessLock),
		typed("integration service on a lock (nuki.lock_n_go)", nil, []string{"nuki.lock_n_go", "lock.front"}, reasonAccessLock),
		typed("integration service on an alarm (envisalink keypress)", func(f *fakeHA) {
			f.ent("alarm_control_panel.home", "envisalink", "armed_away", nil)
		}, []string{"envisalink.alarm_keypress", "alarm_control_panel.home", "--set", "keypress=1"}, reasonAccessAlarm),
		typed("stored zwave_js.set_value on a lock", func(f *fakeHA) {
			guest(f, map[string]any{"action": "zwave_js.set_value", "target": map[string]any{"entity_id": "lock.front"}, "data": map[string]any{"value": 0}})
		}, helper, reasonAccessLock),
		typed("device-form step of an integration domain on a lock device", func(f *fakeHA) {
			f.ent("lock.side", "zwave_js", "locked", map[string]any{"_di": "dev_lock"})
			f.script("leave", map[string]any{"device_id": "dev_lock", "domain": "zwave_js", "type": "set_lock_usercode"})
		}, []string{"script.leave"}, reasonAccessLock),
		typed("user-authored ESPHome API service", nil, []string{"esphome.garage_open"}, reasonUserService),
		typed("command_line switch runs a shell command", func(f *fakeHA) {
			f.ent("switch.garage_cmd", "command_line", "off", nil)
		}, []string{"switch.turn_on", "switch.garage_cmd"}, reasonUnreadCommand),
		typed("any targetless reload", nil, []string{"group.reload"}, reasonReload),
		typed("reload_config_entry", nil, []string{"homeassistant.reload_config_entry", "--set", "entry_id=abc"}, reasonReload),
		typed("stop_cover on a closing garage leaves it open", func(f *fakeHA) {
			f.ent("cover.garage", "ratgdo", "closing", map[string]any{"device_class": "garage"})
		}, []string{"cover.stop_cover", "cover.garage"}, reasonAccessCover),
		typed("homeassistant.toggle on a garage can open it", garage, []string{"homeassistant.toggle", "cover.garage"}, reasonAccessCover),
		typed("legacy trigger/action/service spellings in a consumer", func(f *fakeHA) {
			f.ent("input_boolean.guest", "input_boolean", "off", nil)
			f.related["entity:input_boolean.guest"] = []string{"automation.g"}
			f.automation("g", "g1", map[string]any{
				"trigger": []any{map[string]any{"platform": "state", "entity_id": "input_boolean.guest"}},
				"action":  []any{map[string]any{"service": "lock.unlock", "entity_id": "lock.front"}},
			})
		}, helper, reasonAccessLock),
		typed("area-targeted trigger counts as a consumer", func(f *fakeHA) {
			f.related["entity:light.kitchen"] = []string{"automation.g"}
			f.automation("g", "g1", map[string]any{
				"triggers": []any{map[string]any{"trigger": "light.turned_on", "target": map[string]any{"area_id": "kitchen"}}},
				"actions":  []any{step("lock.unlock", "lock.front")},
			})
		}, []string{"light.turn_on", "light.kitchen"}, reasonAccessLock),
		typed("integration scene whose platform cover has no class in its state", func(f *fakeHA) {
			f.ent("scene.morning", "hunterdouglas_powerview", "unknown", nil)
			f.unique["scene.morning"] = "pv-1"
			f.ent("cover.mystery", "hunterdouglas_powerview", "closed", nil)
			f.full = []map[string]any{{"entity_id": "cover.mystery", "platform": "hunterdouglas_powerview"}}
		}, []string{"scene.turn_on", "scene.morning"}, reasonSceneAccess),
		typed("nested {triggers: [...]} listener lists are enumerated", func(f *fakeHA) {
			f.events = []map[string]any{{"event": "call_service", "listener_count": 1}}
			f.automation("watch", "w1", map[string]any{
				"triggers": []any{map[string]any{"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service"}}}},
				"actions":  []any{step("lock.unlock", "lock.front")},
			})
		}, []string{"light.turn_on", "light.kitchen"}, reasonAccessLock),
		typed("stored event nobody can enumerate is typed", func(f *fakeHA) {
			f.events = []map[string]any{{"event": "garage_go", "listener_count": 1}}
			f.script("leave", map[string]any{"event": "garage_go"})
		}, []string{"script.turn_on", "script.leave"}, reasonListener),
		typed("failed group leaf read is typed, never exit 1", func(f *fakeHA) {
			f.ent("light.all", "group", "on", map[string]any{"entity_id": []any{"light.kitchen", "light.hall"}})
			f.fail["/api/states/light.hall"] = true
		}, []string{"light.turn_off", "light.all"}, reasonScanFailed),
		{name: "integration scene with blinds only stays preview", setup: func(f *fakeHA) {
			f.ent("scene.morning", "hunterdouglas_powerview", "unknown", nil)
			f.unique["scene.morning"] = "pv-1"
			f.ent("cover.living_blind", "hunterdouglas_powerview", "open", map[string]any{"device_class": "blind"})
			f.full = []map[string]any{{"entity_id": "cover.living_blind", "platform": "hunterdouglas_powerview"}}
		}, args: []string{"scene.turn_on", "scene.morning"}, exit: 3, tier: "preview", reasons: []string{reasonSceneUnread}},
		{name: "repeat.for_each items are data, not actions", setup: func(f *fakeHA) {
			f.script("s", map[string]any{"repeat": map[string]any{"for_each": []any{map[string]any{"action": "on", "who": "x"}}, "sequence": []any{step("light.turn_on", "light.hall")}}})
		}, args: []string{"script.turn_on", "script.s"}, exit: 3, tier: "preview", absent: []string{reasonTemplatedAction}},
		{name: "targetless integration service is at least preview", args: []string{"hue.activate_scene", "--set", "group_name=x"},
			exit: 3, tier: "preview", reasons: []string{reasonIntegrationSvc}},
		{name: "safe calls on access entities stay ordinary", args: []string{"lock.lock", "lock.front"}, exit: 0, tier: "ordinary"},
		{name: "homeassistant.turn_off on a garage maps to close_cover", setup: garage, args: []string{"homeassistant.turn_off", "cover.garage"}, exit: 0, tier: "ordinary"},
		{name: "homeassistant.update_entity on a lock stays ordinary", args: []string{"homeassistant.update_entity", "lock.front"}, exit: 0, tier: "ordinary"},
		{name: "opening a blind is ordinary", setup: func(f *fakeHA) { f.ent("cover.blind", "somfy", "closed", map[string]any{"device_class": "blind"}) },
			args: []string{"cover.open_cover", "cover.blind"}, exit: 0, tier: "ordinary"},
		{name: "area leaf without state is dropped with a note, not exit 5", setup: func(f *fakeHA) {
			f.related["area:kitchen"] = []string{"light.kitchen", "light.ghost"}
		}, args: []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", check: func(t *testing.T, env verbTestEnvelope) {
			if strings.Join(env.Action.EntityIDs, ",") != "light.kitchen" || !strings.Contains(strings.Join(env.Coverage.Notes, ","), noteSkippedTargets) {
				t.Fatalf("action %+v coverage %+v", env.Action, env.Coverage)
			}
		}},
		{name: "frozen leaves are capped at 64", setup: func(f *fakeHA) {
			for i := 0; i < 70; i++ {
				id := fmt.Sprintf("light.l%02d", i)
				f.ent(id, "hue", "on", nil)
				f.related["area:big"] = append(f.related["area:big"], id)
			}
		}, args: []string{"light.turn_off", "--area", "big"}, exit: 3, tier: "typed", reasons: []string{reasonLimitCut}, check: func(t *testing.T, env verbTestEnvelope) {
			if len(env.Action.EntityIDs) != tierMaxActed {
				t.Fatalf("%d frozen ids, want %d", len(env.Action.EntityIDs), tierMaxActed)
			}
		}},
		{name: "consumer class counts members classified earlier", setup: func(f *fakeHA) {
			f.script("leave", step("lock.unlock", "lock.front"), step("input_boolean.turn_on", "input_boolean.guest"))
			guest(f, step("lock.unlock", "lock.front"))
		}, args: []string{"script.turn_on", "script.leave"}, exit: 3, tier: "typed", check: func(t *testing.T, env verbTestEnvelope) {
			if len(env.Consumers) != 1 || env.Consumers[0].Class != "typed" {
				t.Fatalf("consumers %+v", env.Consumers)
			}
		}},
		{name: "scan failure details are stable words, not error text", setup: func(f *fakeHA) { f.fail["search/related"] = true },
			args: []string{"light.turn_off", "light.kitchen"}, exit: 3, tier: "typed", check: func(t *testing.T, env verbTestEnvelope) {
				if env.Reasons[0].Detail != "related" {
					t.Fatalf("reasons %+v", env.Reasons)
				}
			}},
		{name: "multi-target code names the first target and the count", setup: func(f *fakeHA) { f.ent("lock.back", "zwave_js", "locked", nil) },
			args: []string{"lock.unlock", "lock.front", "lock.back"}, exit: 3, tier: "typed", check: func(t *testing.T, env verbTestEnvelope) {
				if !fourDigits.MatchString(env.Plan.Code) {
					t.Fatalf("code %s", env.Plan.Code)
				}
			}},
		{name: "cover and brightness deltas read the live attributes", setup: func(f *fakeHA) {
			f.ent("cover.blind", "somfy", "open", map[string]any{"device_class": "blind", "current_position": 30.0})
			f.ent("light.dim", "hue", "on", nil)
		}, args: []string{"cover.set_cover_position", "cover.blind", "--set", "position=50"}, exit: 0, tier: "ordinary", check: func(t *testing.T, env verbTestEnvelope) {
			if d := env.Deltas[0]; d.Field != "position" || d.Before != "30%" || d.After != "50%" {
				t.Fatalf("deltas %+v", env.Deltas)
			}
			if got := brightnessPct(&entityState{State: "on"}); got != "unknown" {
				t.Fatalf("brightness of an on light without value = %s", got)
			}
		}},
		{name: "null floor_id is still a refused selector", args: []string{"light.turn_off", "light.kitchen", "--set", "floor_id=null"},
			exit: 4, tier: "refused", reasons: []string{reasonSelector}},
	}
	runTierCases(t, cases)
}

func TestPlanCallDeadlineIsLimitCutNotExit1(t *testing.T) {
	previous := relaySessionOverallTimeout
	relaySessionOverallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { relaySessionOverallTimeout = previous })
	f := newFakeHA()
	f.hang["/api/events"] = true
	exit, env, _, _ := runPlanForTest(t, f, "light.turn_off", "light.kitchen")
	if exit != 3 || env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonLimitCut) {
		t.Fatalf("exit %d tier %s reasons %s error %+v", exit, env.Tier, reasonCodes(env), env.Error)
	}
}

func TestPlanCallStaticRefusalNeedsNoRelay(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no config, no token: any Relay use would fail
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"mqtt.publish", "--set", "topic=x"}, {"hassio.addon_stop"}, {"lock.unlock", "lock.x", "--set", "code=null"}} {
		exit, env := runVerbForTest(t, func() int { return runPlanCommand(paths, append([]string{"call"}, args...)) })
		if exit != verbExitRefused || env.Tier != "refused" {
			t.Fatalf("%v: exit %d env %+v", args, exit, env)
		}
	}
}

func TestBlueprintWithoutInputSendsEmptyInput(t *testing.T) {
	f := newFakeHA()
	f.automation("bp", "a1", map[string]any{"use_blueprint": map[string]any{"path": "door.yaml"}})
	f.substitute = map[string]any{"substituted_config": map[string]any{"actions": []any{step("light.turn_on", "light.hall")}}}
	_, _, _, log := runPlanForTest(t, f, "automation.trigger", "automation.bp")
	for _, c := range log.all() {
		if c.payload["type"] == "blueprint/substitute" {
			if input, isMap := c.payload["input"].(map[string]any); !isMap || len(input) != 0 {
				t.Fatalf("substitute input = %#v, want {}", c.payload["input"])
			}
			return
		}
	}
	t.Fatal("blueprint/substitute was never called")
}
