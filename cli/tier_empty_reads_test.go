package main

import "testing"

// MAR-272: an answer that says "nothing there" where Home Assistant always
// has something is an unread answer, a gap, never a clean census: an empty
// event listener list, an empty state list, a loaded config of {}, and a
// state of null.
func TestEmptyAnswersAreUnread(t *testing.T) {
	listening := func(f *fakeHA) { // one live call_service listener: the census reads every automation
		f.events = []map[string]any{{"event": "call_service", "listener_count": 1}}
	}
	cases := []tierCase{
		{name: "an empty event listener list", setup: func(f *fakeHA) { f.events = []map[string]any{} },
			args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "an empty state list in the listener census", setup: func(f *fakeHA) {
			listening(f)
			f.extra = func(_ string, p map[string]any) (any, bool) {
				return coreReply(200, []any{}), p["path"] == "/api/states"
			}
		}, args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}, absent: []string{noteUnexplainedListener}},
		{name: "a loaded config of {} in the listener census", setup: func(f *fakeHA) {
			listening(f)
			f.automation("watch", "w1", map[string]any{
				"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service"}},
				"actions":  []any{step("lock.unlock", "lock.front")},
			})
			f.loaded = map[string]any{"automation.watch": map[string]any{}}
		}, args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "a consumer whose loaded config is {}", setup: func(f *fakeHA) {
			f.related["entity:light.kitchen"] = []string{"automation.follow"}
			f.automation("follow", "", map[string]any{})
			f.loaded = map[string]any{"automation.follow": map[string]any{}}
		}, args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "an event row without its event", setup: func(f *fakeHA) { f.events = []map[string]any{{"listener_count": 1}} },
			args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "a state list row without an entity id in the listener census", setup: func(f *fakeHA) {
			listening(f)
			f.extra = func(_ string, p map[string]any) (any, bool) {
				return coreReply(200, []any{map[string]any{}}), p["path"] == "/api/states"
			}
		}, args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}, absent: []string{noteUnexplainedListener}},
		{name: "--match over an empty state list proves no uniqueness", setup: func(f *fakeHA) {
			f.extra = func(_ string, p map[string]any) (any, bool) {
				return coreReply(200, []any{}), p["path"] == "/api/states"
			}
		}, args: []string{"light.turn_on", "--match", "Kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "an event row without listeners", setup: func(f *fakeHA) { f.events = []map[string]any{{"event": "call_service"}} },
			args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "a state of {}", setup: func(f *fakeHA) {
			f.extra = func(_ string, p map[string]any) (any, bool) {
				return coreReply(200, map[string]any{}), p["path"] == "/api/states/light.kitchen"
			}
		}, args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "a state of null", setup: func(f *fakeHA) {
			f.extra = func(_ string, p map[string]any) (any, bool) {
				return coreReply(200, nil), p["path"] == "/api/states/light.kitchen"
			}
		}, args: []string{"light.turn_on", "light.kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
	}
	runTierCases(t, cases)
}
