package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
)

// roomsAgain: what to do when a device changed since the preview.
func roomsAgain(req roomsRequest) string {
	if req.Undo {
		return "Nothing was reverted: a device changed since. Tell the user; they can move devices in Home Assistant's own device settings."
	}
	return "Nothing was sent. Plan again with ha-nova plan rooms, show its preview, and get confirmation again."
}

// roomsAccessRefusal: a rooms plan never moves a device with a lock, an
// alarm panel, or an access cover, nor one in the family of one: its parent
// and that parent's child devices (Home Assistant 2026.9+), as the raw tier
// and the sibling guard read it. A new room can change what an area-targeted
// unlock acts on, so that move takes the user's own confirmation as a raw
// device update (rawRequestTier). A failed read refuses too.
func roomsAccessRefusal(s *relaySession, req roomsRequest, reg roomsRegistry) error {
	var rows []struct {
		EntityID string `json:"entity_id"`
		DeviceID string `json:"device_id"`
	}
	data, err := s.ws(map[string]any{"type": "config/entity_registry/list"})
	if err == nil {
		err = json.Unmarshal(data, &rows)
	}
	if err == nil && rows == nil {
		err = errors.New("it came back empty")
	}
	if err != nil {
		return fmt.Errorf("cannot read the entity registry to check for locks, so nothing was planned: %w", err)
	}
	items, via := map[string]bool{}, map[string]string{}
	for _, it := range req.Items {
		items[it.DeviceID] = true
	}
	for _, it := range req.Items {
		root := cmp.Or(reg.parent[it.DeviceID], it.DeviceID)
		family := []string{root}
		for device, parent := range reg.parent {
			if parent == root {
				family = append(family, device)
			}
		}
		for _, device := range family {
			if !items[device] && via[device] == "" {
				via[device] = it.DeviceID
			}
		}
	}
	for _, row := range rows {
		item := via[row.DeviceID]
		if !items[row.DeviceID] && item == "" || !accessCapable(row.EntityID, coverClass(s, row.EntityID)) {
			continue
		}
		if item == "" {
			return &verbFailure{exit: verbExitRefused, code: "REFUSED", message: "a device in this plan has " + row.EntityID + " (a lock, alarm panel, or door); nothing was planned",
				next: "Move that device as a raw plan (config/device_registry/update with its area_id), which needs the user's own confirmation; plan the other devices without it."}
		}
		relation := " shares a parent device with "
		switch {
		case reg.parent[row.DeviceID] == item:
			relation = " is a child device of "
		case reg.parent[item] == row.DeviceID:
			relation = " is the parent device of "
		}
		name := sayText(reg.devices[item].name, true)
		return &verbFailure{exit: verbExitRefused, code: "REFUSED", message: sayText(reg.devices[row.DeviceID].name, true) + " has " + row.EntityID + " (a lock, alarm panel, or door) and" + relation + name + ", a device in this plan; nothing was planned",
			next: "Move " + name + " as a raw plan (config/device_registry/update with its area_id), which needs the user's own confirmation; plan the other devices without it."}
	}
	return nil
}
