package main

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// personFollows: a person write changes which device trackers the person
// follows, and an unlock or disarm may wait for that person (rawWhys
// person_follows). A create follows every tracker it names. An update
// replaces the whole list, and one without device_trackers empties it
// (Home Assistant's UPDATE_FIELDS default it to []), so it changes the
// trackers when the set it leaves differs from person/list's. An unknown
// person, a failed read, a listed person without device_trackers, or a
// value that is not a list of ids counts as a change: fail closed. Ids compare lowercased, as Home Assistant stores
// them.
func (n *rawNamer) personFollows(wsType string, payload map[string]any) bool {
	next, ok := trackerSet(payload["device_trackers"])
	if !ok || wsType == "person/create" {
		return !ok || len(next) > 0
	}
	if n.s == nil {
		n.missed = true
		return true
	}
	var list struct {
		Storage []struct {
			ID       string `json:"id"`
			Trackers any    `json:"device_trackers"`
		} `json:"storage"`
	}
	data, err := rawRead(n.s, "ws", map[string]any{"type": "person/list"})
	if err != nil || json.Unmarshal(data, &list) != nil {
		return true
	}
	for _, person := range list.Storage {
		if person.ID != "" && person.ID == payload["person_id"] {
			now, ok := trackerSet(person.Trackers)
			return person.Trackers == nil || !ok || !maps.Equal(now, next)
		}
	}
	return true
}

// trackerFollowed: an entity registry update renames or disables a device
// tracker a person follows, or renames one onto such an id. Home Assistant
// drops a tracker from a person only when it is removed, so a rename leaves
// the person following an id the next tracker given it takes over, and a
// disabled tracker stops counting for their presence. A failed read, or a
// list without both person lists or a person without its tracker ids,
// counts as followed: fail closed.
func (n *rawNamer) trackerFollowed(payload map[string]any) bool {
	_, renames := payload["new_entity_id"]
	disabled, disables := payload["disabled_by"]
	if !renames && (!disables || disabled == nil) {
		return false
	}
	var ids []string
	for _, key := range []string{"entity_id", "new_entity_id"} {
		if id, _ := payload[key].(string); strings.HasPrefix(strings.ToLower(id), "device_tracker.") {
			ids = append(ids, strings.ToLower(id))
		}
	}
	if len(ids) == 0 {
		return false
	}
	if n.s == nil {
		n.missed = true
		return true
	}
	var list struct {
		Storage, Config *[]*struct {
			Trackers *[]string `json:"device_trackers"`
		}
	}
	data, err := rawRead(n.s, "ws", map[string]any{"type": "person/list"})
	if err != nil || json.Unmarshal(data, &list) != nil || list.Storage == nil || list.Config == nil {
		return true
	}
	for _, person := range append(*list.Storage, *list.Config...) {
		if person == nil || person.Trackers == nil {
			return true
		}
		for _, tracker := range *person.Trackers {
			if slices.Contains(ids, strings.ToLower(tracker)) {
				return true
			}
		}
	}
	return false
}

// trackerSet: device tracker ids as a set; absent is the empty set.
func trackerSet(value any) (map[string]bool, bool) {
	set := map[string]bool{}
	if value == nil {
		return set, true
	}
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	for _, item := range list {
		id, ok := item.(string)
		if !ok {
			return nil, false
		}
		set[strings.ToLower(id)] = true
	}
	return set, true
}
