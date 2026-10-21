package main

import (
	"regexp"
	"strings"
	"testing"
)

// A preview row a person reads names the entity, never its raw
// domain.object_id, wherever the home gives it a name no other entity has;
// a shared name keeps its id beside it, and the plan still binds the ids.
func TestPreviewRowsShowNamesNotRawIDs(t *testing.T) {
	f := newFakeHA()
	named := map[string]string{
		"switch.smart_plug": "Smart Plug", "switch.decorative_lights": "Decorative Lights",
		"sensor.heat_pump_temp": "Heat Pump Temperature", "binary_sensor.hall_motion": "Hall Motion",
	}
	for id, name := range named {
		f.ent(id, "mqtt", "off", map[string]any{"friendly_name": name})
	}
	f.ent("light.lamp_a", "mqtt", "off", map[string]any{"friendly_name": "Lamp"})
	f.ent("light.lamp_b", "mqtt", "off", map[string]any{"friendly_name": "lamp"}) // same name, other case
	before := map[string]any{"alias": "Heat turns on plug", "mode": "single",
		"triggers":   []any{map[string]any{"trigger": "numeric_state", "entity_id": "sensor.heat_pump_temp", "above": 25}},
		"conditions": []any{map[string]any{"condition": "state", "entity_id": "binary_sensor.hall_motion", "state": "off"}},
		"actions":    []any{step("switch.turn_on", "switch.smart_plug")}}
	f.storeConfig("/api/config/automation/config/g1", before)
	h := newWriteHarness(t, f)
	after := map[string]any{"alias": "Heat turns on plug", "mode": "restart",
		"triggers": []any{map[string]any{"trigger": "numeric_state", "entity_id": "sensor.heat_pump_temp", "above": 25},
			map[string]any{"trigger": "time", "at": "22:00:00"}},
		"conditions": []any{map[string]any{"condition": "state", "entity_id": "binary_sensor.hall_motion", "state": "on"},
			map[string]any{"condition": "time", "weekday": []any{"sat", "sun"}}},
		"actions": []any{step("switch.turn_on", "switch.decorative_lights"),
			map[string]any{"delay": map[string]any{"hours": 0, "minutes": 5, "seconds": 0}},
			map[string]any{"action": "light.turn_off", "target": map[string]any{"entity_id": []any{"light.lamp_a", "light.lamp_b"}}, "data": map[string]any{"transition": 2}}}}
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "g1", "--file", draftFile(t, after))
	if exit != 3 || len(env.Diff) == 0 {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	rows := strings.Join(env.Diff, "\n")
	for _, want := range []string{
		"| Mode | one run at a time | a new run restarts it |",
		"| Trigger 2 | — | at 22:00 |",
		"| Condition 1 (“Hall Motion” › state) | off | on |",
		"| Condition 2 | — | only on Saturday and Sunday |",
		"| “Smart Plug” | “Decorative Lights” |",
		"| Action 2 | — | wait 5 min |",
		"| Action 3 | — | turn off light.lamp_a (“Lamp”) and light.lamp_b (“lamp”) with transition…", // a shared name keeps its id, first
	} {
		if !strings.Contains(rows, want) {
			t.Errorf("rows lack %q:\n%s", want, rows)
		}
	}
	shown := env.Say + "\n" + rows
	for id := range named {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(id) + `\b`).MatchString(shown) {
			t.Errorf("a human-facing line shows the raw id %s although it has a name:\n%s", id, shown)
		}
	}
	if !strings.Contains(string(env.Config.Body), "switch.decorative_lights") {
		t.Fatalf("the plan must bind the exact ids: %s", env.Config.Body)
	}
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 || applied.Say != "Done: changed the automation “Heat turns on plug”. I checked that Home Assistant saved it." {
		t.Fatalf("apply: exit %d say %q", exit, applied.Say)
	}
}

// Outside a preview render nothing is renamed: the comparisons and
// `ha-nova diff` stay exact.
func TestComparisonsStayExact(t *testing.T) {
	before := map[string]any{"actions": []any{step("switch.turn_on", "switch.a")}, "enabled": true}
	after := map[string]any{"actions": []any{step("switch.turn_on", "switch.b")}, "enabled": false}
	exact := strings.Join(renderConfigChanges(before, after), "\n")
	if !strings.Contains(exact, "| switch.a | switch.b |") || !strings.Contains(exact, "| true | false |") {
		t.Fatalf("exact rows changed: %s", exact)
	}
	var named string
	withPreviewNames(&plainNames{entity: map[string]string{"switch.a": "“A”", "switch.b": "“B”"}}, func() {
		named = strings.Join(renderConfigChanges(before, after), "\n")
	})
	if !strings.Contains(named, "| “A” | “B” |") || !strings.Contains(named, "| on | off |") || previewNames != nil {
		t.Fatalf("preview rows: %s", named)
	}
}

// A step with a key the plain phrase doesn't cover keeps its exact form, and
// so does any value under a key that doesn't hold entity ids.
func TestPlainItemNeverDropsAKey(t *testing.T) {
	withPreviewNames(&plainNames{entity: map[string]string{"light.a": "“A”"}}, func() {
		for _, item := range []map[string]any{
			{"action": "light.turn_on", "target": map[string]any{"entity_id": "light.a"}, "continue_on_error": true},
			{"action": "light.turn_on", "target": map[string]any{"area_id": "kitchen"}},
			{"trigger": "state", "entity_id": "light.a", "to": nil},
			{"trigger": "state", "entity_id": "light.a", "id": "t1"},
			{"condition": "template", "value_template": "{{ true }}"},
			{"action": "{{ chosen }}", "target": map[string]any{"entity_id": "light.a"}},
		} {
			if text := plainItem(item); text != "" {
				t.Errorf("%v worded as %q", item, text)
			}
		}
		if text, ok := plainValue("message", "light.a"); ok {
			t.Errorf("a message that looks like an id was renamed: %q", text)
		}
		if text := plainItem(map[string]any{"condition": "state", "entity_id": "light.a", "state": true}); text != "" {
			t.Errorf("a boolean state read as on/off: %q", text)
		}
	})
	withPreviewNames(&plainNames{entity: map[string]string{"binary_sensor.front": "binary_sensor.front (“Door”)", "sensor.t": "“" + strings.Repeat("温", 40) + "”"}}, func() {
		rows := strings.Join(renderConfigChanges(
			map[string]any{"conditions": []any{map[string]any{"condition": "state", "entity_id": "binary_sensor.front", "state": "off"}, map[string]any{"condition": "numeric_state", "entity_id": "sensor.t", "above": 1, "below": 5}}},
			map[string]any{"conditions": []any{map[string]any{"condition": "state", "entity_id": "binary_sensor.front", "state": "on"}, map[string]any{"condition": "numeric_state", "entity_id": "sensor.t", "above": 2, "below": 6}}}), "\n")
		for _, want := range []string{"| Condition 1 (binary_sensor.front (“Door”) › state) | off | on |", "| Condition 2 (above) | 1 | 2 |", "| Condition 2 (below) | 5 | 6 |"} {
			if !strings.Contains(rows, want) {
				t.Errorf("labels lack %q:\n%s", want, rows)
			}
		}
	})
	shown := map[string]string{}
	uniqueNames(map[string]string{"a": "Kitchen", "b": "Office", "c": "office", "d": "Fr\u043ent Door", "e": "Lamp (light.x)"}, shown)
	if shown["a"] != "“Kitchen”" || shown["b"] != "b (“Office”)" || shown["c"] != "c (“office”)" || shown["d"] != "d (“Frоnt Door”)" || shown["e"] != "e (“Lamp (light.x)”)" {
		t.Fatalf("names: %v", shown)
	}
}

// Review findings: a partial parse never rounds a value, and a phrase never
// hides what tells two services apart.
func TestPlainValuesStayExact(t *testing.T) {
	withPreviewNames(&plainNames{entity: map[string]string{"alarm_control_panel.h": "“House Alarm”"}}, func() {
		for key, value := range map[string]string{"delay": "0:0:1.5", "at": "6:0:0.5", "after": "06:00:30"} {
			if text, ok := plainValue(key, value); ok {
				t.Errorf("%s %q became %q", key, value, text)
			}
		}
		if text, _ := plainValue("delay", "01:30:00"); text != "1 h 30 min" {
			t.Errorf("span: %q", text)
		}
		// Plain words never make two different values look the same.
		if rows := strings.Join(renderConfigChanges(map[string]any{"at": "22:00"}, map[string]any{"at": "22:00:00"}), ""); !strings.Contains(rows, "| 22:00 | 22:00:00 |") {
			t.Errorf("equal-looking times: %s", rows)
		}
		if rows := strings.Join(dashboardDiff(map[string]any{"entity": "alarm_control_panel.h"}, map[string]any{"entity": []any{"alarm_control_panel.h"}}), ""); !strings.Contains(rows, `| alarm_control_panel.h | ["alarm_control_panel.h"] |`) {
			t.Errorf("equal-looking dashboard values: %s", rows)
		}
		home := plainItem(map[string]any{"action": "alarm_control_panel.alarm_arm_home", "target": map[string]any{"entity_id": "alarm_control_panel.h"}})
		away := plainItem(map[string]any{"action": "alarm_control_panel.alarm_arm_away", "target": map[string]any{"entity_id": "alarm_control_panel.h"}})
		if home != "arm “House Alarm” in home mode" || away != "arm “House Alarm” in away mode" {
			t.Errorf("alarm modes: %q / %q", home, away)
		}
	})
	env := &verbEnvelope{Members: []tierMember{{EntityID: "media_player.tv", Action: "media_player.media_stop", Via: "script.x/sequence/0"}}}
	if got := sayOwnedEffect(env, tierReason{EntityID: "media_player.tv", Via: "script.x/sequence/0"}); got != "controls a speaker or screen" {
		t.Errorf("owned effect: %q", got)
	}
	env.Members[0].Action = "tts.clear_cache"
	if got := sayOwnedEffect(env, tierReason{EntityID: "media_player.tv", Via: "script.x/sequence/0"}); got != "clears saved speech audio" {
		t.Errorf("owned effect: %q", got)
	}
}
