package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestPlanCallTierGoldenCases(t *testing.T) {
	consumer := func(action string) func(*fakeHA) {
		return func(f *fakeHA) {
			f.ent("input_boolean.guest", "input_boolean", "off", nil)
			f.related["entity:input_boolean.guest"] = []string{"automation.guest_lights"}
			f.automation("guest_lights", "g1", map[string]any{
				"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}},
				"actions":  []any{step(action, "lock.front")},
			})
		}
	}
	listener := func(live int) func(*fakeHA) {
		return func(f *fakeHA) {
			f.events = []map[string]any{{"event": "call_service", "listener_count": live}}
			f.automation("watch", "w1", map[string]any{
				"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service", "event_data": map[string]any{"domain": "light"}}},
				"actions":  []any{step("lock.unlock", "lock.front")},
			})
		}
	}
	cases := []tierCase{
		{name: "ordinary light; a template entity elsewhere is a coverage note only",
			setup: func(f *fakeHA) { f.ent("sensor.tpl", "template", "1", nil) },
			args:  []string{"light.turn_on", "light.kitchen", "--set", "brightness_pct=40"}, exit: 0, tier: "ordinary",
			check: func(t *testing.T, env verbTestEnvelope) {
				notes := strings.Join(env.Coverage.Notes, ",")
				if env.Status != "ready" || env.Plan.Confirm != "none" || notes != noteTemplates+","+noteNoManager || !env.Coverage.Complete {
					t.Fatalf("status %s plan %+v coverage %+v", env.Status, env.Plan, env.Coverage)
				}
				if d := env.Deltas[1]; d.Field != "brightness" || d.Before != "100%" || d.After != "40%" {
					t.Fatalf("brightness delta %+v", env.Deltas)
				}
			}},
		{name: "template light carries its author's action: typed",
			setup: func(f *fakeHA) { f.ent("light.tpl", "template", "off", nil) },
			args:  []string{"light.turn_on", "light.tpl"}, exit: 3, tier: "typed", reasons: []string{reasonTemplateAction},
			check: func(t *testing.T, env verbTestEnvelope) {
				if !strings.HasPrefix(env.Plan.Code, "confirm:turn_on-tpl-") || env.Coverage.Notes[0] != noteTemplates {
					t.Fatalf("plan %+v coverage %+v", env.Plan, env.Coverage)
				}
			}},
		{name: "disabled unlock step is not a member",
			setup: func(f *fakeHA) {
				disabled := step("lock.unlock", "lock.front")
				disabled["enabled"] = false
				f.script("leave", disabled, step("light.turn_off", "light.hall"))
			},
			args: []string{"script.turn_on", "script.leave"}, exit: 3, tier: "preview", reasons: []string{reasonStoredMembers}, absent: []string{reasonAccessLock},
			check: func(t *testing.T, env verbTestEnvelope) {
				if len(env.Members) != 1 || env.Members[0].EntityID != "light.hall" || env.Plan.Confirm != "natural" {
					t.Fatalf("members %+v plan %+v", env.Members, env.Plan)
				}
			}},
		{name: "device-form unlock (domain + type) is typed",
			setup: func(f *fakeHA) {
				f.ent("lock.side", "zwave_js", "locked", map[string]any{"_di": "dev_lock"})
				f.script("leave", map[string]any{"device_id": "dev_lock", "domain": "lock", "type": "unlock", "entity_id": "0f3a9c"})
			},
			args: []string{"script.leave"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock, reasonStoredMembers}},
		{name: "shorthand scene member that unlocks is typed",
			setup: func(f *fakeHA) {
				f.script("leave", map[string]any{"scene": "scene.open_house"})
				f.ent("scene.open_house", "homeassistant", "unknown", nil)
				f.unique["scene.open_house"] = "oh1"
				f.configs["/api/config/scene/config/oh1"] = map[string]any{"entities": map[string]any{"lock.front": "unlocked", "light.hall": "on"}}
			},
			args: []string{"script.turn_on", "script.leave"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "use_blueprint expands through blueprint/substitute",
			setup: func(f *fakeHA) {
				f.automation("bp", "a1", map[string]any{"use_blueprint": map[string]any{"path": "door.yaml", "input": map[string]any{"door": "lock.front"}}})
				f.substitute = map[string]any{"substituted_config": map[string]any{"actions": []any{step("lock.unlock", "lock.front")}}}
			},
			args: []string{"automation.trigger", "automation.bp"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "failed blueprint substitution is unresolved: typed",
			setup: func(f *fakeHA) {
				f.automation("bp", "a1", map[string]any{"use_blueprint": map[string]any{"path": "door.yaml"}})
				f.fail["blueprint/substitute"] = true
			},
			args: []string{"automation.trigger", "automation.bp"}, exit: 3, tier: "typed", reasons: []string{reasonBlueprint}},
		{name: "cycle between fully read scripts is resolved",
			setup: func(f *fakeHA) {
				f.script("a", step("script.turn_on", "script.b"), step("light.turn_on", "light.hall"))
				f.script("b", map[string]any{"action": "script.a"})
			},
			args: []string{"script.turn_on", "script.a"}, exit: 3, tier: "preview", absent: []string{reasonLimitCut, reasonUnreadConfig}},
		{name: "cycle through an unread script is typed",
			setup: func(f *fakeHA) {
				f.script("a", map[string]any{"action": "script.c"})
				f.ent("script.c", "script", "off", nil)
				f.unique["script.c"] = "c"
			},
			args: []string{"script.turn_on", "script.a"}, exit: 3, tier: "typed", reasons: []string{reasonUnreadConfig}},
		{name: "depth cut is typed (LIMIT_CUT)",
			setup: func(f *fakeHA) {
				for i := 0; i < 12; i++ {
					f.script(fmt.Sprintf("s%d", i), map[string]any{"action": fmt.Sprintf("script.s%d", i+1)})
				}
				f.script("s12")
			},
			args: []string{"script.turn_on", "script.s0"}, exit: 3, tier: "typed", reasons: []string{reasonLimitCut}},
		{name: "call_service listener surplus runs the full scan and finds the unlock",
			setup: listener(1), args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock},
			absent: []string{reasonListener},
			check: func(t *testing.T, env verbTestEnvelope) {
				if len(env.Consumers) != 1 || env.Consumers[0].Match != "event:call_service" || env.Consumers[0].Class != "typed" {
					t.Fatalf("consumers %+v", env.Consumers)
				}
			}},
		{name: "unexplained call_service surplus keeps the attributed unlock typed",
			setup: listener(2), args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock, reasonListenerUnexpl}},
		{name: "unexplained call_service listener (logbook stream) is preview",
			setup: func(f *fakeHA) { f.events = []map[string]any{{"event": "call_service", "listener_count": 1}} },
			args:  []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonListenerUnexpl}},
		{name: "no call_service row at all reads as 0 listeners",
			setup: func(f *fakeHA) { f.events = []map[string]any{{"event": "state_changed", "listener_count": 40}} },
			args:  []string{"light.turn_on", "light.kitchen"}, exit: 0, tier: "ordinary"},
		{name: "garage set_cover_position 10 opens it: typed",
			setup: func(f *fakeHA) { f.ent("cover.garage", "ratgdo", "closed", map[string]any{"device_class": "garage"}) },
			args:  []string{"cover.set_cover_position", "cover.garage", "--set", "position=10"}, exit: 3, tier: "typed", reasons: []string{reasonAccessCover}},
		{name: "closing a garage grants nothing: ordinary",
			setup: func(f *fakeHA) { f.ent("cover.garage", "ratgdo", "open", map[string]any{"device_class": "garage"}) },
			args:  []string{"cover.close_cover", "cover.garage"}, exit: 0, tier: "ordinary"},
		{name: "open-capable cover without device_class is typed (Q3)",
			setup: func(f *fakeHA) { f.ent("cover.thing", "shelly", "closed", nil) },
			args:  []string{"cover.open_cover", "cover.thing"}, exit: 3, tier: "typed", reasons: []string{reasonCoverClassUnknown}},
		{name: "integration scene on a lock-owning platform is typed",
			setup: func(f *fakeHA) {
				f.ent("scene.away", "smartthings", "unknown", nil)
				f.unique["scene.away"] = "st-1"
				f.full = []map[string]any{{"entity_id": "lock.st_door", "platform": "smartthings"}}
			},
			args: []string{"scene.turn_on", "scene.away"}, exit: 3, tier: "typed", reasons: []string{reasonSceneAccess}},
		{name: "lighting-hub scene is preview with the gap named",
			setup: func(f *fakeHA) {
				f.ent("scene.relax", "hue", "unknown", nil)
				f.unique["scene.relax"] = "hue-1"
				f.full = []map[string]any{{"entity_id": "light.kitchen", "platform": "hue"}}
			},
			args: []string{"scene.turn_on", "scene.relax"}, exit: 3, tier: "preview", reasons: []string{reasonSceneUnread},
			check: func(t *testing.T, env verbTestEnvelope) {
				if env.Coverage.Complete {
					t.Fatal("an unread scene reported complete coverage")
				}
			}},
		{name: "YAML automation without id is unresolved: typed",
			setup: func(f *fakeHA) { f.automation("yaml_only", "", nil) },
			args:  []string{"automation.trigger", "automation.yaml_only"}, exit: 3, tier: "typed", reasons: []string{reasonYAMLNoID}},
		{name: "consumer automation with only ordinary actions keeps the fast path (Q1)",
			setup: consumer("lock.lock"), args: []string{"input_boolean.turn_on", "input_boolean.guest"}, exit: 0, tier: "ordinary",
			check: func(t *testing.T, env verbTestEnvelope) {
				if len(env.Consumers) != 1 || env.Consumers[0] != (tierConsumer{"automation.guest_lights", "trigger", "ordinary"}) {
					t.Fatalf("consumers %+v", env.Consumers)
				}
			}},
		{name: "consumer automation that unlocks makes the helper write typed",
			setup: consumer("lock.unlock"), args: []string{"input_boolean.turn_on", "input_boolean.guest"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "Node-RED config entry present: preview (Q2)",
			setup: func(f *fakeHA) { f.entries = []map[string]any{{"domain": "nodered"}} },
			args:  []string{"light.turn_off", "light.kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonManager}},
		{name: "AppDaemon App present: preview (Q2)",
			setup: func(f *fakeHA) {
				f.full = []map[string]any{{"entity_id": "update.appdaemon_update", "platform": "hassio", "unique_id": "a0d7b954_appdaemon_version_latest"}}
			},
			args: []string{"light.turn_off", "light.kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonManager}},
		{name: "area expansion is preview with the frozen leaf list",
			setup: func(f *fakeHA) { f.related["area:kitchen"] = []string{"light.kitchen", "switch.kettle", "light.hall"} },
			args:  []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded},
			check: func(t *testing.T, env verbTestEnvelope) {
				if got := strings.Join(env.Action.EntityIDs, ","); got != "light.hall,light.kitchen" {
					t.Fatalf("frozen ids %s", got)
				}
			}},
		{name: "group target freezes its leaves",
			setup: func(f *fakeHA) {
				f.ent("light.all", "group", "on", map[string]any{"entity_id": []any{"light.kitchen", "light.hall"}})
			},
			args: []string{"light.turn_off", "light.all"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded},
			check: func(t *testing.T, env verbTestEnvelope) {
				if got := strings.Join(env.Action.EntityIDs, ","); got != "light.kitchen,light.hall" {
					t.Fatalf("frozen ids %s", got)
				}
			}},
		{name: "ambiguous --match exits 5 with candidates",
			setup: func(f *fakeHA) {
				f.ent("light.kitchen_spot", "hue", "on", map[string]any{"friendly_name": "Kitchen spot"})
			},
			args: []string{"light.turn_off", "--match", "kitchen"}, exit: 5,
			check: func(t *testing.T, env verbTestEnvelope) {
				if env.Error == nil || env.Error.Code != "AMBIGUOUS_TARGET" || len(env.Error.Candidates) != 2 || env.Plan != nil {
					t.Fatalf("env %+v", env)
				}
			}},
		{name: "unique --match resolves and stays ordinary",
			args: []string{"light.turn_off", "--match", "kitchen"}, exit: 0, tier: "ordinary",
			check: func(t *testing.T, env verbTestEnvelope) {
				if env.Resolved[0].EntityID != "light.kitchen" || env.Resolved[0].By != "match" {
					t.Fatalf("resolved %+v", env.Resolved)
				}
			}},
		{name: "owned service is refused naming the skill",
			args: []string{"mqtt.publish", "--set", "topic=x/set"}, exit: 4, tier: "refused", reasons: []string{reasonOwnedService},
			check: func(t *testing.T, env verbTestEnvelope) {
				if env.Reasons[0].Skill != "ha-nova:mqtt" || env.Plan != nil || env.Error.Code != "REFUSED" {
					t.Fatalf("env %+v", env)
				}
			}},
		{name: "unlock is typed and carries a plan-bound code",
			setup: func(f *fakeHA) { f.ent("lock.front_door", "zwave_js", "locked", nil) },
			args:  []string{"lock.unlock", "lock.front_door"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock},
			check: func(t *testing.T, env verbTestEnvelope) {
				if !regexp.MustCompile(`^confirm:unlock-front_door-[0-9a-f]{6}$`).MatchString(env.Plan.Code) || env.Plan.Code[len(env.Plan.Code)-6:] != env.Plan.ID[2:8] {
					t.Fatalf("plan %+v", env.Plan)
				}
			}},
		{name: "code-bearing lock hands off to the UI",
			setup: func(f *fakeHA) { f.ent("lock.keypad", "zwave_js", "locked", map[string]any{"code_format": "^\\d{4}$"}) },
			args:  []string{"lock.unlock", "lock.keypad"}, exit: 4, tier: "refused", reasons: []string{reasonCodeRequired}},
		{name: "lock.open without the open feature bit is refused",
			args: []string{"lock.open", "lock.front"}, exit: 4, tier: "refused", reasons: []string{reasonFeature}},
		{name: "floor selector in data is refused",
			args: []string{"light.turn_off", "--set", "floor_id=upstairs"}, exit: 4, tier: "refused", reasons: []string{reasonSelector}},
		{name: "failed relation scan is typed, never clean",
			setup: func(f *fakeHA) { f.fail["search/related"] = true },
			args:  []string{"light.turn_off", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "YAML entity without registry row never takes the fast path",
			setup: func(f *fakeHA) { f.ent("switch.yaml_plug", "", "off", nil) },
			args:  []string{"switch.turn_on", "switch.yaml_plug"}, exit: 3, tier: "preview", reasons: []string{reasonNoRegistryRow}},
		{name: "consumer acting on an unregistered entity leaves the fast path",
			setup: func(f *fakeHA) {
				consumer("lock.lock")(f)
				f.ent("switch.yaml_tpl", "", "off", nil)
				f.automation("guest_lights", "g1", map[string]any{
					"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.guest"}},
					"actions":  []any{step("switch.turn_on", "switch.yaml_tpl")},
				})
			},
			args: []string{"input_boolean.turn_on", "input_boolean.guest"}, exit: 3, tier: "preview", reasons: []string{reasonNoRegistryRow}},
		{name: "ESPHome button press is user-authored: typed",
			setup: func(f *fakeHA) { f.ent("button.gate", "esphome", "unknown", nil) },
			args:  []string{"button.press", "button.gate"}, exit: 3, tier: "typed", reasons: []string{reasonUserButton}},
		{name: "restart is disruptive: preview",
			args: []string{"homeassistant.restart"}, exit: 3, tier: "preview", reasons: []string{reasonDisruptive}},
	}
	runTierCases(t, cases)
}
