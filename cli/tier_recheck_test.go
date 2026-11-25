package main

import "testing"

// The census reads its states and configs early in the check (tier_warm.go),
// so a listener added after it, such as a script that starts and waits on
// call_service, must not leave the tier lower. The counts are read again,
// past the memo, before the plan is sealed: a higher count, or no answer, is
// a gap.
func TestListenersRecheckedBeforeSealing(t *testing.T) {
	light := []string{"light.turn_on", "light.kitchen"}
	logbook := []map[string]any{{"event": "call_service", "listener_count": 1}} // an open logbook: the census only notes it
	reads := 0
	later := func(answer any) func(*fakeHA) { // the census's read answers f.events, a later one answer (nil: f.events too)
		return func(f *fakeHA) {
			reads = 0
			f.events = logbook
			f.extra = func(_ string, p map[string]any) (any, bool) {
				if p["path"] != "/api/events" {
					return nil, false
				}
				reads++
				return answer, reads > 1 && answer != nil
			}
		}
	}
	readTwice := func(t *testing.T, env verbTestEnvelope) {
		t.Helper()
		hasNote(noteUnexplainedListener)(t, env)
		if reads != 2 {
			t.Errorf("/api/events read %d times, want 2 (the census, then before sealing)", reads)
		}
	}
	cases := []tierCase{
		{name: "unchanged counts keep the call ordinary", setup: later(nil),
			args: light, exit: 0, tier: "ordinary", absent: []string{reasonListener, reasonScanFailed}, check: readTwice},
		{name: "a call_service listener added after the census", setup: later(coreReply(200, []map[string]any{{"event": "call_service", "listener_count": 2}})),
			args: light, exit: 3, tier: "typed", reasons: []string{reasonListener}},
		{name: "a state_changed listener added after the census", setup: later(coreReply(200, append([]map[string]any{{"event": "state_changed", "listener_count": 1}}, logbook...))),
			args: light, exit: 3, tier: "typed", reasons: []string{reasonListener}},
		{name: "a listener gone after the census adds no reaction", setup: later(coreReply(200, []map[string]any{{"event": "homeassistant_stop", "listener_count": 1}})),
			args: light, exit: 0, tier: "ordinary", absent: []string{reasonListener, reasonScanFailed}, check: readTwice},
		{name: "no answer before sealing", setup: later(fakeRelayFailure{"UPSTREAM_ERROR", "boom"}),
			args: light, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "without a census nothing is read again", setup: func(f *fakeHA) {
			reads = 0
			f.extra = func(_ string, p map[string]any) (any, bool) {
				if p["path"] == "/api/events" {
					reads++
				}
				return nil, false
			}
		}, args: light, exit: 0, tier: "ordinary", check: func(t *testing.T, _ verbTestEnvelope) {
			if reads != 1 {
				t.Errorf("/api/events read %d times, want 1", reads)
			}
		}},
	}
	runTierCases(t, cases)
}
