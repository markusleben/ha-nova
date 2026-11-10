package main

import (
	"slices"
	"strings"
	"sync"
	"testing"
)

// helperFlowRequest: the only non-read requests the tier engine sends, a
// helper's options flow start and its abort (tier_helper_options.go).
func helperFlowRequest(endpoint string, payload map[string]any) bool {
	body, _ := payload["body"].(map[string]any)
	path, _ := payload["path"].(string)
	return endpoint == "core" && (payload["method"] == "POST" && path == optionsFlowPath && len(body) == 1 && body["handler"] != nil ||
		payload["method"] == "DELETE" && strings.HasPrefix(path, optionsFlowPath+"/"))
}

// thermoHome: callHome (a cover watchdog on call_service) plus a UI generic
// thermostat whose heater is a plain switch nothing listens to, and a
// listener on homeassistant.turn_on. flows records every options-flow
// request; form answers the start.
type thermoFlows struct {
	mu      sync.Mutex
	starts  []string
	aborts  []string
	form    func(entry string) any
	abort   any
	sources map[string]any
	scripts []string // search/related scripts of switch.heater
}

func thermoHome(t *testing.T) (*writeHarness, *fakeHA, *thermoFlows) {
	t.Helper()
	h, f := callHome(t, "generic_thermostat", "generic_thermostat.climate")
	f.ent("climate.thermo", "generic_thermostat", "heat", map[string]any{"friendly_name": "Thermo"})
	f.ent("switch.heater", "shelly", "off", map[string]any{"friendly_name": "Heater", "_di": "dh"})
	f.full = append(f.full, map[string]any{"entity_id": "climate.thermo", "platform": "generic_thermostat", "config_entry_id": "ETHERMO"},
		map[string]any{"entity_id": "switch.heater", "platform": "shelly", "config_entry_id": "ESHELLY", "device_id": "dh"})
	f.entries = append(f.entries, map[string]any{"entry_id": "ETHERMO", "domain": "generic_thermostat", "supports_options": true},
		map[string]any{"entry_id": "ESHELLY", "domain": "shelly", "supports_options": true})
	listen(f, "ha_on", map[string]any{"domain": "homeassistant", "service": "turn_on"}, step("light.turn_on", "light.hall"))
	flows := &thermoFlows{sources: map[string]any{"climate.thermo": map[string]any{"domain": "generic_thermostat"}, "switch.heater": map[string]any{"domain": "shelly"}}, abort: coreReply(200, map[string]any{"message": "Flow aborted"})}
	flows.form = func(entry string) any { return coreReply(200, thermoForm(entry, "heater", "switch.heater")) }
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		path, _ := p["path"].(string)
		switch {
		case endpoint == "ws" && p["type"] == "entity/source":
			return flows.sources, true
		case endpoint == "ws" && p["type"] == "search/related" && p["item_id"] == "switch.heater" && flows.scripts != nil:
			return map[string]any{"script": flows.scripts}, true
		case endpoint == "core" && p["method"] == "POST" && path == optionsFlowPath:
			entry := p["body"].(map[string]any)["handler"].(string)
			flows.mu.Lock()
			flows.starts = append(flows.starts, entry)
			flows.mu.Unlock()
			return flows.form(entry), true
		case endpoint == "core" && p["method"] == "DELETE" && strings.HasPrefix(path, optionsFlowPath+"/"):
			flows.mu.Lock()
			flows.aborts = append(flows.aborts, strings.TrimPrefix(path, optionsFlowPath+"/"))
			flows.mu.Unlock()
			return flows.abort, true
		}
		return nil, false
	}
	return h, f, flows
}

// thermoForm: the init form Home Assistant shows, the stored value suggested.
func thermoForm(entry, field string, stored any) map[string]any {
	return map[string]any{"type": "form", "flow_id": "f" + strings.ToLower(entry), "handler": entry, "step_id": "init", "data_schema": []any{
		map[string]any{"name": "ac_mode", "selector": map[string]any{"boolean": map[string]any{}}, "description": map[string]any{"suggested_value": false}},
		map[string]any{"name": field, "selector": map[string]any{"entity": map[string]any{"domain": []any{"fan", "switch"}}}, "description": map[string]any{"suggested_value": stored}},
	}}
}

// A UI thermostat whose heater is a plain switch bounds what it calls: the
// cover watchdog no longer counts, a homeassistant.turn_on listener does,
// and the flow it read is aborted.
func TestHelperOptionsBoundTheThermostat(t *testing.T) {
	for _, call := range [][]string{{"call", "light.turn_off", "light.kitchen"}, {"call", "climate.set_temperature", "climate.thermo", "--set", "temperature=21"}} {
		h, f, flows := thermoHome(t)
		// The thermostat sits on the heater's device: an automation that only
		// sets it is related to that device and doesn't react to the heater.
		f.automation("evening_heat", "evening_heat", map[string]any{"triggers": []any{map[string]any{"trigger": "time", "at": "18:00:00"}}, "actions": []any{step("climate.set_temperature", "climate.thermo")}})
		f.related["device:dh"] = []string{"automation.evening_heat"}
		_, env := h.run(runPlanCommand, call...)
		got := consumerList(env)
		if slices.Contains(got, "automation.watchdog") || !slices.Contains(got, "automation.ha_on") {
			t.Errorf("%v: tier %q (reasons %s), consumers %v; want ha_on, not the watchdog", call, env.Tier, reasonCodes(env), got)
		}
		if len(flows.starts) == 0 || len(flows.aborts) != len(flows.starts) || slices.ContainsFunc(flows.starts, func(e string) bool { return e != "ETHERMO" }) {
			t.Errorf("%v: flows started %v, aborted %v; want ETHERMO's, each aborted", call, flows.starts, flows.aborts)
		}
	}
}

// Anything the engine can't read, or can't trust, about a thermostat or
// its heater keeps every listener, as before; a started flow is aborted
// whatever came back.
func TestHelperOptionsFailClosed(t *testing.T) {
	for name, tc := range map[string]struct {
		setup  func(*fakeHA, *thermoFlows)
		starts bool // a flow is started (and must be aborted)
	}{
		"a YAML thermostat without a registry row": {func(f *fakeHA, fl *thermoFlows) {
			fl.sources["climate.yaml"] = map[string]any{"domain": "generic_thermostat"}
		}, false},
		"a thermostat row without an entry": {func(f *fakeHA, fl *thermoFlows) {
			f.full[0]["config_entry_id"] = nil
		}, false},
		"the entity sources can't be read": {func(f *fakeHA, fl *thermoFlows) { fl.sources = nil; f.fail["entity/source"] = true }, false},
		"alert loaded beside it": {func(f *fakeHA, fl *thermoFlows) {
			f.config["components"] = append(f.config["components"].([]string), "alert")
		}, false},
		"its entry has no options flow": {func(f *fakeHA, fl *thermoFlows) {
			f.entries[len(f.entries)-2]["supports_options"] = false
		}, false},
		"a custom integration took the domain": {func(f *fakeHA, fl *thermoFlows) {
			for _, m := range f.manifests {
				if m["domain"] == "generic_thermostat" {
					m["is_built_in"] = false
				}
			}
		}, false},
		"the start fails": {func(f *fakeHA, fl *thermoFlows) {
			fl.form = func(string) any { return fakeRelayFailure{"UPSTREAM_HTTP_TIMEOUT", "timed out"} }
		}, true},
		"the start answers 400": {func(f *fakeHA, fl *thermoFlows) {
			fl.form = func(e string) any { return coreReply(400, thermoForm(e, "heater", "switch.heater")) }
		}, true},
		"another step": {func(f *fakeHA, fl *thermoFlows) {
			fl.form = func(e string) any {
				m := thermoForm(e, "heater", "switch.heater")
				m["step_id"] = "presets"
				return coreReply(200, m)
			}
		}, true},
		"no suggested value": {func(f *fakeHA, fl *thermoFlows) {
			fl.form = func(e string) any { return coreReply(200, thermoForm(e, "heater", nil)) }
		}, true},
		"a registry uuid": {func(f *fakeHA, fl *thermoFlows) {
			fl.form = func(e string) any { return coreReply(200, thermoForm(e, "heater", "0123456789abcdef0123456789abcdef")) }
		}, true},
		"a templated value": {func(f *fakeHA, fl *thermoFlows) {
			fl.form = func(e string) any { return coreReply(200, thermoForm(e, "heater", "{{ 'switch.heater' }}")) }
		}, true},
		"the abort fails": {func(f *fakeHA, fl *thermoFlows) {
			fl.abort = coreReply(404, map[string]any{"message": "Invalid flow specified"})
		}, true},
		"a light as heater": {func(f *fakeHA, fl *thermoFlows) {
			fl.form = func(e string) any { return coreReply(200, thermoForm(e, "heater", "light.hall")) }
		}, true},
		"a group switch":    {func(f *fakeHA, fl *thermoFlows) { f.ent("switch.heater", "group", "off", nil) }, true},
		"a template switch": {func(f *fakeHA, fl *thermoFlows) { f.ent("switch.heater", "template", "off", nil) }, true},
		"an automation on it": {func(f *fakeHA, fl *thermoFlows) {
			f.automation("heater_follow", "heater_follow", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "switch.heater"}}, "actions": []any{step("cover.stop_cover", "cover.blind")}})
			f.related["entity:switch.heater"] = []string{"automation.heater_follow"}
		}, true},
		"a device trigger on it": {func(f *fakeHA, fl *thermoFlows) {
			f.automation("heater_device", "heater_device", map[string]any{"triggers": []any{map[string]any{"trigger": "device", "device_id": "dh", "domain": "switch", "type": "turned_on", "entity_id": "switch.heater"}}, "actions": []any{step("cover.stop_cover", "cover.blind")}})
			f.related["device:dh"] = []string{"automation.heater_device"}
		}, true},
		"an automation on its device that can't be read": {func(f *fakeHA, fl *thermoFlows) { f.related["device:dh"] = []string{"automation.gone"} }, true},
		"a device-less switch_as_x": {func(f *fakeHA, fl *thermoFlows) {
			f.ent("switch.heater", "shelly", "off", map[string]any{"friendly_name": "Heater"})
			f.ent("light.wrapped", "switch_as_x", "off", nil)
		}, true},
		"a script that waits on it": {func(f *fakeHA, fl *thermoFlows) { fl.scripts = []string{"script.heater_wait"} }, true},
		"an automation on state_changed events": {func(f *fakeHA, fl *thermoFlows) {
			f.automation("state_watch", "state_watch", map[string]any{
				"triggers": []any{map[string]any{"trigger": "event", "event_type": "state_changed", "event_data": map[string]any{"entity_id": "switch.heater"}}},
				"actions":  []any{step("cover.stop_cover", "cover.blind")},
			})
			f.events = append(f.events, map[string]any{"event": "state_changed", "listener_count": 1.0})
		}, false},
		"an automation on the thermostat's state": {func(f *fakeHA, fl *thermoFlows) {
			f.automation("thermo_follow", "thermo_follow", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "climate.thermo", "attribute": "hvac_action"}}, "actions": []any{step("cover.stop_cover", "cover.blind")}})
			f.related["entity:climate.thermo"] = []string{"automation.thermo_follow"}
		}, false},
		"a switch_as_x on its device": {func(f *fakeHA, fl *thermoFlows) {
			f.ent("light.heater", "switch_as_x", "off", map[string]any{"_di": "dh"})
		}, true},
		"one of two reads fails": {func(f *fakeHA, fl *thermoFlows) {
			f.ent("climate.second", "generic_thermostat", "heat", nil)
			f.full = append(f.full, map[string]any{"entity_id": "climate.second", "platform": "generic_thermostat", "config_entry_id": "ESECOND"})
			f.entries = append(f.entries, map[string]any{"entry_id": "ESECOND", "domain": "generic_thermostat", "supports_options": true})
			fl.sources["climate.second"] = map[string]any{"domain": "generic_thermostat"}
			fl.form = func(e string) any {
				if e == "ESECOND" {
					return coreReply(500, thermoForm(e, "heater", "switch.heater"))
				}
				return coreReply(200, thermoForm(e, "heater", "switch.heater"))
			}
		}, true},
	} {
		t.Run(name, func(t *testing.T) {
			h, f, flows := thermoHome(t)
			tc.setup(f, flows)
			_, env := h.run(runPlanCommand, "call", "light.turn_off", "light.kitchen")
			if !slices.Contains(consumerList(env), "automation.watchdog") {
				t.Errorf("tier %q (reasons %s), consumers %v; want every listener (unbounded calls)", env.Tier, reasonCodes(env), consumerList(env))
			}
			if started := len(flows.starts) > 0; started != tc.starts {
				t.Errorf("flows started %v; want started %v", flows.starts, tc.starts)
			}
			if name != "the start fails" && len(flows.aborts) != len(flows.starts) {
				t.Errorf("flows started %v, aborted %v; want each aborted", flows.starts, flows.aborts)
			}
		})
	}
}

// Once one heater leaves the calls unbounded, no further flow is started.
func TestHelperOptionsStopOnceUnbounded(t *testing.T) {
	h, f, flows := thermoHome(t)
	f.ent("climate.second", "generic_thermostat", "heat", nil)
	f.ent("switch.tpl", "template", "off", nil)
	f.full = append(f.full, map[string]any{"entity_id": "climate.second", "platform": "generic_thermostat", "config_entry_id": "ESECOND"})
	f.entries = append(f.entries, map[string]any{"entry_id": "ESECOND", "domain": "generic_thermostat", "supports_options": true})
	flows.sources["climate.second"] = map[string]any{"domain": "generic_thermostat"}
	flows.form = func(e string) any {
		if e == "ESECOND" {
			return coreReply(200, thermoForm(e, "heater", "switch.tpl"))
		}
		return coreReply(200, thermoForm(e, "heater", "switch.heater"))
	}
	_, env := h.run(runPlanCommand, "call", "light.turn_off", "light.kitchen")
	if !slices.Contains(consumerList(env), "automation.watchdog") || !slices.Equal(flows.starts, []string{"ESECOND"}) || len(flows.aborts) != 1 {
		t.Errorf("consumers %v, flows started %v, aborted %v; want every listener and only ESECOND's flow, aborted", consumerList(env), flows.starts, flows.aborts)
	}
}

// The flow is started only where a listener's filter decides on it, and
// never for an entry outside the allowlist, whoever asks.
func TestHelperOptionsReadOnlyWhereItDecides(t *testing.T) {
	h, f, flows := thermoHome(t)
	f.events = nil // no call_service listener
	h.run(runPlanCommand, "call", "light.turn_off", "light.kitchen")
	if len(flows.starts) != 0 {
		t.Errorf("no service-call listener: flows started %v", flows.starts)
	}

	h, _, flows = thermoHome(t)
	h.run(runPlanCommand, "call", "automation.reload") // a reload's calls are unknown already
	if len(flows.starts) != 0 {
		t.Errorf("a reload: flows started %v", flows.starts)
	}

	h, _, flows = thermoHome(t)
	var s *relaySession
	check, err := gateVerdict("core", []byte(`{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"ETHERMO"}}`), func() (*relaySession, error) {
		opened, err := newRelaySession(h.paths, verbOptions{})
		s = opened
		return opened, err
	})
	if s != nil {
		s.close()
	}
	if err != nil || len(flows.starts) != 0 {
		t.Errorf("the gate rating an options-flow start (%s): err %v, flows started %v", check.Verdict, err, flows.starts)
	}

	h, _, flows = thermoHome(t)
	s, err = newRelaySession(h.paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	for _, entry := range []string{"ESHELLY", "EMISSING", "../x"} { // a replayed read names whatever the plan file holds
		if _, err := s.read(helperOptionsEndpoint, map[string]any{"entry_id": entry}); err == nil {
			t.Errorf("entry %s: read answered", entry)
		}
	}
	if _, err := s.read(helperOptionsEndpoint, map[string]any{"entry_id": "ETHERMO", "show_advanced_options": true}); err == nil {
		t.Errorf("an extra key: read answered")
	}
	if len(flows.starts) != 0 {
		t.Errorf("flows started %v; want none", flows.starts)
	}
}
