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
	var list struct {
		Storage []struct {
			ID       string `json:"id"`
			Trackers any    `json:"device_trackers"`
		} `json:"storage"`
	}
	data, err := n.read("ws", map[string]any{"type": "person/list"})
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
// disabled tracker stops counting for their presence (followsAny).
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
	return len(ids) > 0 && n.followsAny(ids)
}

// trackerOwnerDisabled: disabling a device (with its child devices) or an
// integration entry disables their entities too (Home Assistant sets their
// disabled_by to device or config_entry), so it is typed when one of them
// is a device tracker a person follows. A failed registry read counts as
// followed. A tracker without a registry row has no device, and no read
// ties it to an entry.
func (n *rawNamer) trackerOwnerDisabled(wsType string, payload map[string]any) bool {
	if disabled, disables := payload["disabled_by"]; !disables || disabled == nil {
		return false
	}
	entry, _ := payload["entry_id"].(string)
	device, _ := payload["device_id"].(string)
	var entities []rawEntityRow
	var ok bool
	reached := func(e rawEntityRow) bool { return e.ConfigEntryID == entry }
	if wsType == "config_entries/disable" {
		data, err := n.read("ws", map[string]any{"type": "config/entity_registry/list"})
		ok = entry != "" && err == nil && json.Unmarshal(data, &entities) == nil && entities != nil &&
			!slices.ContainsFunc(entities, func(e rawEntityRow) bool { return e.EntityID == "" }) // a null row
	} else {
		var devices []rawDeviceRow
		entities, devices, ok = n.registries()
		family := map[string]bool{device: true}
		for _, d := range devices {
			if d.Parent == device {
				family[d.ID] = true
			}
		}
		ok = ok && device != ""
		reached = func(e rawEntityRow) bool { return family[e.DeviceID] }
	}
	if !ok {
		return true
	}
	var ids []string
	for _, e := range entities {
		if reached(e) && strings.HasPrefix(e.EntityID, "device_tracker.") {
			ids = append(ids, e.EntityID)
		}
	}
	return len(ids) > 0 && n.followsAny(ids)
}

// followsAny: a person follows one of the lowercased tracker ids. A failed
// read, or a list without both person lists or a person without its
// tracker ids, counts as followed: fail closed.
func (n *rawNamer) followsAny(ids []string) bool {
	var list struct {
		Storage, Config *[]*struct {
			Trackers *[]string `json:"device_trackers"`
		}
	}
	data, err := n.read("ws", map[string]any{"type": "person/list"})
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
