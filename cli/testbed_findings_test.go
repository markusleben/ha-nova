package main

import (
	"strings"
	"testing"
)

// Findings of the local test bed (scripts/e2e/testbed).

// 1: a button without a registry row (a YAML template button) runs whatever
// its author wired: typed, directly and as a stored member. A registered
// button keeps its tier.
func TestUnregisteredButtonsAreTyped(t *testing.T) {
	cases := []tierCase{
		{name: "YAML template button press is typed", setup: func(f *fakeHA) { f.ent("button.yaml_gate", "", "unknown", nil) },
			args: []string{"button.press", "button.yaml_gate"}, exit: 3, tier: "typed", reasons: []string{reasonUnreadableButton}},
		{name: "a stored press of it is typed", setup: func(f *fakeHA) {
			f.ent("button.yaml_gate", "", "unknown", nil)
			f.script("open", step("button.press", "button.yaml_gate"))
		}, args: []string{"script.turn_on", "script.open"}, exit: 3, tier: "typed", reasons: []string{reasonUnreadableButton}},
		{name: "a registered demo button stays ordinary", setup: func(f *fakeHA) { f.ent("button.demo_doorbell", "demo", "unknown", nil) },
			args: []string{"button.press", "button.demo_doorbell"}, exit: 0, tier: "ordinary"},
		{name: "a YAML switch without registry row stays preview", setup: func(f *fakeHA) { f.ent("switch.yaml_plug", "", "off", nil) },
			args: []string{"switch.turn_on", "switch.yaml_plug"}, exit: 3, tier: "preview", reasons: []string{reasonNoRegistryRow}},
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
	if got := refused("PLAN_EXPIRED", "x").next; got != replan {
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
