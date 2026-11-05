package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// calendarShortens: a calendar/event/update that can leave a repeating
// event with fewer dates, typed like a delete. That is a rule other than the
// live one or a longer end of it (a lower COUNT, an earlier UNTIL, a new end,
// any other part changed), and for this and later occurrences also a new
// start, and any update of an event that holds other parts or changed dates,
// or that an earlier update in the plan changed: Home Assistant's local
// calendar recounts COUNT from the series start, ends the old part before
// the new start, drops a start its rule doesn't hold, and, from the first
// date of a part, replaces every part and changed date of the event; the
// read can't show where a part starts. An update that sends neither a rule
// nor a series start changes no dates. The event is read live
// (calendarLive); one that can't be read, an occurrence it shows no rule
// for, and a whole repeating event count as shortened.
func (n *rawNamer) calendarShortens(payload map[string]any) bool {
	entity, _ := payload["entity_id"].(string)
	uid, _ := payload["uid"].(string)
	event, _ := payload["event"].(map[string]any)
	rule, sendsRule := event["rrule"]
	start, sendsStart := event["dtstart"]
	rid, _ := payload["recurrence_id"].(string)
	series := rid == "" || payload["recurrence_range"] != nil && payload["recurrence_range"] != ""
	again := rid != "" && series && slices.Contains(n.calendarUpdates, entity+" "+uid)
	n.calendarUpdates = append(n.calendarUpdates, entity+" "+uid)
	if again || !sendsRule && !(series && sendsStart) {
		return again
	}
	live, ok := n.calendarLive(entity, uid, rid)
	sent, _ := rule.(string)
	switch {
	case !ok:
		return true
	case !series:
		return sendsRule && !(ruleKeepsDates(live.rule, sent) && ruleKeepsDates(sent, live.rule))
	case rid == "":
		return live.rule != ""
	case live.rule == "" || live.parts > 1:
		return true
	case sendsRule && !ruleKeepsDates(live.rule, sent):
		return true
	}
	return sendsStart && !sameStart(live.start, start)
}

// calendarLiveEvent: the start and rule of the event an update targets,
// and how many rules its event holds in the window ("" for a changed
// date).
type calendarLiveEvent struct {
	start, rule string
	parts       int
}

// calendarLive finds the occurrence (uid, recurrence_id) in the calendar's
// window (calendarWindow), or without one the uid's single event. Anything
// else, a repeating event named without an occurrence included, is not ok.
// What it found enters the basis (rawNamer.calendars), so an event changed
// since the preview is drift.
func (n *rawNamer) calendarLive(entity, uid, rid string) (calendarLiveEvent, bool) {
	live, ok := n.readCalendarLive(entity, uid, rid)
	if n.calendars == nil {
		n.calendars = map[string]any{}
	}
	key := fmt.Sprintf("%s %q %q", entity, uid, rid)
	n.calendars[key] = "unreadable"
	if ok {
		n.calendars[key] = map[string]any{"start": live.start, "rrule": live.rule, "parts": live.parts}
	}
	return live, ok
}

func (n *rawNamer) readCalendarLive(entity, uid, rid string) (calendarLiveEvent, bool) {
	if !entityIDPattern.MatchString(entity) || uid == "" {
		return calendarLiveEvent{}, false
	}
	var core struct {
		Status int
		Body   []struct {
			UID          string
			RecurrenceID string `json:"recurrence_id"`
			RRule        string
			Start        struct{ Date, DateTime string }
		}
	}
	data, err := n.read("core", calendarWindow(entity))
	if err != nil || json.Unmarshal(data, &core) != nil || core.Status != http.StatusOK {
		return calendarLiveEvent{}, false
	}
	var found []calendarLiveEvent
	rules := map[string]bool{}
	for _, event := range core.Body {
		if event.UID != uid {
			continue
		}
		rules[event.RRule] = true
		if event.RecurrenceID == rid {
			found = append(found, calendarLiveEvent{start: firstNonEmpty(event.Start.DateTime, event.Start.Date), rule: event.RRule})
		}
	}
	if len(found) != 1 || rid == "" && len(rules) != 1 {
		return calendarLiveEvent{}, false
	}
	found[0].parts = len(rules)
	return found[0], true
}

// calendarWindow: the read of a calendar a year around today, where a
// delete's event is named (calendarEvent) and an update's is found
// (calendarLive).
func calendarWindow(entity string) map[string]any {
	now := verbNow().UTC()
	return map[string]any{"method": http.MethodGet, "path": fmt.Sprintf("/api/calendars/%s?start=%s&end=%s", entity,
		now.AddDate(-1, 0, 0).Format("2006-01-02T15:04:05Z"), now.AddDate(1, 0, 0).Format("2006-01-02T15:04:05Z"))}
}

// ruleKeepsDates: the next rule holds every date of the old one: the same
// parts, with no end, a COUNT at least as high, or an UNTIL at least as
// late. Anything it can't compare doesn't.
func ruleKeepsDates(old, next string) bool {
	before, after := ruleParts(old), ruleParts(next)
	if before == nil || after == nil {
		return false
	}
	for _, parts := range []map[string]string{before, after} {
		for key := range parts {
			if key != "COUNT" && key != "UNTIL" && before[key] != after[key] {
				return false
			}
		}
	}
	switch {
	case after["COUNT"] == "" && after["UNTIL"] == "":
		return true
	case before["COUNT"] != "" && after["COUNT"] != "" && after["UNTIL"] == "":
		was, errWas := strconv.Atoi(before["COUNT"])
		is, errIs := strconv.Atoi(after["COUNT"])
		return errWas == nil && errIs == nil && is >= was
	case before["UNTIL"] != "" && after["UNTIL"] != "" && after["COUNT"] == "":
		for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102"} {
			was, errWas := time.Parse(layout, before["UNTIL"])
			is, errIs := time.Parse(layout, after["UNTIL"])
			if errWas == nil && errIs == nil {
				return !is.Before(was)
			}
		}
	}
	return false
}

// ruleParts: an RFC 5545 rule's parts by name, upper case; nil when it is
// not one.
func ruleParts(rule string) map[string]string {
	parts := map[string]string{}
	for _, part := range strings.Split(strings.ToUpper(rule), ";") {
		key, value, found := strings.Cut(part, "=")
		if _, twice := parts[key]; !found || key == "" || twice {
			return nil
		}
		parts[key] = value
	}
	return parts
}

// sameStart: the sent start is the live one, as written, as the same
// instant, or as its clock time without an offset, which Home Assistant
// reads in its own time zone, the one the calendar read answers in.
func sameStart(live string, sent any) bool {
	text, _ := sent.(string)
	was, err := time.Parse(time.RFC3339, live)
	if text == live || err != nil {
		return text == live
	}
	if is, err := time.Parse(time.RFC3339, text); err == nil {
		return was.Equal(is)
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if is, err := time.Parse(layout, text); err == nil {
			return was.Format(time.DateTime) == is.Format(time.DateTime)
		}
	}
	return false
}
