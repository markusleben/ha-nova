package main

import (
	"slices"
	"testing"
)

// A step target that names a literal top-level variable is that value; a
// name anything else can bind stays unresolved (MAR-127 PR2).
func TestLiteralVariablesResolveTargets(t *testing.T) {
	for _, c := range []struct {
		name             string
		variables        map[string]any
		extra            map[string]any
		target           string
		tier             string
		want             string // reason the plan must carry
		unwanted         string // reason it must not
		triggerVariables map[string]any
		call             []string
	}{
		{name: "a literal blind", variables: map[string]any{"blind": "cover.blind"}, target: "{{ blind }}", tier: "ordinary", unwanted: reasonUnresolvedTarget},
		{name: "a literal garage door", variables: map[string]any{"door": "cover.garage"}, target: "{{door}}", tier: "typed", want: reasonAccessCover, unwanted: reasonUnresolvedTarget},
		{name: "a templated variable", variables: map[string]any{"blind": "{{ 'cover.blind' }}"}, target: "{{ blind }}", tier: "typed", want: reasonUnresolvedTarget},
		{name: "a variables step rebinds it", variables: map[string]any{"blind": "cover.blind"}, extra: map[string]any{"variables": map[string]any{"blind": "cover.garage"}}, target: "{{ blind }}", tier: "typed", want: reasonUnresolvedTarget},
		{name: "a response_variable binds it", variables: map[string]any{"blind": "cover.blind"}, extra: map[string]any{"action": "script.pick", "response_variable": "blind"}, target: "{{ blind }}", tier: "typed", want: reasonUnresolvedTarget},
		{name: "trigger is never a literal", variables: map[string]any{"trigger": "cover.blind"}, target: "{{ trigger }}", tier: "typed", want: reasonUnresolvedTarget},
		{name: "a template around it", variables: map[string]any{"blind": "cover.blind"}, target: "{{ blind ~ '' }}", tier: "typed", want: reasonUnresolvedTarget},
		{name: "a trigger's own variables bind it", variables: map[string]any{"blind": "cover.blind"}, triggerVariables: map[string]any{"blind": "cover.garage"}, target: "{{ blind }}", tier: "typed", want: reasonUnresolvedTarget},
		{name: "automation.trigger can pass it", variables: map[string]any{"blind": "cover.blind"}, call: []string{"call", "automation.trigger", "automation.follow"}, target: "{{ blind }}", tier: "typed", want: reasonUnresolvedTarget},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeHA()
			f.ent("cover.blind", "zwave_js", "open", map[string]any{"device_class": "shade", "friendly_name": "Blind", "supported_features": 15.0})
			f.ent("cover.garage", "zwave_js", "closed", map[string]any{"device_class": "garage", "friendly_name": "Garage", "supported_features": 15.0})
			actions := []any{}
			if c.extra != nil {
				actions = append(actions, c.extra)
			}
			actions = append(actions, step("cover.open_cover", c.target))
			f.automation("follow", "follow", map[string]any{
				"variables": c.variables,
				"triggers":  []any{map[string]any{"trigger": "state", "entity_id": "light.kitchen", "variables": c.triggerVariables}},
				"actions":   actions,
			})
			f.related["entity:light.kitchen"] = []string{"automation.follow"}
			h := newWriteHarness(t, f)
			call := []string{"call", "light.turn_off", "light.kitchen"}
			if c.call != nil {
				call = c.call
			}
			_, env := h.run(runPlanCommand, call...)
			reasons := reasonList(env)
			if env.Tier != c.tier || c.want != "" && !slices.Contains(reasons, c.want) || c.unwanted != "" && slices.Contains(reasons, c.unwanted) {
				t.Errorf("tier %q (reasons %v); want %s with %q, without %q", env.Tier, reasons, c.tier, c.want, c.unwanted)
			}
		})
	}
}
