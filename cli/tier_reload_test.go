package main

import (
	"testing"
)

// The reload after a config write is rated like the configs it loads
// (Markus, 2026-11-07): an automation's own as preview unless it reaches a
// typed action, every script's for script.reload; any other reload stays typed.
func TestConfigReloadIsRatedByItsConfig(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(f *fakeHA)
		args  []string
		tier  string
	}{
		{"a harmless automation", func(f *fakeHA) { f.automation("hall", "a1", hallDraft("light.turn_on")) }, []string{"automation.reload", "--set", "id=a1"}, "preview"},
		{"an automation that unlocks", func(f *fakeHA) {
			cfg := hallDraft("light.turn_on")
			cfg["actions"] = []any{step("lock.unlock", "lock.front")}
			f.automation("hall", "a1", cfg)
		}, []string{"automation.reload", "--set", "id=a1"}, "typed"},
		{"an automation with no stored config", func(*fakeHA) {}, []string{"automation.reload", "--set", "id=gone"}, "typed"},
		{"every automation", func(f *fakeHA) { f.automation("hall", "a1", hallDraft("light.turn_on")) }, []string{"automation.reload"}, "typed"},
		{"harmless scripts", func(f *fakeHA) { f.script("dim", step("light.turn_on", "light.hall")) }, []string{"script.reload"}, "preview"},
		{"a script that unlocks", func(f *fakeHA) {
			f.script("dim", step("light.turn_on", "light.hall")).script("leave", step("lock.unlock", "lock.front"))
		}, []string{"script.reload"}, "typed"},
		{"a YAML script, whose file reload reads unseen", func(f *fakeHA) {
			f.script("dim", step("light.turn_on", "light.hall")).ent("script.from_package", "script", "off", nil)
		}, []string{"script.reload"}, "typed"},
		{"a disabled script that unlocks", func(f *fakeHA) {
			f.script("dim", step("light.turn_on", "light.hall"))
			f.unique["script.hidden"] = "hidden"
			f.configs["/api/config/script/config/hidden"] = map[string]any{"sequence": []any{step("lock.unlock", "lock.front")}}
			f.full = append(f.full, map[string]any{"entity_id": "script.hidden", "platform": "script", "unique_id": "hidden", "disabled_by": "user"})
		}, []string{"script.reload"}, "typed"},
		{"an automation that runs on every automation reload and unlocks", func(f *fakeHA) {
			f.automation("hall", "a1", hallDraft("light.turn_on"))
			reloadUnlocks(f)
		}, []string{"automation.reload", "--set", "id=a1"}, "typed"},
		{"a script that reloads scripts", func(f *fakeHA) {
			f.script("again", map[string]any{"action": "script.reload"})
		}, []string{"script.reload"}, "typed"},
		{"an automation that reloads itself", func(f *fakeHA) {
			cfg := hallDraft("light.turn_on")
			cfg["actions"] = []any{map[string]any{"action": "automation.reload", "data": map[string]any{"id": "a1"}}}
			f.automation("hall", "a1", cfg)
		}, []string{"automation.reload", "--set", "id=a1"}, "typed"},
		{"a script whose state an unlocking automation watches", func(f *fakeHA) {
			f.script("dim", step("light.turn_on", "light.hall"))
			watches(f, "script.dim")
		}, []string{"script.reload"}, "typed"},
		{"an automation whose state an unlocking automation watches", func(f *fakeHA) {
			f.automation("hall", "a1", hallDraft("light.turn_on"))
			watches(f, "automation.hall")
		}, []string{"automation.reload", "--set", "id=a1"}, "typed"},
		{"another reload", func(*fakeHA) {}, []string{"template.reload"}, "typed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHA()
			tc.setup(f)
			withReloads(f)
			paths, _ := setupVerbRelayTest(t, f.reply)
			_, env := runVerbForTest(t, func() int { return runPlanCommand(paths, append([]string{"call"}, tc.args...)) })
			if env.Tier != tc.tier {
				t.Errorf("tier %q (reasons %s), want %s", env.Tier, reasonCodes(env), tc.tier)
			}
		})
	}
}

// At the gate, the reload after a harmless automation write passes; after
// one that unlocks, it waits for the person, as the write itself does.
func TestGateRatesTheReloadAfterAWrite(t *testing.T) {
	for _, unlocks := range []bool{false, true} {
		f := newFakeHA()
		cfg := hallDraft("light.turn_on")
		if unlocks {
			cfg["actions"] = []any{step("lock.unlock", "lock.front")}
		}
		f.automation("hall", "a1", cfg)
		withReloads(f)
		paths, _ := setupVerbRelayTest(t, f.reply)
		var s *relaySession
		check, err := gateVerdict("core", []byte(`{"method":"POST","path":"/api/services/automation/reload","body":{"id":"a1"}}`), func() (*relaySession, error) {
			opened, err := newRelaySession(paths, verbOptions{})
			s = opened
			return opened, err
		})
		if s != nil {
			s.close()
		}
		want := map[bool]gateDecision{false: gatePass, true: gateTyped}[unlocks]
		if err != nil || check.Verdict != want {
			t.Errorf("unlocks %v: %+v %v, want %s", unlocks, check, err, want)
		}
	}
}

// withReloads lists the automation, script, and template reloads too.
func withReloads(f *fakeHA) {
	f.services = f.defaultServices()
	for _, domain := range []string{"automation", "script", "template"} {
		f.services = append(f.services, map[string]any{"domain": domain, "services": map[string]any{"reload": map[string]any{}}})
	}
}

// reloadUnlocks: an automation that unlocks the front door on each
// automation_reloaded, which every automation reload and write fires.
func reloadUnlocks(f *fakeHA) {
	f.events = append(f.events, map[string]any{"event": "automation_reloaded", "listener_count": 1})
	f.automation("watch", "w1", map[string]any{
		"triggers": []any{map[string]any{"trigger": "event", "event_type": "automation_reloaded"}},
		"actions":  []any{step("lock.unlock", "lock.front")},
	})
}

// Home Assistant reloads an automation right after its config write; what
// that reload sets off is not rated, as in 0.28.3 (Markus, 2026-11-09: "Nicht
// mitbewerten, wie in 0.28.3"). A reload the user asks for still is.
func TestAutomationWriteDoesNotRateItsReloadListeners(t *testing.T) {
	for _, listens := range []bool{false, true} {
		f := newFakeHA()
		if listens {
			reloadUnlocks(f)
		}
		h := newWriteHarness(t, f)
		if _, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("light.turn_on"))); env.Tier != "preview" {
			t.Errorf("listens %v: tier %q (reasons %s), want preview", listens, env.Tier, reasonCodes(env))
		}
	}
}

// reloadHome is shaped like the home of 2026-11-09: a kitchen light that
// runs on every automation reload, a panel blueprint whose templated
// actions follow that light, one more reload listener nobody can attribute,
// and a cover watchdog on call_service.
func reloadHome(f *fakeHA) {
	f.ent("cover.garage", "zwave_js", "closed", map[string]any{"device_class": "garage", "friendly_name": "Garage", "supported_features": 15.0})
	f.events = append(f.events, map[string]any{"event": "automation_reloaded", "listener_count": 2.0}, map[string]any{"event": "call_service", "listener_count": 1.0})
	f.automation("kitchen", "k1", map[string]any{
		"triggers": []any{map[string]any{"trigger": "homeassistant", "event": "start"}, map[string]any{"trigger": "event", "event_type": "automation_reloaded"}},
		"actions":  []any{step("light.turn_off", "light.kitchen")},
	})
	f.related["entity:light.kitchen"] = []string{"automation.panel"}
	f.automation("panel", "p1", map[string]any{
		"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.kitchen"}},
		"actions":  []any{map[string]any{"action": "esphome.{{ panel_name }}_component_text", "data": map[string]any{"text": "{{ states('light.kitchen') }}"}}},
	})
	f.automation("watchdog", "w1", map[string]any{
		"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service", "event_data": map[string]any{"domain": "cover"}}},
		"actions":  []any{map[string]any{"action": "cover.open_cover", "target": map[string]any{"entity_id": "{{ trigger.event.data.service_data.entity_id }}"}}},
	})
}

// In that home a calm edit runs again, a reload call still asks, and an edit
// whose own actions open the garage or unlock still asks.
func TestReloadHomeEditsRunCalmly(t *testing.T) {
	h := newWriteHarness(t, calmHome())
	reloadHome(h.f)
	if env := h.planHall("light.turn_off"); env.Tier != "ordinary" || env.Plan == nil || env.Plan.Confirm != "none" {
		t.Errorf("edit: tier %q (reasons %s) %+v", env.Tier, reasonCodes(env), env.Plan)
	}
	for _, action := range []string{"lock.unlock", "cover.open_cover"} {
		draft := hallDraft("light.turn_off")
		target := map[string]string{"lock.unlock": "lock.front", "cover.open_cover": "cover.garage"}[action]
		draft["actions"] = []any{step(action, target)}
		if _, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, draft)); env.Tier != "typed" {
			t.Errorf("%s: tier %q (reasons %s), want typed", action, env.Tier, reasonCodes(env))
		}
	}
	withReloads(h.f)
	if _, env := h.run(runPlanCommand, "call", "automation.reload", "--set", "id=a1"); env.Tier != "typed" {
		t.Errorf("reload call: tier %q (reasons %s), want typed", env.Tier, reasonCodes(env))
	}
}

// watches: an automation that unlocks the front door when entityID's state
// changes, which a reload that recreates it sets off.
func watches(f *fakeHA, entityID string) {
	f.related["entity:"+entityID] = []string{"automation.watch"}
	f.automation("watch", "w1", map[string]any{
		"triggers": []any{map[string]any{"trigger": "state", "entity_id": entityID}},
		"actions":  []any{step("lock.unlock", "lock.front")},
	})
}
