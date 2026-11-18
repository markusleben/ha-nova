package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func hasNote(code string) func(*testing.T, verbTestEnvelope) {
	return func(t *testing.T, env verbTestEnvelope) {
		t.Helper()
		if env.Coverage == nil || !slices.Contains(env.Coverage.Notes, code) {
			t.Fatalf("coverage %+v lacks the note %s", env.Coverage, code)
		}
	}
}

const mar209Watchdog = "automation.shutter_watchdog_dynamic_cover_groups"

// mar209Home: the home behind MAR-209 (cli/testdata/mar209, sanitized from
// the maintainer's read-only configs): a watchdog on call_service filtered to
// covers, whose script waits, notifies, logs, and moves covers it picks by
// template, beside a light, a shutter, and a garage door. logbookOpen adds
// one call_service listener no automation explains, as an open logbook does.
func mar209Home(t *testing.T, logbookOpen bool) func(*fakeHA) {
	fixture := func(name string) map[string]any {
		raw, err := os.ReadFile("testdata/mar209/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	automation, script := fixture("automation.json"), fixture("script.json")
	return func(f *fakeHA) {
		f.ent("cover.shutter_left", "zwave_js", "open", map[string]any{"device_class": "shutter", "supported_features": 15.0})
		f.ent("cover.garage_door", "zwave_js", "closed", map[string]any{"device_class": "garage", "supported_features": 15.0})
		f.ent("input_text.shutter_watchdog_token", "input_text", "", nil)
		f.ent("input_boolean.shutter_watchdog_retry_active", "input_boolean", "off", nil)
		f.config = map[string]any{"components": []string{"automation", "hue.light", "zwave_js.cover", "zwave_js.lock"}}
		f.manifests = []map[string]any{}
		for _, d := range []string{"hue", "zwave_js", "automation", "script", "input_text", "input_boolean", "persistent_notification", "logbook", "homeassistant"} {
			f.manifests = append(f.manifests, map[string]any{"domain": d, "is_built_in": true})
		}
		f.automation("shutter_watchdog_dynamic_cover_groups", "shutter_watchdog_cover_groups", automation)
		f.ent("script.shutter_watchdog_check_group", "script", "off", nil)
		f.unique["script.shutter_watchdog_check_group"] = "shutter_watchdog_check_group"
		f.configs["/api/config/script/config/shutter_watchdog_check_group"] = script
		listeners := 1.0
		if logbookOpen {
			listeners = 2
		}
		f.events = []map[string]any{{"event": "call_service", "listener_count": listeners}}
	}
}

func reachesWatchdog(want bool) func(*testing.T, verbTestEnvelope) {
	return func(t *testing.T, env verbTestEnvelope) {
		t.Helper()
		if slices.Contains(consumerList(env), mar209Watchdog) != want {
			t.Fatalf("consumers %v: watchdog reached = %v, want %v", consumerList(env), !want, want)
		}
	}
}

func both(checks ...func(*testing.T, verbTestEnvelope)) func(*testing.T, verbTestEnvelope) {
	return func(t *testing.T, env verbTestEnvelope) {
		t.Helper()
		for _, check := range checks {
			check(t, env)
		}
	}
}

// Decision A (docs/work/2026-11-18-0.33-spec.md §1.2): an unexplained listener
// on a logbook-listened event is a note, and each automation's own filter
// decides. Every other gap keeps today's fallback, in the CLI and the App's
// check alike (runTierCases checks gate parity).
func TestUnexplainedListenerIsANote(t *testing.T) {
	light := []string{"light.turn_on", "light.kitchen"}
	runTierCases(t, []tierCase{
		{name: "a light with a logbook open asks nothing", setup: mar209Home(t, true), args: light, exit: 0, tier: "ordinary",
			check: both(hasNote(noteUnexplainedListener), reachesWatchdog(false))},
		{name: "a light without a logbook asks nothing", setup: mar209Home(t, false), args: light, exit: 0, tier: "ordinary",
			check: func(t *testing.T, env verbTestEnvelope) {
				if env.Coverage != nil && slices.Contains(env.Coverage.Notes, noteUnexplainedListener) {
					t.Fatalf("notes %v: no listener is unexplained", env.Coverage.Notes)
				}
				reachesWatchdog(false)(t, env)
			}},
		{name: "a cover call still starts the watchdog, which could open the garage", setup: mar209Home(t, true),
			args: []string{"cover.close_cover", "cover.shutter_left"}, exit: 3, tier: "typed", reasons: []string{reasonUnresolvedTarget}, check: reachesWatchdog(true)},
		{name: "a light whose platform calls services keeps every listener", setup: func(f *fakeHA) {
			mar209Home(t, true)(f)
			f.ent("light.porch", "custom_lights", "off", nil)
		}, args: []string{"light.turn_on", "light.porch"}, exit: 3, tier: "typed", check: reachesWatchdog(true)},
		{name: "a loaded reactor keeps every listener", setup: func(f *fakeHA) {
			mar209Home(t, true)(f)
			f.config["components"] = append(f.config["components"].([]string), "alert")
		}, args: light, exit: 3, tier: "typed", check: reachesWatchdog(true)},
		{name: "a failed automation read keeps the census incomplete", setup: func(f *fakeHA) {
			mar209Home(t, true)(f)
			f.fail["automation/config "+mar209Watchdog] = true
		}, args: light, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "an automation Home Assistant hasn't loaded keeps the census incomplete", setup: func(f *fakeHA) {
			mar209Home(t, true)(f)
			f.loaded = nil
			delete(f.configs, "/api/config/automation/config/shutter_watchdog_cover_groups")
		}, args: light, exit: 3, tier: "typed", reasons: []string{reasonUnreadConfig}},
		{name: "a surplus on an event a logbook doesn't listen to stays unenumerable", setup: func(f *fakeHA) {
			mar209Home(t, true)(f)
			f.ent("todo.shopping_list", "shopping_list", "1", nil)
			f.services = append(f.services, map[string]any{"domain": "shopping_list", "services": map[string]any{"add_item": map[string]any{}}})
			f.events = append(f.events, map[string]any{"event": "shopping_list_updated", "listener_count": 1.0})
		}, args: []string{"shopping_list.add_item", "--set", "name=milk"}, exit: 3, tier: "typed", reasons: []string{reasonListener}},
		{name: "a running automation waiting on service calls keeps every listener", setup: waitsOn(t, "call_service", true), args: light, exit: 3, tier: "typed", reasons: []string{reasonListener}},
		{name: "an automation that isn't running holds no wait", setup: waitsOn(t, "call_service", false), args: light, exit: 0, tier: "ordinary", check: hasNote(noteUnexplainedListener)},
		{name: "a running wait on a templated event keeps every listener", setup: waitsOn(t, "{{ which }}", true), args: light, exit: 3, tier: "typed", reasons: []string{reasonListener}},
		{name: "a running wait on a state change is no service-call listener", setup: func(f *fakeHA) {
			mar209Home(t, true)(f)
			f.automation("motion", "m1", map[string]any{"actions": []any{map[string]any{"wait_for_trigger": map[string]any{"trigger": "state", "entity_id": "binary_sensor.hall"}}, step("light.turn_off", "light.hall")}})
			f.states["automation.motion"]["attributes"].(map[string]any)["current"] = 1.0
		}, args: light, exit: 0, tier: "ordinary", check: hasNote(noteUnexplainedListener)},
		{name: "a running script waiting on service calls keeps every listener", setup: func(f *fakeHA) {
			mar209Home(t, true)(f)
			f.script("wait_then_unlock", map[string]any{"wait_for_trigger": []any{map[string]any{"trigger": "event", "event_type": "call_service"}}}, step("lock.unlock", "lock.front"))
			f.states["script.wait_then_unlock"]["state"] = "on"
		}, args: light, exit: 3, tier: "typed", reasons: []string{reasonListener}},
	})
}

// waitsOn: the MAR-209 home with an open logbook, and an automation whose run
// waits for an event of eventType and then unlocks the front door.
func waitsOn(t *testing.T, eventType string, running bool) func(*fakeHA) {
	return func(f *fakeHA) {
		mar209Home(t, true)(f)
		f.automation("door", "d1", map[string]any{"actions": []any{
			map[string]any{"wait_for_trigger": []any{map[string]any{"trigger": "event", "event_type": eventType}}},
			step("lock.unlock", "lock.front"),
		}})
		if running {
			f.states["automation.door"]["attributes"].(map[string]any)["current"] = 1.0
		}
	}
}

// A census the read budget cuts keeps today's fallback, logbook or not.
func TestACutCensusBesideAnUnexplainedListenerStillAsks(t *testing.T) {
	f := newFakeHA()
	mar209Home(t, true)(f)
	h := newSlowHarness(t, f, 0, 300*time.Millisecond)
	h.hangFirst("shutter_watchdog_cover_groups", 100)
	env := h.plan("call", "light.turn_on", "light.kitchen")
	if env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonLimitCut) {
		t.Fatalf("tier %s reasons %s: want the cut census typed", env.Tier, reasonCodes(env))
	}
	assertGateParity(t, h.paths, env)
}
