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
	// The census's read answers f.events, every later one answer.
	later := func(answer any) func(*fakeHA) {
		return func(f *fakeHA) {
			f.events = logbook
			reads := 0
			f.extra = func(_ string, p map[string]any) (any, bool) {
				if p["path"] != "/api/events" {
					return nil, false
				}
				reads++
				return answer, reads > 1
			}
		}
	}
	cases := []tierCase{
		{name: "unchanged counts keep the call ordinary", setup: later(coreReply(200, logbook)),
			args: light, exit: 0, tier: "ordinary", absent: []string{reasonListener, reasonScanFailed}, check: hasNote(noteUnexplainedListener)},
		{name: "a call_service listener added after the census", setup: later(coreReply(200, []map[string]any{{"event": "call_service", "listener_count": 2}})),
			args: light, exit: 3, tier: "typed", reasons: []string{reasonListener}},
		{name: "a state_changed listener added after the census", setup: later(coreReply(200, append([]map[string]any{{"event": "state_changed", "listener_count": 1}}, logbook...))),
			args: light, exit: 3, tier: "typed", reasons: []string{reasonListener}},
		{name: "a listener gone after the census adds no reaction", setup: later(coreReply(200, []map[string]any{{"event": "homeassistant_stop", "listener_count": 1}})),
			args: light, exit: 0, tier: "ordinary", absent: []string{reasonListener, reasonScanFailed}},
		{name: "no answer before sealing", setup: later(fakeRelayFailure{"UPSTREAM_ERROR", "boom"}),
			args: light, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
	}
	runTierCases(t, cases)
}
