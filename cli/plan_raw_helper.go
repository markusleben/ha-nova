package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// An input_select or schedule update replaces the whole helper (Home
// Assistant's _update_data keeps only the item's id, and the schedule
// schema fills a left-out weekday with []): an option or time block the
// update leaves out is gone. It is checked like a scene save
// (plan_raw_scene.go): against the live helper at planning and at apply's
// recompute, the helper enters the plan's basis, and a removal needs a copy
// saved first (docs/work/2026-11-03-raw-omission-checks-spec.md).

// rawHelperLists: the helpers whose update drops list entries; noun names
// them, keep says how to send them all, removes what a needed copy keeps
// (copyNeeded), and dropped lists what body leaves out of live.
var rawHelperLists = map[string]struct {
	noun, keep, removes string
	dropped             func(live, body map[string]any) []helperDrop
}{
	"input_select": {"options", "every option as text", "options from a dropdown helper", func(live, body map[string]any) []helperDrop {
		kept := map[string]bool{}
		for _, option := range ensureList(body["options"]) {
			if text, ok := option.(string); ok { // the Relay re-encodes a number (1.0 becomes 1) before Home Assistant makes it text
				kept[text] = true
			}
		}
		var dropped []helperDrop
		for _, option := range ensureList(live["options"]) {
			if !kept[fmt.Sprint(option)] {
				dropped = append(dropped, helperDrop{option: fmt.Sprint(option)})
			}
		}
		return dropped
	}},
	"schedule": {"time blocks", "every time block with its data", "time blocks from a schedule", func(live, body map[string]any) []helperDrop {
		var dropped []helperDrop
		for _, day := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
			sent := map[[2]string]map[string]any{} // a block's start and end → the data the update gives it
			for _, block := range ensureList(body[day]) {
				from, to, data := scheduleBlock(block)
				sent[[2]string{from, to}] = data
			}
			for _, block := range ensureList(live[day]) {
				from, to, was := scheduleBlock(block)
				data, kept := sent[[2]string{from, to}]
				if !kept {
					dropped = append(dropped, helperDrop{day: day, from: from, to: to})
					continue
				}
				for key, value := range was { // a key added to its data drops nothing
					if now, has := data[key]; !has || !jsonEqual(value, now) {
						dropped = append(dropped, helperDrop{day: day, from: from, to: to, data: true})
						break
					}
				}
			}
		}
		return dropped
	}},
}

// helperDrop: an option an update removes, or a schedule time block (data:
// only its data goes).
type helperDrop struct {
	option, day, from, to string
	data                  bool
}

// String: the drop as a refusal names it.
func (d helperDrop) String() string {
	switch {
	case d.day == "":
		return jsonText(d.option)
	case d.data:
		return d.day + " " + d.from + "-" + d.to + " (only its data)"
	}
	return d.day + " " + d.from + "-" + d.to
}

// say: the drop as the preview names it, times as HH:MM.
func (d helperDrop) say(lang string) string {
	if d.day == "" {
		return sayText(d.option, true)
	}
	day, from, to := scheduleDay(lang, d.day), sayText(strings.TrimSuffix(d.from, ":00"), false), sayText(strings.TrimSuffix(d.to, ":00"), false)
	if d.data {
		return confirmT(lang, "raw.schedule_block_data", "day", day, "from", from, "to", to)
	}
	return confirmT(lang, "raw.schedule_block", "day", day, "from", from, "to", to)
}

// scheduleDay: a schedule's weekday key as the preview names it.
func scheduleDay(lang, day string) string {
	return map[string]string{
		"monday": confirmT(lang, "raw.day.monday"), "tuesday": confirmT(lang, "raw.day.tuesday"), "wednesday": confirmT(lang, "raw.day.wednesday"),
		"thursday": confirmT(lang, "raw.day.thursday"), "friday": confirmT(lang, "raw.day.friday"), "saturday": confirmT(lang, "raw.day.saturday"),
		"sunday": confirmT(lang, "raw.day.sunday"),
	}[day]
}

// helperNote: the sentence after an input_select or schedule update's line
// naming what it removes after its copy (helperKept); "" for any other.
func (n *rawNamer) helperNote(lang, endpoint string, payload map[string]any) string {
	collection, id := rawHelperUpdate(endpoint, payload)
	var names []string
	for _, drop := range n.helperDrops[collection+"/"+id] {
		names = append(names, drop.say(lang))
	}
	switch {
	case len(names) == 0:
		return ""
	case collection == "input_select":
		return confirmT(lang, "raw.options_drops", "options", sayList(lang, names))
	}
	return confirmT(lang, "raw.schedule_drops", "blocks", sayList(lang, names))
}

// kept: sceneKept or helperKept, whichever guards the request.
func (n *rawNamer) kept(earlier []rawRequest, endpoint string, payload map[string]any) (refusal, err error) {
	if endpoint == "core" {
		return n.sceneKept(earlier, payload)
	}
	return n.helperKept(earlier, endpoint, payload)
}

// helperKept: an input_select or schedule update that leaves out what the
// live helper holds needs a backups save of exactly its live item earlier in
// the plan (sceneKept). Keeps the live item for the basis, and what the
// update drops for its line (helperNote). A helper that can't be read, or
// isn't there, is an error: nothing is saved over unseen.
func (n *rawNamer) helperKept(earlier []rawRequest, endpoint string, payload map[string]any) (refusal, err error) {
	collection, id := rawHelperUpdate(endpoint, payload)
	list, guarded := rawHelperLists[collection]
	key := collection + "/" + id
	switch _, again := n.helpers[key]; {
	case !guarded:
		return nil, nil
	case again: // each update is checked against the live helper, not the one before it
		return rawDrop{message: "a plan updates the " + collection + " " + id + " once", hint: "Put every change to the helper into one update of the whole helper."}, nil
	}
	live, err := readHelper(n.s, collection, id)
	switch {
	case err != nil:
		return nil, fmt.Errorf("cannot read the %s %s, so nothing was planned or sent: %w", collection, id, err)
	case live == nil:
		return nil, helperNotFound(collection, id)
	}
	if n.helpers == nil {
		n.helpers, n.helperDrops = map[string]any{}, map[string][]helperDrop{}
	}
	n.helpers[key] = live
	drops := list.dropped(live, payload)
	if len(drops) == 0 {
		return nil, nil
	}
	var names []string
	for _, drop := range drops {
		names = append(names, drop.String())
	}
	dropped := strings.Join(names, ", ")
	removal := fmt.Sprintf("updating the %s %s would remove the %s %s from it", collection, id, list.noun, dropped)
	if hidden := hiddenRemoval(live, removal); hidden != nil {
		return hidden, nil
	}
	if copySaved(earlier, live) {
		n.helperDrops[key] = drops
		return nil, nil
	}
	return rawDrop{
		message: removal + ", with no copy of its current item saved first",
		hint: fmt.Sprintf(`This would remove the %s %s: an update replaces the whole helper. If the user asked for that (renaming, moving, or changing an entry removes the old one), tell them which ones go, `+
			`then plan it with a backups save of the helper's current item first (ha-nova plan raw): {"endpoint":"backups","payload":{"action":"save","category":"helpers","name":"%s","data":<its item from ws {"type":"%s/list"}, unchanged>}}. `+
			`If they were left out by mistake, read that list and send its item with the change, its id as %s_id, %s.`,
			list.noun, dropped, autoName(id), collection, collection, list.keep),
	}, nil
}

// readHelper: the helper's item from its collection's list, live; nil when
// the list doesn't hold it.
func readHelper(s *relaySession, collection, id string) (map[string]any, error) {
	data, err := rawRead(s, "ws", map[string]any{"type": collection + "/list"})
	var rows []map[string]any
	if err == nil {
		err = json.Unmarshal(data, &rows)
	}
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row["id"] == id {
			return row, nil
		}
	}
	return nil, nil
}

// helperNotFound: an update of a helper its list doesn't hold, which Home
// Assistant refuses too; the usual cause is an entity_id sent as its id.
func helperNotFound(collection, id string) error {
	return &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("there is no %s %s in Home Assistant, so nothing was sent", collection, id),
		next: fmt.Sprintf(`Its id is the id in ws {"type":"%s/list"}, not its entity_id: read that list, then plan the update with that id.`, collection)}
}

// helperUnchanged runs right before an input_select or schedule update is
// sent: the helper must still be the one the plan's basis holds
// (sceneUnchanged).
func helperUnchanged(s *relaySession, endpoint string, payload, basis map[string]any) (refusal, err error) {
	collection, id := rawHelperUpdate(endpoint, payload)
	if _, guarded := rawHelperLists[collection]; !guarded {
		return nil, nil
	}
	live, err := readHelper(s, collection, id)
	if err != nil {
		return nil, fmt.Errorf("cannot read the %s %s again before its update: %w", collection, id, err)
	}
	if helpers, _ := basis["helpers"].(map[string]any); !jsonEqual(live, helpers[collection+"/"+id]) {
		return fmt.Errorf("the %s %s changed in Home Assistant since the preview, so it was not updated", collection, id), nil
	}
	return nil, nil
}

// scheduleBlock: a time block's start and end (clockTime), and its data
// with numbers by value.
func scheduleBlock(block any) (from, to string, data map[string]any) {
	fields, _ := block.(map[string]any)
	data, _ = numbersByValue(fields["data"]).(map[string]any)
	return clockTime(fields["from"]), clockTime(fields["to"]), data
}

// clockTime: a schedule time as HH:MM:SS, as Home Assistant reads it (8:00
// is 08:00:00); one it can't read stays as sent.
func clockTime(v any) string {
	text, _ := v.(string)
	parts := strings.Split(text, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return text
	}
	var clock [3]int
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return text
		}
		clock[i] = number
	}
	return fmt.Sprintf("%02d:%02d:%02d", clock[0], clock[1], clock[2])
}

// ensureList: v as Home Assistant's ensure_list reads it.
func ensureList(v any) []any {
	switch list := v.(type) {
	case nil:
		return nil
	case []any:
		return list
	}
	return []any{v}
}
