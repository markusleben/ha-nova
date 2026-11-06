package main

import (
	"slices"
	"strings"
	"testing"
)

// Issue #1009: an update weighs what it changes; a read-only response
// service pulls in nothing that reacts to its target.

func routineHA() *fakeHA {
	f := newFakeHA()
	f.ent("weather.home", "met", "sunny", nil)
	f.ent("cover.pool", "template", "open", nil)
	f.ent("switch.art", "hue", "off", nil)
	f.script("briefing", map[string]any{"action": "weather.get_forecasts", "target": map[string]any{"entity_id": "weather.home"},
		"data": map[string]any{"type": "daily"}, "response_variable": "forecast"})
	f.automation("pool_weather", "pw", map[string]any{"alias": "Pool weather",
		"triggers": []any{map[string]any{"trigger": "state", "entity_id": "weather.home"}},
		"actions":  []any{step("cover.close_cover", "cover.pool")}})
	f.related["entity:weather.home"] = []string{"automation.pool_weather"}
	f.storeConfig("/api/config/automation/config/r1", routine())
	return f
}

func routine(extra ...any) map[string]any {
	return map[string]any{"alias": "Routine", "mode": "single",
		"triggers": []any{map[string]any{"trigger": "time", "at": "07:00:00"}},
		"actions":  append([]any{step("lock.unlock", "lock.front"), step("script.turn_on", "script.briefing")}, extra...)}
}

func planRoutine(t *testing.T, f *fakeHA, draft map[string]any) verbTestEnvelope {
	t.Helper()
	_, env := newWriteHarness(t, f).run(runPlanCommand, "write", "--domain", "automation", "--id", "r1", "--file", draftFile(t, draft))
	return env
}

func consumerNames(env verbTestEnvelope) string {
	var out []string
	for _, c := range env.Consumers {
		out = append(out, c.Automation)
	}
	return strings.Join(out, ",")
}

func TestPlanWriteWeighsOnlyTheChangedSteps(t *testing.T) {
	env := planRoutine(t, routineHA(), routine(step("switch.turn_on", "switch.art")))
	if env.Tier != "preview" || reasonCodes(env) != reasonConfigWrite || consumerNames(env) != "" || !env.Coverage.Complete {
		t.Fatalf("an appended ordinary step: tier %q reasons %s consumers %s coverage %+v", env.Tier, reasonCodes(env), consumerNames(env), env.Coverage)
	}
	if len(env.Members) != 1 || env.Members[0].EntityID != "switch.art" || env.Members[0].Via != "draft/actions/2" {
		t.Fatalf("members %+v, want only the new step", env.Members)
	}
}

func TestPlanWriteStillWeighsWhatTheChangeReaches(t *testing.T) {
	gate := map[string]any{"condition": "state", "entity_id": "switch.art", "state": "on"}
	cases := []struct {
		name   string
		before map[string]any // live config (nil: routine())
		draft  map[string]any
		tier   string
		reason string
	}{
		{"a new templated step", nil, routine(map[string]any{"action": "{{ states('sensor.x') }}"}), "typed", reasonTemplatedAction},
		{"a new step that unlocks", nil, routine(step("lock.unlock", "lock.front")), "typed", reasonAccessLock},
		{"a changed trigger", nil, func() map[string]any {
			d := routine()
			d["triggers"] = []any{map[string]any{"trigger": "time", "at": "06:00:00"}}
			return d
		}(), "typed", reasonAccessLock},
		{"a changed mode", nil, func() map[string]any { d := routine(); d["mode"] = "parallel"; return d }(), "typed", reasonAccessLock},
		{"a changed step", nil, func() map[string]any {
			d := routine()
			d["actions"].([]any)[0] = map[string]any{"action": "lock.unlock", "target": map[string]any{"entity_id": "lock.front"}, "data": map[string]any{"code": "1"}}
			return d
		}(), "typed", reasonAccessLock},
		{"a removed gate before an unchanged step", map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{gate, step("lock.unlock", "lock.front")}},
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{step("lock.unlock", "lock.front")}}, "typed", reasonAccessLock},
		{"a new variable before an unchanged step", nil, func() map[string]any {
			d := routine()
			d["actions"] = append([]any{map[string]any{"variables": map[string]any{"x": 1}}}, d["actions"].([]any)...)
			return d
		}(), "typed", reasonAccessLock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := routineHA()
			if tc.before != nil {
				f.configs["/api/config/automation/config/r1"] = tc.before
			}
			env := planRoutine(t, f, tc.draft)
			if env.Tier != tc.tier || !slices.ContainsFunc(env.Reasons, func(r tierReason) bool { return r.Code == tc.reason }) {
				t.Fatalf("tier %q reasons %s, want %s with %s", env.Tier, reasonCodes(env), tc.tier, tc.reason)
			}
		})
	}
}

func TestPlanWriteAliasOnlyWeighsNoStep(t *testing.T) {
	d := routine()
	d["alias"] = "Morning"
	if env := planRoutine(t, routineHA(), d); env.Tier != "preview" || reasonCodes(env) != reasonConfigWrite || len(env.Members) != 0 {
		t.Fatalf("an alias change: tier %q reasons %s members %+v", env.Tier, reasonCodes(env), env.Members)
	}
}

func TestReadOnlyResponseServiceReachesNoConsumer(t *testing.T) {
	f := routineHA()
	delete(f.configs, "/api/config/automation/config/r1")
	_, env := newWriteHarness(t, f).run(runPlanCommand, "write", "--domain", "automation", "--create", "--file",
		draftFile(t, map[string]any{"alias": "Brief", "triggers": []any{}, "actions": []any{step("script.turn_on", "script.briefing")}}))
	if consumerNames(env) != "" || slices.ContainsFunc(env.Reasons, func(r tierReason) bool { return r.Code == reasonTemplateAction }) {
		t.Fatalf("a script that only reads the weather: consumers %s reasons %s", consumerNames(env), reasonCodes(env))
	}
	// The same entity under a service that changes state still scans.
	f.script("briefing", step("homeassistant.update_entity", "weather.home"))
	_, env = newWriteHarness(t, f).run(runPlanCommand, "write", "--domain", "automation", "--create", "--file",
		draftFile(t, map[string]any{"alias": "Brief two", "triggers": []any{}, "actions": []any{step("script.turn_on", "script.briefing")}}))
	if consumerNames(env) != "automation.pool_weather" {
		t.Fatalf("an update of the weather entity: consumers %s, want automation.pool_weather", consumerNames(env))
	}
}
