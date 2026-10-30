package main

import (
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
// alarm panel, or an access cover, nor one whose child device without a room
// of its own has one (since Home Assistant 2026.9 it takes the parent's room).
// A new room can change what an area-targeted unlock acts on, so that move
// takes the user's own confirmation as a raw device update (rawRequestTier).
// A failed read refuses too.
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
	items, moving, under := map[string]bool{}, map[string]bool{}, map[string]string{}
	for _, it := range req.Items {
		items[it.DeviceID], moving[it.DeviceID] = true, true
	}
	for device, parent := range reg.parent {
		if items[parent] && !items[device] && reg.devices[device].area == "" {
			moving[device], under[device] = true, parent
		}
	}
	for _, row := range rows {
		if !moving[row.DeviceID] || !accessCapable(row.EntityID, coverClass(s, row.EntityID)) {
			continue
		}
		if parent := under[row.DeviceID]; parent != "" {
			child := sayText(reg.devices[row.DeviceID].name, true)
			return &verbFailure{exit: verbExitRefused, code: "REFUSED", message: child + " has " + row.EntityID + " (a lock, alarm panel, or door) and takes the room of " + sayText(reg.devices[parent].name, true) + ", a device in this plan; nothing was planned",
				next: "Tell the user: once " + child + " has its own room in Home Assistant's device settings, moving its parent no longer moves it; then plan again."}
		}
		return &verbFailure{exit: verbExitRefused, code: "REFUSED", message: "a device in this plan has " + row.EntityID + " (a lock, alarm panel, or door); nothing was planned",
			next: "Move that device as a raw plan (config/device_registry/update with its area_id), which needs the user's own confirmation; plan the other devices without it."}
	}
	return nil
}
