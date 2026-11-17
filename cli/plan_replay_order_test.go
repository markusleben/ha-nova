package main

import (
	"fmt"
	"testing"
	"time"
)

// A cut plan's replay reads again everything its preview had an answer to,
// on a budget of its own (plan_replay.go). In a home with many automations
// most of those reads are the listener scan's configs, which never decide
// the plan; the walk's own reads go first, so slow scan reads can't starve
// them. The order moves only the budget: a read the walk needs that still
// gets no answer is drift.

const fillers = 120

// hallHome: one automation reacts to light.hall (its consumer, as
// search/related names it), a service call listener makes the listener scan
// read every automation, and fillers automations nothing relates make that
// scan long.
func hallHome() *fakeHA {
	f := newFakeHA()
	f.ent("light.hall", "hue", "on", nil)
	f.ent("light.kitchen", "hue", "off", nil)
	f.automation("hall_watch", "hw", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.hall"}}, "actions": []any{step("light.turn_on", "light.kitchen")}})
	f.related["entity:light.hall"] = []string{"automation.hall_watch"}
	f.events = []map[string]any{{"event": "call_service", "listener_count": 1.0}}
	for i := range fillers {
		f.automation(fmt.Sprintf("filler_%03d", i), fmt.Sprintf("f%03d", i), map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": "sensor.elsewhere"}}, "actions": []any{}})
	}
	return f
}

// cutHallPlan plans turning the hall light off with the listener scan cut
// at one filler, then makes every filler config read slow: far more scan
// reads than a replay's budget covers.
func cutHallPlan(t *testing.T) (*slowHarness, verbTestEnvelope) {
	h := newSlowHarness(t, hallHome(), 0, 300*time.Millisecond)
	h.hangFirst("f000", 1)
	_, planned := h.run(runCallCommand, "light.turn_off", "light.hall")
	if planned.Plan == nil || planned.Plan.Code == "" || h.configReads("hw") != 1 {
		t.Fatalf("setup: %+v", planned)
	}
	h.mu.Lock()
	for i := range fillers {
		h.lag[fmt.Sprintf("automation/config automation.filler_%03d", i)] = 100 * time.Millisecond
	}
	h.mu.Unlock()
	return h, planned
}

func TestAReplayReadsWhatTheWalkNeedsBeforeTheListenerScan(t *testing.T) {
	h, planned := cutHallPlan(t)
	h.hangFirst("hw", 1) // apply's first pass misses the consumer; only the replay can read it
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if exit != 0 || len(h.posts()) != 1 || h.configReads("hw") != 3 {
		t.Fatalf("exit %d posts %d consumer reads %d say %q", exit, len(h.posts()), h.configReads("hw"), env.Say)
	}
}

func TestAReplayThatCannotReadAConsumerStillDrifts(t *testing.T) {
	h, planned := cutHallPlan(t)
	h.hangFirst("hw", 2) // apply's first pass and its replay
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if exit != verbExitDrift || len(h.posts()) != 0 {
		t.Fatalf("exit %d posts %d say %q", exit, len(h.posts()), env.Say)
	}
}
