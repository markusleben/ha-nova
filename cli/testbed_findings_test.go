package main

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// Findings of the local test bed (scripts/e2e/testbed).

// 1: a button without a registry row (a YAML template button) runs whatever
// its author wired: typed, directly and as a stored member. A registered
// button keeps its tier.
func TestUnregisteredButtonsAreTyped(t *testing.T) {
	cases := []tierCase{
		{name: "YAML template button press is typed", setup: func(f *fakeHA) { f.ent("button.yaml_gate", "", "unknown", nil) },
			args: []string{"button.press", "button.yaml_gate"}, exit: 3, tier: "typed", reasons: []string{reasonUnregisteredActuator}},
		{name: "a stored press of it is typed", setup: func(f *fakeHA) {
			f.ent("button.yaml_gate", "", "unknown", nil)
			f.script("open", step("button.press", "button.yaml_gate"))
		}, args: []string{"script.turn_on", "script.open"}, exit: 3, tier: "typed", reasons: []string{reasonUnregisteredActuator}},
		{name: "a registered demo button stays ordinary", setup: func(f *fakeHA) { f.ent("button.demo_doorbell", "demo", "unknown", nil) },
			args: []string{"button.press", "button.demo_doorbell"}, exit: 0, tier: "ordinary"},
		{name: "any unregistered actuator is typed: a YAML switch", setup: func(f *fakeHA) { f.ent("switch.yaml_plug", "", "off", nil) },
			args: []string{"switch.turn_on", "switch.yaml_plug"}, exit: 3, tier: "typed", reasons: []string{reasonUnregisteredActuator}},
		{name: "and a YAML light", setup: func(f *fakeHA) { f.ent("light.yaml_lamp", "", "off", nil) },
			args: []string{"light.turn_on", "light.yaml_lamp"}, exit: 3, tier: "typed", reasons: []string{reasonUnregisteredActuator}},
		{name: "an unregistered template update run by a script is typed", setup: func(f *fakeHA) {
			f.ent("update.yaml_fw", "", "on", nil)
			f.script("upgrade", step("update.install", "update.yaml_fw"))
		}, args: []string{"script.turn_on", "script.upgrade"}, exit: 3, tier: "typed", reasons: []string{reasonUnregisteredActuator}},
		{name: "a YAML light group stays preview: its members are classified", setup: func(f *fakeHA) {
			f.ent("light.yaml_group", "", "on", map[string]any{"entity_id": []any{"light.kitchen", "light.hall"}})
			f.script("dark", step("light.turn_off", "light.yaml_group"))
		}, args: []string{"script.turn_on", "script.dark"}, exit: 3, tier: "preview", reasons: []string{reasonNoRegistryRow}, absent: []string{reasonUnregisteredActuator}},
		{name: "a non-actuator without registry row stays preview", setup: func(f *fakeHA) { f.ent("input_boolean.yaml_flag", "", "off", nil) },
			args: []string{"input_boolean.turn_on", "input_boolean.yaml_flag"}, exit: 3, tier: "preview", reasons: []string{reasonNoRegistryRow}},
	}
	runTierCases(t, cases)
}

// 2: every refusal's next step fits its code; a config refusal never says
// "plan call".
func TestRefusalNextSteps(t *testing.T) {
	f := newFakeHA()
	f.ent("automation.pkg", "automation", "on", map[string]any{"id": "pkg1"})
	f.full = append(f.full, map[string]any{"entity_id": "automation.pkg", "platform": "automation", "unique_id": "pkg1"})
	h := newWriteHarness(t, f)
	exit, env := h.run(runPlanCommand, "delete", "automation.pkg")
	if exit != 4 || env.Error.Code != "CONFIG_NOT_UI_MANAGED" || !strings.Contains(env.Error.Next, "ha-nova:yaml-config") || strings.Contains(env.Error.Next, "plan call") {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	for code, next := range refusalNext {
		if strings.Contains(next, "plan call …") {
			t.Fatalf("%s keeps the call-only copy: %q", code, next)
		}
	}
	if got := refused("PLAN_INVALID", "x").next; got != replan || !strings.Contains(replan, "get confirmation again") {
		t.Fatalf("plan-lifecycle refusal next %q", got)
	}
}

// 3: a registry-disabled entity named explicitly is reported as disabled,
// not as missing; a truly unknown id stays NOT_FOUND.
func TestDisabledTargetsAreNamedDisabled(t *testing.T) {
	f := newFakeHA()
	f.full = append(f.full, map[string]any{"entity_id": "light.old", "platform": "hue", "unique_id": "o1", "disabled_by": "user"})
	h := newWriteHarness(t, f)
	for _, run := range []struct {
		command func(runtimePaths, []string) int
		args    []string
	}{
		{runGetCommand, []string{"light.old"}},
		{runPlanCommand, []string{"call", "light.turn_on", "light.old"}},
		{runCallCommand, []string{"light.turn_on", "light.old"}},
	} {
		if exit, env := h.run(run.command, run.args...); exit != verbExitNotFound || env.Error.Code != "ENTITY_DISABLED" || !strings.Contains(env.Error.Next, "ha-nova:organize") {
			t.Fatalf("%v: exit %d env %+v", run.args, exit, env)
		}
	}
	if exit, env := h.run(runGetCommand, "light.never_was"); exit != verbExitNotFound || env.Error.Code != "NOT_FOUND" {
		t.Fatalf("unknown id: exit %d env %+v", exit, env)
	}
	if len(h.posts()) != 0 {
		t.Fatal("a disabled target was called")
	}
}

// F1: an id-less (or package) YAML automation is read through its loaded
// config instead of making every overlapping plan typed.
func TestLoadedConfigOfYAMLItems(t *testing.T) {
	yaml := func(action string) func(*fakeHA) {
		return func(f *fakeHA) {
			f.automation("night", "", nil) // enabled, no id
			f.loaded = map[string]any{"automation.night": map[string]any{"alias": "Night",
				"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service"}},
				"actions":  []any{step(action, "lock.front")}}}
		}
	}
	cases := []tierCase{
		{name: "trigger of an id-less automation reads its loaded actions", setup: yaml("lock.lock"),
			args: []string{"automation.trigger", "automation.night"}, exit: 3, tier: "preview", reasons: []string{reasonStoredMembers}, absent: []string{reasonYAMLNoID}},
		{name: "and classifies them", setup: yaml("lock.unlock"),
			args: []string{"automation.trigger", "automation.night"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}, absent: []string{reasonYAMLNoID}},
		{name: "the listener scan enumerates it instead of typing every call", setup: func(f *fakeHA) {
			yaml("lock.lock")(f)
			f.events = []map[string]any{{"event": "call_service", "listener_count": 1}}
		}, args: []string{"light.turn_on", "light.kitchen"}, exit: 0, tier: "ordinary"},
		{name: "a package automation with an id but no stored config uses it too", setup: func(f *fakeHA) {
			f.automation("pkg", "pkg1", nil)
			delete(f.configs, "/api/config/automation/config/pkg1")
			f.loaded = map[string]any{"automation.pkg": map[string]any{"alias": "Pkg", "actions": []any{step("light.turn_on", "light.hall")}}}
		}, args: []string{"automation.trigger", "automation.pkg"}, exit: 3, tier: "preview", absent: []string{reasonUnreadConfig}},
		{name: "unreadable loaded config stays YAML_NO_ID", setup: func(f *fakeHA) { f.automation("night", "", nil) },
			args: []string{"automation.trigger", "automation.night"}, exit: 3, tier: "typed", reasons: []string{reasonYAMLNoID}},
	}
	runTierCases(t, cases)

	f := newFakeHA()
	yaml("lock.lock")(f)
	h := newWriteHarness(t, f)
	id := h.plan("automation.trigger", "automation.night").Plan.ID
	h.f.loaded["automation.night"].(map[string]any)["actions"] = []any{step("light.toggle", "light.hall")}
	if exit, _ := h.run(runApplyCommand, "--plan", id); exit != 2 || len(h.posts()) != 0 {
		t.Fatalf("a changed loaded config did not drift: exit %d", exit)
	}
	exit, env := h.run(runGetCommand, "automation.night", "--config")
	if exit != 0 || !strings.Contains(string(env.Result), `"managed":"yaml"`) || !strings.Contains(string(env.Result), "ha-nova:yaml-config") || !strings.Contains(string(env.Result), "light.toggle") {
		t.Fatalf("get --config of an id-less automation: exit %d %s", exit, env.Result)
	}
}

// F4: find lists by state without a phrase; unregistered entities are
// included, a bare find without phrase or filter is a usage error.
func TestFindByState(t *testing.T) {
	f := newFakeHA() // light.kitchen on, light.hall off
	f.ent("light.yaml_spot", "", "on", map[string]any{"friendly_name": "Spot"})
	h := newWriteHarness(t, f)
	exit, env := h.run(runFindCommand, "--domain", "light", "--state", "on")
	if exit != 0 || !strings.Contains(string(env.Result), `"entity_id":"light.kitchen"`) || !strings.Contains(string(env.Result), `"entity_id":"light.yaml_spot"`) ||
		strings.Contains(string(env.Result), "light.hall") || !strings.Contains(string(env.Result), `"state":"on"`) {
		t.Fatalf("find --state: exit %d %s", exit, env.Result)
	}
	if exit, _ := h.run(runFindCommand); exit != 1 {
		t.Fatalf("a bare find: exit %d", exit)
	}
	if exit, env := h.run(runFindCommand, "kitchen", "--state", "off"); exit != 5 || !strings.Contains(env.Error.Message, `state "off"`) {
		t.Fatalf("no match in state: exit %d %+v", exit, env.Error)
	}
	c := startMCP(t, h.paths)
	c.request("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}})
	if res := c.tool("ha_find", map[string]any{"domain": "light", "state": "on"}); res["isError"] != false {
		t.Fatalf("ha_find by state: %v", res)
	}
}

// F6: a create's code is named after the alias, not the minted number;
// updates keep the config id; the hash+nonce suffix still binds it.
func TestCreateCodeUsesTheAlias(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	draft := hallDraft("light.turn_on")
	draft["actions"] = []any{step("lock.unlock", "lock.front")}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, draft))
	if !strings.HasPrefix(env.Plan.Code, "confirm:write-hall_motion-") {
		t.Fatalf("create code %q", env.Plan.Code)
	}
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 0 || applied.Status != "verified" {
		t.Fatalf("apply with the alias code: exit %d %+v", exit, applied)
	}
}

// F7: a late read error never overwrites what the device already answered;
// an unverified row says what it still is.
func TestVerificationKeepsTheLastGoodRead(t *testing.T) {
	fastVerify(t)
	f := newFakeHA() // light.kitchen on; the service below changes nothing
	var posted atomic.Bool
	var reads atomic.Int32
	f.service = func(*fakeHA, string, map[string]any) any { posted.Store(true); return coreReply(200, []any{}) }
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		// after the call: the first verification read answers, every later one fails
		if p["path"] == "/api/states/light.kitchen" && posted.Load() && reads.Add(1) > 1 {
			return fakeRelayFailure{"UPSTREAM_HTTP_TIMEOUT", "slow"}
		}
		return f.reply(r, endpoint, p)
	}, true)
	h := &writeHarness{t: t, f: f, paths: paths, log: log}
	exit, env := h.run(runCallCommand, "light.turn_off", "light.kitchen")
	row := env.Verification[0]
	if exit != 6 || row.Outcome != "unverified" || row.Observed != "on" || row.Note != "last read on; later reads failed" || reads.Load() < 2 {
		t.Fatalf("exit %d row %+v reads %d", exit, row, reads.Load())
	}
}

// F8: the exact call a real MCP client made — empty phrase, a domain, a
// big limit — lists instead of failing.
func TestMCPFindWithEmptyPhrase(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	c.request("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}})
	res := c.tool("ha_find", map[string]any{"domain": "light", "phrase": "", "limit": 200})
	if res["isError"] != false || res["structuredContent"].(map[string]any)["status"] != "ok" {
		t.Fatalf("ha_find with an empty phrase: %v", res)
	}
	for _, raw := range c.request("tools/list", map[string]any{})["result"].(map[string]any)["tools"].([]any) {
		if tool := raw.(map[string]any); tool["name"] == "ha_find" {
			schema := tool["inputSchema"].(map[string]any)
			if _, required := schema["required"]; required || schema["properties"].(map[string]any)["state"] == nil {
				t.Fatalf("ha_find schema %v", schema)
			}
		}
	}
}
