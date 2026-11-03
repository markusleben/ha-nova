package main

import (
	"strings"
	"sync"
	"testing"
)

// calendarRelay is a raw relay whose calendar.family holds the weekly
// "Dentist" series e1 (the given rule; occurrences on 2, 9, and 16 November
// 2026 at 14:00, Berlin time, plus any extra rows of e1) and the single
// event e2; setRule changes the series rule as another client would, and ""
// makes the calendar unreadable.
func calendarRelay(t *testing.T, rule string, extra ...any) (paths runtimePaths, sent func() []string, setRule func(string)) {
	t.Helper()
	var mu sync.Mutex
	paths, sent = rawRelay(t, map[string]func(map[string]any) any{
		"GET /api/calendars/calendar.family": func(map[string]any) any {
			mu.Lock()
			defer mu.Unlock()
			if rule == "" {
				return map[string]any{"status": 500, "body": map[string]any{"message": "Error reading events"}}
			}
			events := []any{map[string]any{"uid": "e2", "summary": "Call", "start": map[string]any{"dateTime": "2026-11-03T09:00:00+01:00"}}}
			for _, day := range []string{"02", "09", "16"} {
				events = append(events, map[string]any{"uid": "e1", "summary": "Dentist", "recurrence_id": "202611" + day + "T140000", "rrule": rule,
					"start": map[string]any{"dateTime": "2026-11-" + day + "T14:00:00+01:00"}})
			}
			return map[string]any{"status": 200, "body": append(events, extra...)}
		},
	})
	return paths, sent, func(next string) {
		mu.Lock()
		defer mu.Unlock()
		rule = next
	}
}

const (
	calendarFuture = `"uid":"e1","recurrence_id":"20261109T140000","recurrence_range":"THISANDFUTURE"`
	calendarOne    = `"uid":"e1","recurrence_id":"20261109T140000","recurrence_range":""`
	calendarFirst  = `"uid":"e1","recurrence_id":"20261102T140000","recurrence_range":"THISANDFUTURE"`
	calendarAt     = "2026-11-09T14:00:00+01:00"
)

// calendarUpdate is one raw plan item: an update of target in
// calendar.family with the start, and the rule unless it is "".
func calendarUpdate(target, start, rule string) string {
	event := `"summary":"Dentist","dtstart":"` + start + `","dtend":"2026-11-09T15:00:00+01:00"`
	if rule != "" {
		event += `,"rrule":"` + rule + `"`
	}
	return `{"endpoint":"ws","payload":{"type":"calendar/event/update","entity_id":"calendar.family",` + target + `,"event":{` + event + `}}}`
}

// e1Row is an extra row of the series e1 on a November day.
func e1Row(day, rule string) any {
	row := map[string]any{"uid": "e1", "summary": "Dentist", "recurrence_id": "202611" + day + "T140000", "start": map[string]any{"dateTime": "2026-11-" + day + "T14:00:00+01:00"}}
	if rule != "" {
		row["rrule"] = rule
	}
	return row
}

// An update that can leave a repeating event with fewer dates needs the
// same code as a delete; one that keeps every date stays a preview.
func TestPlanRawCalendarUpdateThatDropsDatesIsTyped(t *testing.T) {
	for _, tc := range []struct {
		name, live, target, start, rule, tier string
		extra                                 []any
	}{
		{name: "text only for this and later ones", live: "FREQ=WEEKLY;COUNT=5", target: calendarFuture, start: calendarAt, tier: "preview"},
		{name: "text only from the series start", live: "FREQ=WEEKLY", target: calendarFirst, start: "2026-11-02T14:00:00+01:00", tier: "preview"},
		{name: "the same start in UTC", live: "FREQ=WEEKLY;COUNT=5", target: calendarFuture, start: "2026-11-09T13:00:00Z", tier: "preview"},
		{name: "the same start without an offset", live: "FREQ=WEEKLY;COUNT=5", target: calendarFuture, start: "2026-11-09T14:00:00", tier: "preview"},
		{name: "the same rule written otherwise", live: "FREQ=WEEKLY;COUNT=5", target: calendarFuture, start: calendarAt, rule: "count=5;freq=weekly", tier: "preview"},
		{name: "a higher COUNT", live: "FREQ=WEEKLY;COUNT=5", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY;COUNT=8", tier: "preview"},
		{name: "a later UNTIL", live: "FREQ=WEEKLY;UNTIL=20261231T230000Z", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY;UNTIL=20270131T230000Z", tier: "preview"},
		{name: "no end any more", live: "FREQ=WEEKLY;COUNT=5", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY", tier: "preview"},
		{name: "a lower COUNT", live: "FREQ=WEEKLY;COUNT=5", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY;COUNT=3", tier: "typed"},
		{name: "an earlier UNTIL", live: "FREQ=WEEKLY;UNTIL=20261231T230000Z", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY;UNTIL=20261201T230000Z", tier: "typed"},
		{name: "a new end", live: "FREQ=WEEKLY", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY;COUNT=10", tier: "typed"},
		{name: "fewer dates without an end", live: "FREQ=WEEKLY", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY;INTERVAL=2", tier: "typed"},
		{name: "an end it can't compare", live: "FREQ=WEEKLY;COUNT=5", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY;UNTIL=20271231T230000Z", tier: "typed"},
		{name: "a rule that is not one", live: "FREQ=WEEKLY", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY;", tier: "typed"},
		{name: "a later start for this and later ones", live: "FREQ=WEEKLY", target: calendarFuture, start: "2026-11-23T14:00:00+01:00", tier: "typed"},
		{name: "a later start for this one", live: "FREQ=WEEKLY;COUNT=5", target: calendarOne, start: "2026-11-10T14:00:00+01:00", tier: "preview"},
		{name: "another rule for this one", live: "FREQ=WEEKLY;COUNT=5", target: calendarOne, start: calendarAt, rule: "FREQ=WEEKLY;COUNT=3", tier: "typed"},
		{name: "text only from the start of a series with a changed date", live: "FREQ=WEEKLY", target: calendarFirst, start: "2026-11-02T14:00:00+01:00", extra: []any{e1Row("23", "")}, tier: "typed"},
		{name: "text only for this and later ones of a split series", live: "FREQ=WEEKLY;UNTIL=20261122T130000Z", target: calendarFuture, start: calendarAt, extra: []any{e1Row("23", "FREQ=WEEKLY"), e1Row("30", "FREQ=WEEKLY")}, tier: "typed"},
		{name: "text only for this one of a split series", live: "FREQ=WEEKLY;UNTIL=20261122T130000Z", target: calendarOne, start: calendarAt, extra: []any{e1Row("23", "FREQ=WEEKLY")}, tier: "preview"},
		{name: "text only for a changed date and later ones", live: "FREQ=WEEKLY", target: `"uid":"e1","recurrence_id":"20261123T140000","recurrence_range":"THISANDFUTURE"`, start: "2026-11-23T14:00:00+01:00", extra: []any{e1Row("23", "")}, tier: "typed"},
		{name: "the whole series", live: "FREQ=WEEKLY", target: `"uid":"e1"`, start: "2026-11-02T14:00:00+01:00", tier: "typed"},
		{name: "a single event", live: "FREQ=WEEKLY", target: `"uid":"e2"`, start: "2026-11-04T09:00:00+01:00", rule: "FREQ=DAILY;COUNT=2", tier: "preview"},
		{name: "a repeating event shown as one row", live: "FREQ=WEEKLY", target: `"uid":"e3"`, start: "2026-11-05T09:00:00+01:00", extra: []any{map[string]any{"uid": "e3", "rrule": "FREQ=WEEKLY", "start": map[string]any{"dateTime": "2026-11-05T09:00:00+01:00"}}}, tier: "typed"},
		{name: "an event the read doesn't hold", live: "FREQ=WEEKLY", target: `"uid":"e1","recurrence_id":"20261130T140000","recurrence_range":"THISANDFUTURE"`, start: calendarAt, tier: "typed"},
		{name: "an unreadable calendar", live: "", target: calendarFuture, start: calendarAt, rule: "FREQ=WEEKLY;COUNT=8", tier: "typed"},
		{name: "an unreadable calendar, text only for this one", live: "", target: calendarOne, start: calendarAt, tier: "preview"},
	} {
		paths, sent, _ := calendarRelay(t, tc.live, tc.extra...)
		exit, env := planRaw(t, paths, "["+calendarUpdate(tc.target, tc.start, tc.rule)+"]")
		if exit != verbExitConfirmation || env.Tier != tc.tier || len(sent()) != 0 {
			t.Errorf("%s: exit %d tier %s, want %s; sent %v", tc.name, exit, env.Tier, tc.tier, sent())
			continue
		}
		if says := strings.Contains(env.Say, "This deletes something in Home Assistant"); says != (tc.tier == "typed") {
			t.Errorf("%s: say %q", tc.name, env.Say)
		}
	}
}

// Each update is tiered on the event before the plan, so this and later
// occurrences of an event an earlier update in the plan changed need the
// code: from the first date of the part that update made, the local
// calendar replaces the whole event.
func TestPlanRawCalendarUpdateAfterAnotherOfTheSameEventIsTyped(t *testing.T) {
	split := calendarUpdate(calendarFuture, calendarAt, "")
	for _, tc := range []struct{ name, items, tier string }{
		{"this and later ones twice", split + "," + split, "typed"},
		{"a changed date, then from the series start", calendarUpdate(calendarOne, calendarAt, "") + "," + calendarUpdate(calendarFirst, "2026-11-02T14:00:00+01:00", ""), "typed"},
		{"two single dates", calendarUpdate(calendarOne, calendarAt, "") + "," + calendarUpdate(`"uid":"e1","recurrence_id":"20261116T140000","recurrence_range":""`, "2026-11-16T14:00:00+01:00", ""), "preview"},
		{"two events", split + "," + calendarUpdate(`"uid":"e2"`, "2026-11-03T09:00:00+01:00", ""), "preview"},
	} {
		paths, _, _ := calendarRelay(t, "FREQ=WEEKLY")
		if exit, env := planRaw(t, paths, "["+tc.items+"]"); exit != verbExitConfirmation || env.Tier != tc.tier {
			t.Errorf("%s: exit %d tier %s, want %s", tc.name, exit, env.Tier, tc.tier)
		}
	}
}

// The event an update is tiered on is its basis: a rule another client
// changed since the preview is drift, and nothing is sent.
func TestPlanRawCalendarUpdateDriftsWhenTheRuleChanged(t *testing.T) {
	paths, sent, setRule := calendarRelay(t, "FREQ=WEEKLY;COUNT=5")
	exit, env := planRaw(t, paths, "["+calendarUpdate(calendarFuture, calendarAt, "FREQ=WEEKLY;COUNT=5")+"]")
	if exit != verbExitConfirmation || env.Tier != "preview" {
		t.Fatalf("plan: exit %d tier %s %+v", exit, env.Tier, env.Error)
	}
	setRule("FREQ=WEEKLY;COUNT=8")
	exit, env = applyRaw(t, paths, env)
	if env.Error == nil || env.Error.Code != "PLAN_DRIFT" || len(sent()) != 0 {
		t.Fatalf("apply after the rule changed: exit %d %+v %v", exit, env.Error, sent())
	}
}
