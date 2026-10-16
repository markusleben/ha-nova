package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// What an area, device, or group target reached but left out is named in
// coverage.skipped; what is acted on never appears there, and a left-out
// entity never reaches the call.
func TestExpansionNamesWhatItLeavesOut(t *testing.T) {
	skipped := func(want string) func(*testing.T, verbTestEnvelope) {
		return func(t *testing.T, env verbTestEnvelope) {
			var got []string
			if env.Coverage != nil {
				for _, s := range env.Coverage.Skipped {
					got = append(got, strings.Join([]string{s.Code, s.EntityID, s.Via, s.Detail}, " "))
				}
			}
			if strings.Join(got, ";") != want {
				t.Fatalf("skipped %q, want %q", got, want)
			}
			if want != "" && !strings.Contains(strings.Join(env.Coverage.Notes, ","), strings.Fields(want)[0]) {
				t.Fatalf("notes %v lack %s", env.Coverage.Notes, strings.Fields(want)[0])
			}
		}
	}
	both := func(checks ...func(*testing.T, verbTestEnvelope)) func(*testing.T, verbTestEnvelope) {
		return func(t *testing.T, env verbTestEnvelope) {
			for _, c := range checks {
				c(t, env)
			}
		}
	}
	acted := func(want string) func(*testing.T, verbTestEnvelope) {
		return func(t *testing.T, env verbTestEnvelope) {
			if env.Action == nil || strings.Join(env.Action.EntityIDs, ",") != want {
				t.Fatalf("action %+v, want %s", env.Action, want)
			}
		}
	}
	hidden := func(f *fakeHA) {
		f.ent("light.raw", "shelly", "on", map[string]any{"_hb": true})
		f.related["area:kitchen"] = []string{"light.kitchen", "light.raw"}
	}
	cases := []tierCase{
		{name: "a hidden light is named, the visible one still runs like naming it",
			setup: hidden, args: []string{"light.turn_off", "--area", "kitchen"}, exit: 0, tier: "ordinary",
			check: both(acted("light.kitchen"), skipped("EXPANDED_TARGET_FILTERED light.raw area hidden"))},
		{name: "a categorized and an elsewhere-assigned entity are named with their cause",
			setup: func(f *fakeHA) {
				f.ent("switch.child_lock", "miele", "off", map[string]any{"_ec": 0})
				f.ent("switch.porch", "tplink", "off", map[string]any{"_ai": "garden"})
				f.ent("switch.kettle", "tplink", "off", nil)
				f.related["area:kitchen"] = []string{"switch.child_lock", "switch.kettle", "switch.porch"}
			},
			args: []string{"switch.turn_on", "--area", "kitchen"}, exit: 0, tier: "ordinary",
			check: both(acted("switch.kettle"), skipped("EXPANDED_TARGET_FILTERED switch.child_lock area entity_category;EXPANDED_TARGET_FILTERED switch.porch area other_area"))},
		{name: "a hidden switch on a device is named",
			setup: func(f *fakeHA) {
				f.ent("switch.out", "shelly", "on", map[string]any{"_di": "dev_2"})
				f.ent("switch.raw", "shelly", "on", map[string]any{"_di": "dev_2", "_hb": true})
			},
			args: []string{"switch.turn_off", "--device", "dev_2"}, exit: 0, tier: "ordinary",
			check: both(acted("switch.out"), skipped("EXPANDED_TARGET_FILTERED switch.raw device hidden"))},
		{name: "a hidden entity also named directly is acted on, not listed",
			setup: hidden, args: []string{"light.turn_off", "light.raw", "--area", "kitchen"}, exit: 3, tier: "preview",
			check: both(acted("light.raw,light.kitchen"), skipped(""))},
		{name: "a hidden entity that is a group's leaf is acted on, not listed",
			setup: func(f *fakeHA) {
				hidden(f)
				f.ent("light.zz_all", "group", "on", map[string]any{"entity_id": []any{"light.raw"}})
				f.related["area:kitchen"] = []string{"light.kitchen", "light.raw", "light.zz_all"}
			},
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview",
			check: both(acted("light.kitchen,light.raw"), skipped(""))},
		{name: "a leaf without state is named",
			setup: func(f *fakeHA) { f.related["area:kitchen"] = []string{"light.kitchen", "light.ghost"} },
			args:  []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview",
			check: both(acted("light.kitchen"), skipped("EXPANDED_TARGET_WITHOUT_STATE light.ghost area "))},
		{name: "a hidden gate cover under a generic area call is left out and named",
			setup: func(f *fakeHA) {
				f.ent("cover.gate", "ratgdo", "closed", map[string]any{"device_class": "gate", "_hb": true})
				f.ent("switch.lamp", "tplink", "off", nil)
				f.related["area:yard"] = []string{"cover.gate", "switch.lamp"}
			},
			args: []string{"homeassistant.turn_on", "--area", "yard"}, exit: 3, tier: "preview",
			check: both(acted("switch.lamp"), skipped("EXPANDED_TARGET_FILTERED cover.gate area hidden"))},
		{name: "an integration service keeps the hidden entity and lists nothing",
			setup: func(f *fakeHA) {
				f.ent("light.raw", "shelly", "on", map[string]any{"_hb": true})
				f.related["area:kitchen"] = []string{"light.raw"}
			},
			args: []string{"shelly.blink", "--area", "kitchen"}, exit: 3, tier: "preview",
			check: both(acted("light.raw"), skipped(""))},
	}
	runTierCases(t, cases)
}

// On `call`, the left-out entity never reaches Home Assistant.
func TestLeftOutEntityIsNeverSent(t *testing.T) {
	f := newFakeHA()
	f.ent("light.raw", "shelly", "on", map[string]any{"_hb": true})
	f.related["area:kitchen"] = []string{"light.kitchen", "light.raw"}
	h := newWriteHarness(t, f)
	exit, env := h.run(runCallCommand, "light.turn_off", "--area", "kitchen")
	posts := h.posts()
	if exit != 0 || env.Status != "verified" || len(posts) != 1 || env.Coverage == nil || len(env.Coverage.Skipped) != 1 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(posts))
	}
	if body, _ := json.Marshal(posts[0].payload["body"]); string(body) != `{"entity_id":["light.kitchen"]}` {
		t.Fatalf("payload %s", body)
	}
}
