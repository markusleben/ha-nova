package main

import (
	"encoding/json"
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
	f.ent("switch.art", "hue", "off", map[string]any{"_di": "dev_art"})
	f.script("art_on", step("switch.turn_on", "switch.art"))
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
	f := routineHA()
	read := map[string]any{"action": "todo.get_items", "target": map[string]any{"entity_id": "todo.list"}, "response_variable": "items"}
	f.configs["/api/config/automation/config/r1"] = map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{read}}
	env := planRoutine(t, f, map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{read, step("switch.turn_on", "switch.art")}})
	if env.Tier != "preview" || reasonCodes(env) != reasonConfigWrite || consumerNames(env) != "" || !env.Coverage.Complete {
		t.Fatalf("an appended ordinary step: tier %q reasons %s consumers %s coverage %+v", env.Tier, reasonCodes(env), consumerNames(env), env.Coverage)
	}
	if len(env.Members) != 1 || env.Members[0].EntityID != "switch.art" || env.Members[0].Via != "draft/actions/1" {
		t.Fatalf("members %+v, want only the new step", env.Members)
	}
	// Inserted in the middle: the new step and every step after it.
	brief := step("script.turn_on", "script.briefing")
	f.configs["/api/config/automation/config/r1"] = map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{read, brief}}
	env = planRoutine(t, f, map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{read, step("switch.turn_on", "switch.art"), brief}})
	var vias []string
	for _, m := range env.Members {
		if strings.HasPrefix(m.Via, "draft/") {
			vias = append(vias, m.EntityID+" "+m.Via)
		}
	}
	if got := strings.Join(vias, ","); got != "switch.art draft/actions/1,script.briefing draft/actions/2" {
		t.Fatalf("an insertion weighs %s, want the new step and the one after it", got)
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
		{"a new state write before an unchanged gate", map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{gate, step("lock.unlock", "lock.front")}},
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{step("switch.turn_on", "switch.art"), gate, step("lock.unlock", "lock.front")}}, "typed", reasonAccessLock},
		{"a removed undo after an unchanged step", map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{step("lock.unlock", "lock.front"), map[string]any{"delay": "00:00:30"}, step("lock.lock", "lock.front")}},
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{step("lock.unlock", "lock.front")}}, "typed", reasonAccessLock},
		{"an appended write a step before it reads on the next run",
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{map[string]any{"if": []any{gate}, "then": []any{step("lock.unlock", "lock.front")}}}},
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{map[string]any{"if": []any{gate}, "then": []any{step("lock.unlock", "lock.front")}}, step("switch.turn_on", "switch.art")}}, "typed", reasonAccessLock},
		{"an appended run that sets what a step before it reads",
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{map[string]any{"if": []any{gate}, "then": []any{step("lock.unlock", "lock.front")}}}},
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{map[string]any{"if": []any{gate}, "then": []any{step("lock.unlock", "lock.front")}}, step("script.turn_on", "script.art_on")}}, "typed", reasonAccessLock},
		{"a step before the insertion with a template that may read any state",
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{map[string]any{"if": []any{map[string]any{"condition": "template", "value_template": "{{ states.switch | selectattr('state', 'eq', 'on') | list | count > 0 }}"}}, "then": []any{step("lock.unlock", "lock.front")}}}},
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{map[string]any{"if": []any{map[string]any{"condition": "template", "value_template": "{{ states.switch | selectattr('state', 'eq', 'on') | list | count > 0 }}"}}, "then": []any{step("lock.unlock", "lock.front")}}, step("switch.turn_on", "switch.art")}}, "typed", reasonAccessLock},
		{"a new alias a template reads without a dot", map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{map[string]any{"if": []any{map[string]any{"condition": "template", "value_template": "{{ (this | attr('attributes')).friendly_name == 'Open' }}"}}, "then": []any{step("lock.unlock", "lock.front")}}}},
			map[string]any{"alias": "Open", "triggers": []any{}, "actions": []any{map[string]any{"if": []any{map[string]any{"condition": "template", "value_template": "{{ (this | attr('attributes')).friendly_name == 'Open' }}"}}, "then": []any{step("lock.unlock", "lock.front")}}}}, "typed", reasonAccessLock},
		{"a moved step", nil, func() map[string]any {
			d := routine()
			steps := d["actions"].([]any)
			d["actions"] = []any{steps[1], steps[0]}
			return d
		}(), "typed", reasonAccessLock},
		{"a removed shorthand gate", map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{map[string]any{"or": []any{gate}}, step("lock.unlock", "lock.front")}},
			map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{step("lock.unlock", "lock.front")}}, "typed", reasonAccessLock},
		{"a new alias a step reads", map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{map[string]any{"action": "lock.unlock", "target": map[string]any{"entity_id": "{{ this.attributes.friendly_name }}"}}}},
			map[string]any{"alias": "lock.front", "triggers": []any{}, "actions": []any{map[string]any{"action": "lock.unlock", "target": map[string]any{"entity_id": "{{ this.attributes.friendly_name }}"}}}}, "typed", reasonUnresolvedTarget},
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

// Home Assistant renders variables in order: a reordered map is a change.
func TestPlanWriteVariableOrderIsAChange(t *testing.T) {
	for name, order := range map[string][2]string{
		"automation variables": {
			`{"alias":"V","variables":{"a":"lock","b":"{{ a }}"},"triggers":[],"actions":[{"action":"lock.unlock","target":{"entity_id":"lock.front"}}]}`,
			`{"alias":"V","variables":{"b":"{{ a }}","a":"lock"},"triggers":[],"actions":[{"action":"lock.unlock","target":{"entity_id":"lock.front"}}]}`},
		"step variables": {
			`{"alias":"V","triggers":[],"actions":[{"variables":{"a":"lock","b":"{{ a }}"}},{"action":"lock.unlock","target":{"entity_id":"lock.front"}}]}`,
			`{"alias":"V","triggers":[],"actions":[{"variables":{"b":"{{ a }}","a":"lock"}},{"action":"lock.unlock","target":{"entity_id":"lock.front"}}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			f := routineHA()
			f.configs["/api/config/automation/config/r1"] = json.RawMessage(order[0])
			_, env := newWriteHarness(t, f).run(runPlanCommand, "write", "--domain", "automation", "--id", "r1", "--file", rawDraft(t, order[1]))
			if env.Tier != "typed" || !slices.ContainsFunc(env.Reasons, func(r tierReason) bool { return r.Code == reasonAccessLock }) {
				t.Fatalf("a reordered %s: tier %q reasons %s, want the unlock weighed", name, env.Tier, reasonCodes(env))
			}
		})
	}
}

func TestPlanWriteAppendedDeviceTargetWidensToWhatReadsIt(t *testing.T) {
	gated := map[string]any{"if": []any{map[string]any{"condition": "state", "entity_id": "switch.art", "state": "on"}}, "then": []any{step("lock.unlock", "lock.front")}}
	f := routineHA()
	f.configs["/api/config/automation/config/r1"] = map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{gated}}
	env := planRoutine(t, f, map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{gated,
		map[string]any{"action": "switch.turn_on", "target": map[string]any{"device_id": "dev_art"}}}})
	if env.Tier != "typed" || !slices.ContainsFunc(env.Reasons, func(r tierReason) bool { return r.Code == reasonAccessLock }) {
		t.Fatalf("an appended device target: tier %q reasons %s members %+v", env.Tier, reasonCodes(env), env.Members)
	}
}

func TestPlanWriteAppendedStepWidensToWhatMayReadIt(t *testing.T) {
	unlockIf := func(condition map[string]any) map[string]any {
		return map[string]any{"if": []any{condition}, "then": []any{step("lock.unlock", "lock.front")}}
	}
	for name, c := range map[string]struct{ live, appended map[string]any }{
		"an area condition": {unlockIf(map[string]any{"condition": "light.is_on", "target": map[string]any{"area_id": "studio"}}),
			step("switch.turn_on", "switch.art")},
		"a step with no entity": {unlockIf(map[string]any{"condition": "state", "entity_id": "switch.art", "state": "on"}),
			map[string]any{"action": "mqtt.publish", "data": map[string]any{"topic": "art/set", "payload": "ON"}}},
		"a templated payload target": {map[string]any{"action": "lock.unlock", "data_template": map[string]any{
			"entity_id": "{{ 'lock.front' if states.switch | selectattr('state','eq','on') | list else 'lock.none' }}"}},
			step("switch.turn_on", "switch.art")},
		"an entity on the same device": {unlockIf(map[string]any{"condition": "numeric_state", "entity_id": "Sensor.Art_Power", "above": 5}),
			step("switch.turn_on", "switch.art")},
	} {
		f := routineHA()
		f.ent("sensor.art_power", "hue", "0", map[string]any{"_di": "dev_art"})
		f.configs["/api/config/automation/config/r1"] = map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{c.live}}
		env := planRoutine(t, f, map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{c.live, c.appended}})
		if env.Tier != "typed" {
			t.Errorf("%s before an appended step: tier %q reasons %s", name, env.Tier, reasonCodes(env))
		}
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

// What an unchanged step runs or sets off reads on the next run too: an
// appended step that sets state weighs the whole draft (#1009).
func TestPlanWriteAppendedStepWidensToWhatARunReads(t *testing.T) {
	gated := map[string]any{"if": []any{map[string]any{"condition": "state", "entity_id": "input_boolean.allow_entry", "state": "on"}},
		"then": []any{step("lock.unlock", "lock.front")}}
	for name, run := range map[string]map[string]any{
		"script.turn_on":                     step("script.turn_on", "script.check_entry"),
		"the script's name":                  {"action": "script.check_entry"},
		"automation.trigger":                 step("automation.trigger", "automation.entry"),
		"a nested script":                    step("script.turn_on", "script.outer"),
		"a python script":                    {"action": "python_script.check_entry"},
		"a service_template":                 {"service_template": "script.check_entry"},
		"an automation named like a service": step("automation.trigger", "automation.turn_on"),
		"an event":                           {"event": "check_entry"},
		"every automation":                   step("automation.trigger", "all"),
		"a step something reacts to":         step("switch.turn_on", "switch.art"),
	} {
		f := routineHA()
		f.ent("input_boolean.allow_entry", "input_boolean", "off", nil)
		f.script("check_entry", gated)
		f.script("outer", step("script.turn_on", "script.check_entry"))
		f.automation("entry", "en", map[string]any{"alias": "Entry", "triggers": []any{map[string]any{"trigger": "event", "event_type": "check_entry"}}, "actions": []any{gated}})
		f.events = append(f.events, map[string]any{"event": "check_entry", "listener_count": 1})
		f.automation("art_react", "ar", map[string]any{"alias": "Art react", "triggers": []any{map[string]any{"trigger": "state", "entity_id": "switch.art"}}, "actions": []any{gated}})
		f.related["entity:switch.art"] = []string{"automation.art_react"}
		f.automation("turn_on", "on", map[string]any{"alias": "Turn on", "triggers": []any{}, "actions": []any{gated}})
		live := map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{run}}
		f.configs["/api/config/automation/config/r1"] = live
		env := planRoutine(t, f, map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{run, step("input_boolean.turn_on", "input_boolean.allow_entry")}})
		if env.Tier != "typed" {
			t.Errorf("%s before an appended step it reads: tier %q reasons %s", name, env.Tier, reasonCodes(env))
		}
	}
}
