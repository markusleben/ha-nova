package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// A WS update that replaces a whole value drops what it leaves out: an
// input_select or schedule update replaces the whole helper (Home
// Assistant's _update_data keeps only the item's id, and the schedule
// schema fills a left-out weekday with []), and the updates in
// plan_raw_replace.go replace a list or reset a field. Each is checked like
// a scene save (plan_raw_scene.go): against the live item at planning and at
// apply's recompute, the item enters the plan's basis, and a removal needs a
// copy saved first (docs/work/2026-11-03-raw-omission-checks-spec.md).

// rawReplace: one guarded update type. kind and owner name the item (`the
// <kind> <id>`, `the <owner>'s current item`), why says what the update
// replaces, noun what it removes, keep how to send it all, removes what a
// needed copy keeps (copyNeeded); source is where its live item comes from,
// category the copy's. sends lists the body keys that can drop anything
// (nil: every update can); dropped lists what body leaves out of live.
// part: a restore sends back only what it dropped, so only that must hold
// no value HA NOVA hid (hiddenRemoval); else the whole item must.
type rawReplace struct {
	kind, owner, why, noun, keep, removes, source, category string
	idKey                                                   string
	part                                                    bool
	sends                                                   []string
	read                                                    func(s *relaySession, id string) (map[string]any, error)
	dropped                                                 func(live, body map[string]any) []helperDrop
}

// rawReplaced: the guarded updates by WS type.
var rawReplaced = map[string]rawReplace{
	"input_select/update": {kind: "input_select", owner: "helper", why: "an update replaces the whole helper", noun: "options", category: "helpers",
		keep: "its item with the change, its id as input_select_id, every option as text", removes: "options from a dropdown helper",
		idKey: "input_select_id", source: `ws {"type":"input_select/list"}`, read: listItem("input_select/list", "", "id"), dropped: func(live, body map[string]any) []helperDrop {
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
	"schedule/update": {kind: "schedule", owner: "helper", why: "an update replaces the whole helper", noun: "time blocks", category: "helpers",
		keep: "its item with the change, its id as schedule_id, every time block with its data", removes: "time blocks from a schedule",
		idKey: "schedule_id", source: `ws {"type":"schedule/list"}`, read: listItem("schedule/list", "", "id"), dropped: func(live, body map[string]any) []helperDrop {
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
	"person/update":                   rawPersonUpdate,
	"config/entity_registry/update":   rawEntityUpdate,
	"config/device_registry/update":   rawDeviceUpdate,
	"config/area_registry/update":     rawAreaUpdate,
	"config/floor_registry/update":    rawFloorUpdate,
	"assist_pipeline/pipeline/update": rawPipelineUpdate,
}

// rawReplaceTarget: the guarded update a request is, with its id and the
// key its live item has in the basis; ok is false for any other request,
// and for an update that sends nothing it could drop.
func rawReplaceTarget(endpoint string, payload map[string]any) (guard rawReplace, id, key string, ok bool) {
	wsType, _ := payload["type"].(string)
	guard, ok = rawReplaced[wsType]
	id, _ = payload[guard.idKey].(string)
	if endpoint != "ws" || !ok || id == "" {
		return guard, "", "", false
	}
	sends := guard.sends == nil
	for _, field := range guard.sends {
		sends = sends || payload[field] != nil
	}
	key = id
	if guard.idKey == "entity_id" { // Home Assistant lowercases it
		key = strings.ToLower(id)
	}
	return guard, id, strings.TrimSuffix(wsType, "/update") + "/" + key, sends
}

// helperDrop: what an update removes: an entry (option; list names the
// list it leaves when there are several), or a schedule time block (data:
// only its data goes).
type helperDrop struct {
	option, list, day, from, to string
	data                        bool
}

// String: the drop as a refusal names it.
func (d helperDrop) String() string {
	switch {
	case d.day == "" && d.list != "":
		return jsonText(d.option) + " (" + d.list + ")"
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

// helperNote: the sentence after a guarded update's line naming what it
// removes after its copy (helperKept); "" for any other.
func (n *rawNamer) helperNote(lang, endpoint string, payload map[string]any) string {
	guard, _, key, _ := rawReplaceTarget(endpoint, payload)
	var names []string
	for _, drop := range n.helperDrops[key] {
		names = append(names, drop.say(lang))
	}
	switch {
	case len(names) == 0:
		return ""
	case guard.kind == "input_select":
		return confirmT(lang, "raw.options_drops", "options", sayList(lang, names))
	case guard.kind == "schedule":
		return confirmT(lang, "raw.schedule_drops", "blocks", sayList(lang, names))
	}
	return confirmT(lang, "raw.omits", "items", sayList(lang, names))
}

// kept: sceneKept, fileKept, or helperKept, whichever guards the request.
func (n *rawNamer) kept(earlier []rawRequest, endpoint string, payload map[string]any) (refusal, err error) {
	switch {
	case endpoint == "core":
		return n.sceneKept(earlier, payload)
	case rawFileChange(endpoint, payload) != "":
		return n.fileKept(earlier, payload)
	}
	return n.helperKept(earlier, endpoint, payload)
}

// helperKept: a guarded update (rawReplaced) that leaves out what the live
// item holds needs a backups save of exactly its live item earlier in the
// plan (sceneKept). Keeps the live item for the basis, and what the update
// drops for its line (helperNote). An item that can't be read, or isn't
// there, is an error: nothing is saved over unseen.
func (n *rawNamer) helperKept(earlier []rawRequest, endpoint string, payload map[string]any) (refusal, err error) {
	guard, id, key, guarded := rawReplaceTarget(endpoint, payload)
	switch _, again := n.helpers[key]; {
	case !guarded:
		return nil, nil
	case again: // each update is checked against the live item, not the one before it
		return rawDrop{message: "a plan updates the " + guard.kind + " " + id + " once", hint: "Put every change to the " + guard.owner + " into one update."}, nil
	}
	live, err := guard.read(n.s, id)
	switch {
	case isUpstreamNotFound(err):
		return nil, helperNotFound(guard.kind, id, guard.source)
	case err != nil:
		return nil, fmt.Errorf("cannot read the %s %s, so nothing was planned or sent: %w", guard.kind, id, err)
	case live == nil:
		return nil, helperNotFound(guard.kind, id, guard.source)
	}
	if n.helpers == nil {
		n.helpers, n.helperDrops = map[string]any{}, map[string][]helperDrop{}
	}
	n.helpers[key] = live
	drops := guard.dropped(live, payload)
	if len(drops) == 0 {
		return nil, nil
	}
	var names []string
	for _, drop := range drops {
		names = append(names, drop.String())
	}
	dropped := strings.Join(names, ", ")
	removal := fmt.Sprintf("updating the %s %s would remove the %s %s from it", guard.kind, id, guard.noun, dropped)
	var held any = live
	if guard.part {
		held = names
	}
	if hidden := hiddenRemoval(held, removal); hidden != nil {
		return hidden, nil
	}
	if copySaved(earlier, live) {
		n.helperDrops[key] = drops
		return nil, nil
	}
	return rawDrop{
		message: removal + ", with no copy of its current item saved first",
		hint: fmt.Sprintf(`This would remove the %s %s: %s. If the user asked for that (renaming, moving, or changing an entry removes the old one), tell them which ones go, `+
			`then plan it with a backups save of the %s's current item first (ha-nova plan raw): {"endpoint":"backups","payload":{"action":"save","category":"%s","name":%s,"data":<its item from %s, unchanged>}}. `+
			`If they were left out by mistake, read it again and send %s.`,
			guard.noun, dropped, guard.why, guard.owner, guard.category, jsonText(autoName(id)), guard.source, guard.keep),
	}, nil
}

// listItem: a read of the item whose idField is id in a WS list (under
// that key of the answer when under is set), live; nil when the list
// doesn't hold it.
func listItem(listType, under, idField string) func(s *relaySession, id string) (map[string]any, error) {
	return func(s *relaySession, id string) (map[string]any, error) {
		data, err := rawRead(s, "ws", map[string]any{"type": listType})
		if err == nil && under != "" {
			var answer map[string]json.RawMessage
			if err = json.Unmarshal(data, &answer); err == nil {
				data = answer[under]
			}
		}
		var rows []map[string]any
		if err == nil {
			err = json.Unmarshal(data, &rows)
		}
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row[idField] == id {
				return row, nil
			}
		}
		return nil, nil
	}
}

// readHelper: the helper's item from its collection's list, live; nil when
// the list doesn't hold it.
func readHelper(s *relaySession, collection, id string) (map[string]any, error) {
	return listItem(collection+"/list", "", "id")(s, id)
}

// helperNotFound: an update of an item its list doesn't hold, which Home
// Assistant refuses too; the usual cause is an entity_id sent as its id.
func helperNotFound(kind, id, source string) error {
	return &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("there is no %s %s in Home Assistant, so nothing was sent", kind, id),
		next: fmt.Sprintf("Its id is the id in %s, not its entity_id: read that list, then plan the update with that id.", source)}
}

// helperUnchanged runs right before a guarded update is sent: the item must
// still be the one the plan's basis holds (sceneUnchanged).
func helperUnchanged(s *relaySession, endpoint string, payload, basis map[string]any) (refusal, err error) {
	guard, id, key, guarded := rawReplaceTarget(endpoint, payload)
	if !guarded {
		return nil, nil
	}
	live, err := guard.read(s, id)
	if err != nil {
		return nil, fmt.Errorf("cannot read the %s %s again before its update: %w", guard.kind, id, err)
	}
	if helpers, _ := basis["helpers"].(map[string]any); !jsonEqual(live, helpers[key]) {
		return fmt.Errorf("the %s %s changed in Home Assistant since the preview, so it was not updated", guard.kind, id), nil
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
