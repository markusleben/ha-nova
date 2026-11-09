package main

import (
	"slices"
	"testing"
)

// callHome: a home whose integrations are all built in, with a cover
// watchdog on call_service filtered to cover (MAR-127: its templated target
// can't be resolved), and Home Assistant's loaded components.
func callHome(t *testing.T, components ...string) (*writeHarness, *fakeHA) {
	t.Helper()
	f := newFakeHA()
	f.ent("cover.blind", "zwave_js", "open", map[string]any{"device_class": "shade", "friendly_name": "Blind", "supported_features": 15.0})
	f.ent("climate.hall", "zwave_js", "off", map[string]any{"friendly_name": "Hall"})
	f.config = map[string]any{"components": append([]string{"automation", "hue.light", "zwave_js.cover"}, components...)}
	f.manifests = []map[string]any{}
	for _, d := range []string{"hue", "zwave_js", "automation", "script", "homeassistant", "template", "switch_as_x", "group", "alert", "generic_thermostat"} {
		f.manifests = append(f.manifests, map[string]any{"domain": d, "is_built_in": true})
	}
	f.manifests = append(f.manifests, map[string]any{"domain": "custom_lights", "is_built_in": false})
	listen(f, "watchdog", map[string]any{"domain": "cover"}, map[string]any{"action": "cover.open_cover", "target": map[string]any{"entity_id": "{{ trigger.event.data.service_data.entity_id }}"}})
	return newWriteHarness(t, f), f
}

// listen adds an automation on call_service with event_data filter.
func listen(f *fakeHA, object string, filter map[string]any, action map[string]any) {
	f.automation(object, object, map[string]any{
		"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service", "event_data": filter}},
		"actions":  []any{action},
	})
	for i, e := range f.events {
		if e["event"] == "call_service" {
			f.events[i] = map[string]any{"event": "call_service", "listener_count": e["listener_count"].(float64) + 1}
			return
		}
	}
	f.events = append(f.events, map[string]any{"event": "call_service", "listener_count": 1.0})
}

func consumerList(env verbTestEnvelope) []string {
	var out []string
	for _, c := range env.Consumers {
		out = append(out, c.Automation)
	}
	return out
}

// A call_service listener counts only for calls the cascade may make; one
// the engine can't bound keeps every listener (MAR-127).
func TestCallServiceListenersByFilter(t *testing.T) {
	h, _ := callHome(t)
	if _, env := h.run(runPlanCommand, "call", "light.turn_off", "light.kitchen"); env.Tier != "ordinary" || slices.Contains(consumerList(env), "automation.watchdog") {
		t.Errorf("a light call beside a cover watchdog: tier %q (reasons %s), consumers %v", env.Tier, reasonCodes(env), consumerList(env))
	}
	if _, env := h.run(runPlanCommand, "call", "cover.close_cover", "cover.blind"); env.Tier != "typed" || !slices.Contains(consumerList(env), "automation.watchdog") || !slices.Contains(reasonList(env), reasonUnresolvedTarget) {
		t.Errorf("a cover call: tier %q (reasons %s), consumers %v; want typed by the watchdog", env.Tier, reasonCodes(env), consumerList(env))
	}

	for name, setup := range map[string]func(*fakeHA){
		"a listener's own call sets off the next": func(f *fakeHA) {
			listen(f, "chain", map[string]any{"domain": "light", "service": "turn_off"}, step("cover.stop_cover", "cover.blind"))
		},
		"a scene's climate member is set by attribute services": func(f *fakeHA) {
			listen(f, "heat", map[string]any{"domain": "climate", "service": "set_temperature"}, step("cover.stop_cover", "cover.blind"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			h, f := callHome(t)
			setup(f)
			f.ent("scene.evening", "homeassistant", "unknown", map[string]any{"id": "s1"})
			f.unique["scene.evening"] = "s1"
			f.configs["/api/config/scene/config/s1"] = map[string]any{"entities": map[string]any{"climate.hall": map[string]any{"state": "heat", "temperature": 21.0}}}
			call := []string{"call", "light.turn_off", "light.kitchen"}
			if name != "a listener's own call sets off the next" {
				call = []string{"call", "scene.turn_on", "scene.evening"}
			}
			if _, env := h.run(runPlanCommand, call...); !slices.Contains(consumerList(env), "automation.watchdog") {
				t.Errorf("tier %q (reasons %s), consumers %v; want the watchdog reached", env.Tier, reasonCodes(env), consumerList(env))
			}
		})
	}

	for name, setup := range map[string]func(*fakeHA) []string{
		"a switch_as_x light backed by a template switch": func(f *fakeHA) []string {
			f.ent("light.lamp", "switch_as_x", "off", map[string]any{"_di": "d1"})
			f.ent("switch.lamp", "template", "off", map[string]any{"_di": "d1"})
			return []string{"call", "light.turn_on", "light.lamp"}
		},
		"a light on a custom integration": func(f *fakeHA) []string {
			f.ent("light.strip", "custom_lights", "off", nil)
			return []string{"call", "light.turn_on", "light.strip"}
		},
		"a targetless call": func(f *fakeHA) []string {
			f.services = append(f.defaultServices(), map[string]any{"domain": "doorbell", "services": map[string]any{"announce": map[string]any{}}})
			return []string{"call", "doorbell.announce"}
		},
		"a generic thermostat in the home": func(f *fakeHA) []string {
			f.config["components"] = append(f.config["components"].([]string), "generic_thermostat.climate")
			return []string{"call", "light.turn_off", "light.kitchen"}
		},
		"the integrations can't be read": func(f *fakeHA) []string {
			f.fail["manifest/list"] = true
			return []string{"call", "light.turn_off", "light.kitchen"}
		},
		"the loaded components can't be read": func(f *fakeHA) []string {
			f.config = nil
			return []string{"call", "light.turn_off", "light.kitchen"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h, f := callHome(t)
			call := setup(f)
			if _, env := h.run(runPlanCommand, call...); !slices.Contains(consumerList(env), "automation.watchdog") {
				t.Errorf("tier %q (reasons %s), consumers %v; want every listener (unbounded calls)", env.Tier, reasonCodes(env), consumerList(env))
			}
		})
	}

	// alert calls notify when what it watches changes: a notify listener
	// counts for a light call, the cover watchdog still doesn't.
	h, f := callHome(t, "alert")
	listen(f, "notified", map[string]any{"domain": "notify"}, step("cover.stop_cover", "cover.blind"))
	if _, env := h.run(runPlanCommand, "call", "light.turn_off", "light.kitchen"); !slices.Contains(consumerList(env), "automation.notified") {
		t.Errorf("alert in the home: consumers %v, want the notify listener", consumerList(env))
	}
}

func reasonList(env verbTestEnvelope) []string {
	var out []string
	for _, r := range env.Reasons {
		out = append(out, r.Code)
	}
	return out
}
