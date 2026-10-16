package main

import (
	"testing"
)

func TestStateTriggerFires(t *testing.T) {
	onToOff := stateDelta{"on", "off", true}
	cases := []struct {
		trigger map[string]any
		d       stateDelta
		want    string
	}{
		{map[string]any{"trigger": "state", "entity_id": "light.lr", "to": "on"}, onToOff, firesWont},
		{map[string]any{"trigger": "state", "entity_id": "light.lr", "to": "off"}, onToOff, firesWill},
		{map[string]any{"platform": "state", "entity_id": []any{"light.x", "light.lr"}, "to": []any{"off", "unavailable"}}, onToOff, firesWill},
		{map[string]any{"trigger": "state", "entity_id": "light.x, light.lr", "from": "off"}, onToOff, firesWont},
		{map[string]any{"trigger": "state", "entity_id": "light.lr", "not_to": "off"}, onToOff, firesWont},
		{map[string]any{"trigger": "state", "entity_id": "light.lr", "not_from": "off"}, onToOff, firesWill},
		{map[string]any{"trigger": "state", "entity_id": "light.lr", "to": nil}, stateDelta{"on", "on", true}, firesWont},
		{map[string]any{"trigger": "state", "entity_id": "light.lr", "to": "on"}, stateDelta{"on", "on", true}, firesWont},
		{map[string]any{"trigger": "state", "entity_id": "light.lr"}, stateDelta{"on", "on", true}, firesWill}, // attribute changes fire it
		{map[string]any{"trigger": "state", "entity_id": "light.lr"}, stateDelta{}, firesWill},
		{map[string]any{"trigger": "state", "entity_id": "light.lr", "to": "on"}, stateDelta{}, firesMay},
		{map[string]any{"trigger": "state", "entity_id": "light.lr", "to": true}, onToOff, firesMay},
		{map[string]any{"trigger": "state", "entity_id": "light.lr", "attribute": "brightness", "to": 3}, onToOff, firesMay},
		{map[string]any{"trigger": "state", "entity_id": "light.lr_2", "to": "on"}, onToOff, firesMay}, // substring match only
		{map[string]any{"trigger": "numeric_state", "entity_id": "light.lr", "above": 3}, onToOff, firesMay},
		{map[string]any{"trigger": "template", "value_template": "{{ is_state('light.lr', 'on') }}"}, onToOff, firesMay},
	}
	for i, tc := range cases {
		if got := stateTriggerFires(tc.trigger, "light.lr", tc.d); got != tc.want {
			t.Errorf("case %d %v: got %s, want %s", i, tc.trigger, got, tc.want)
		}
	}
}

func TestPlannedDeltaUncertainTurnOn(t *testing.T) {
	off := &entityState{State: "off"}
	for _, data := range []map[string]any{{"brightness": 0.0}, {"brightness_pct": "{{ x }}"}, {"brightness_step_pct": -100.0}} {
		if d := plannedDelta("light.lr", "light.turn_on", data, off); d.known {
			t.Errorf("%v: delta %+v is not certain", data, d)
		}
	}
	if d := plannedDelta("light.lr", "light.turn_on", map[string]any{"brightness": 60.0}, off); d != (stateDelta{"off", "on", true}) {
		t.Errorf("delta %+v", d)
	}
	if d := plannedDelta("lock.front", "lock.lock", nil, &entityState{State: "unlocked"}); d.known {
		t.Errorf("a lock has transitional states: %+v", d)
	}
}

// Consumer B: living-room light on → bed light on (the test bed's seed).
func followLight(trigger map[string]any, actions ...any) func(*fakeHA) {
	return func(f *fakeHA) {
		f.ent("light.lr", "hue", "on", nil)
		f.ent("light.bed", "hue", "off", nil)
		f.related["entity:light.lr"] = []string{"automation.b"}
		if trigger == nil {
			trigger = map[string]any{"trigger": "state", "entity_id": "light.lr", "to": "on"}
		}
		if len(actions) == 0 {
			actions = []any{step("light.turn_on", "light.bed")}
		}
		f.automation("b", "b1", map[string]any{"triggers": []any{trigger}, "actions": actions})
	}
}

func shownIDs(env verbTestEnvelope) (members, consumers []string) {
	for _, m := range env.Members {
		members = append(members, m.EntityID)
	}
	for _, c := range env.Consumers {
		consumers = append(consumers, c.Automation+":"+c.Match)
	}
	return members, consumers
}

func expectShown(members, consumers string) func(*testing.T, verbTestEnvelope) {
	return func(t *testing.T, env verbTestEnvelope) {
		t.Helper()
		m, c := shownIDs(env)
		if got := joinOrDash(m); got != members {
			t.Fatalf("members %s, want %s", got, members)
		}
		if got := joinOrDash(c); got != consumers {
			t.Fatalf("consumers %s, want %s", got, consumers)
		}
	}
}

func joinOrDash(list []string) string {
	if len(list) == 0 {
		return "-"
	}
	out := list[0]
	for _, s := range list[1:] {
		out += "," + s
	}
	return out
}

func TestConsumerFiresForPlannedChange(t *testing.T) {
	runTierCases(t, []tierCase{
		{name: "E4: turning the light off never fires an on-trigger", setup: followLight(nil),
			args: []string{"light.turn_off", "light.lr"}, exit: 0, tier: "ordinary", check: expectShown("-", "-")},
		{name: "turning it on fires it: will, with its member",
			setup: func(f *fakeHA) { followLight(nil)(f); f.ent("light.lr", "hue", "off", nil) },
			args:  []string{"light.turn_on", "light.lr"}, exit: 0, tier: "ordinary", check: expectShown("light.bed", "automation.b:will")},
		{name: "SC2: a script turning the light off lists no follow-on",
			setup: func(f *fakeHA) { followLight(nil)(f); f.script("leave", step("light.turn_off", "light.lr")) },
			args:  []string{"script.turn_on", "script.leave"}, exit: 3, tier: "preview", check: expectShown("light.lr", "-")},
		{name: "SN1: a scene keeping an on light on fires no on-trigger",
			setup: func(f *fakeHA) {
				followLight(nil)(f)
				f.ent("scene.movie", "homeassistant", "unknown", nil)
				f.unique["scene.movie"] = "m1"
				f.configs["/api/config/scene/config/m1"] = map[string]any{"entities": map[string]any{"light.lr": map[string]any{"state": "on", "brightness": 60.0}}}
			},
			args: []string{"scene.turn_on", "scene.movie"}, exit: 3, tier: "preview", check: expectShown("light.lr", "-")},
		{name: "a consumer that cannot fire still raises the tier and stays listed as may",
			setup: followLight(nil, step("lock.unlock", "lock.front")),
			args:  []string{"light.turn_off", "light.lr"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock},
			check: expectShown("lock.front", "automation.b:may")},
		{name: "a consumer reached only through a dropped one is dropped too",
			setup: func(f *fakeHA) {
				followLight(nil)(f)
				f.related["entity:light.bed"] = []string{"automation.z"}
				f.automation("z", "z1", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.bed"}},
					"actions": []any{step("light.turn_on", "light.hall")}})
			},
			args: []string{"light.turn_off", "light.lr"}, exit: 0, tier: "ordinary", check: expectShown("-", "-")},
		{name: "conditions make a matching trigger may",
			setup: func(f *fakeHA) {
				followLight(map[string]any{"trigger": "state", "entity_id": "light.lr"})(f)
				f.configs["/api/config/automation/config/b1"].(map[string]any)["conditions"] = []any{map[string]any{"condition": "sun", "after": "sunset"}}
			},
			args: []string{"light.turn_off", "light.lr"}, exit: 0, tier: "ordinary", check: expectShown("light.bed", "automation.b:may")},
		{name: "a numeric_state trigger may fire",
			setup: followLight(map[string]any{"trigger": "numeric_state", "entity_id": "light.lr", "attribute": "brightness", "below": 10}),
			args:  []string{"light.turn_off", "light.lr"}, exit: 0, tier: "ordinary", check: expectShown("light.bed", "automation.b:may")},
	})
}
