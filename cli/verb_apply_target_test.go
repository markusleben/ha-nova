package main

import (
	"encoding/json"
	"testing"
)

// Codex P1 (#661): a targetless call on a service that takes a target acts
// on every entity of its domain; it is refused unless the request names
// targets. Only genuinely targetless services run without one.
func TestTargetlessEntityServicesAreRefused(t *testing.T) {
	listed := []map[string]any{
		{"domain": "light", "services": map[string]any{"turn_off": map[string]any{"target": map[string]any{"entity": []any{map[string]any{"domain": []any{"light"}}}}}}},
		{"domain": "homeassistant", "services": map[string]any{"check_config": map[string]any{}}},
	}
	for name, services := range map[string][]map[string]any{"service list read": listed, "service list unreadable": nil} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHA()
			f.services = services
			h := newWriteHarness(t, f)
			for _, args := range [][]string{{"light.turn_off"}, {"light.turn_off", "--set", "transition=2"}} {
				for verb, command := range map[string]func(runtimePaths, []string) int{"call": runCallCommand, "plan": runPlanCommand} {
					argv := args
					if verb == "plan" {
						argv = append([]string{"call"}, args...)
					}
					if exit, env := h.run(command, argv...); exit != 4 || env.Reasons[0].Code != reasonTargetRequired {
						t.Fatalf("%s %v: exit %d env %+v", verb, args, exit, env)
					}
				}
			}
			if name == "service list unreadable" {
				if exit, env := h.run(runCallCommand, "input_boolean.turn_on"); exit != 4 || env.Reasons[0].Code != reasonTargetRequired {
					t.Fatalf("fail-closed entity domain: exit %d env %+v", exit, env)
				}
			}
			exit, env := h.run(runCallCommand, "homeassistant.check_config")
			posts := h.posts()
			if exit != 0 || len(posts) != 1 || len(h.planFiles()) != 0 {
				t.Fatalf("genuinely targetless service: exit %d env %+v", exit, env)
			}
			if body, _ := json.Marshal(posts[0].payload["body"]); string(body) != "{}" {
				t.Fatalf("targetless body %s", body)
			}
		})
	}
}

// Sweep: an area or device that resolves to no leaf can never become a
// targetless call — not on call, not on apply.
func TestSelectorsResolvingToNothingNeverSendTargetless(t *testing.T) {
	f := newFakeHA()
	f.related["area:attic"] = []string{"light.ghost"} // no state: dropped with a note
	h := newWriteHarness(t, f)
	exit, env := h.run(runCallCommand, "light.turn_off", "--area", "attic")
	if exit != 3 || env.Tier != "typed" || len(env.Action.EntityIDs) != 0 || len(h.posts()) != 0 {
		t.Fatalf("call: exit %d env %+v", exit, env)
	}
	if exit, env := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 4 || env.Error.Code != "UNRESOLVED_TARGET" || len(h.posts()) != 0 {
		t.Fatalf("apply: exit %d env %+v", exit, env)
	}
	if exit, _ := h.run(runCallCommand, "light.turn_off", "--area", "empty"); exit != 5 || len(h.posts()) != 0 {
		t.Fatalf("empty area: exit %d", exit)
	}
}

// Codex P1 (#661): fields whose current value is the entity state enter the
// drift basis from st.State, so a change during confirmation drifts.
func TestStateBackedFieldsDrift(t *testing.T) {
	f := newFakeHA()
	f.ent("select.mode", "template_free", "eco", map[string]any{"options": []any{"eco", "comfort", "away"}})
	f.ent("input_number.target", "input_number", "20.0", nil)
	f.related["area:hall"] = []string{"select.mode"}
	h := newWriteHarness(t, f)
	planned := h.plan("select.select_option", "--area", "hall", "--set", "option=comfort") // preview
	if planned.Deltas[0].Before != "eco" {
		t.Fatalf("delta reads the state: %+v", planned.Deltas)
	}
	h.f.states["select.mode"]["state"] = "away"
	if exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID); exit != 2 || len(h.posts()) != 0 {
		t.Fatalf("select: exit %d env %+v", exit, env)
	}
	id := h.plan("input_number.set_value", "input_number.target", "--set", "value=21").Plan.ID
	h.f.states["input_number.target"]["state"] = "25.0"
	if exit, _ := h.run(runApplyCommand, "--plan", id); exit != 2 || len(h.posts()) != 0 {
		t.Fatalf("input_number: exit %d", exit)
	}
}
