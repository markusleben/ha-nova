package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
)

// The confirmation tier of every raw plan item, in one place: the rungs the
// skills use for the same request on a computer (parity spec → Writes).
// plan_raw_tier_test.go plans every allowlisted write against it.
//
//	typed:   every delete or removal (WS */delete and */remove,
//	         recorder/clear_statistics, DELETE …/config_entries/entry/… and
//	         …/scene/config/…, files delete_file, backups delete), a new
//	         account, HACS downloads and dashboard resources (code that runs
//	         in Home Assistant or every signed-in browser), an energy save that
//	         empties a list or drops more than one entry, a new id, area, or
//	         label carried onto a lock, alarm panel, or access cover, a person
//	         who follows other devices (skills/device → Access Check), an
//	         account's administrator flag, active state, or local-only
//	         sign-in, Home Assistant's time zone or units (they shift
//	         history and statistics), a backup retention (it deletes older
//	         automatic backups), a Labs feature, a statistics import,
//	         and any item the skill marks typed
//	preview: everything else, a flow cancel and a snapshot prune included
//	         (skills/ha-nova/config-snapshots.md: prune is natural)

const (
	reasonRawWrite = "RAW_WRITE" // a previewed raw item; the preview line says it
	reasonRawTyped = "RAW_TYPED" // Detail says why it needs the user
)

const rawWhyDelete = "This deletes something in Home Assistant"

// reasonHACSInstall: update.install on a HACS entity installs third-party
// code like hacs/repository/download, so a call takes the code too
// (maintainer decision 3, parity spec; tier_engine.go classify).
const reasonHACSInstall = "HACS_INSTALL"

// rawTypedWS: the WS writes typed besides the deletes and removals.
var rawTypedWS = map[string]string{
	"recorder/clear_statistics":  rawWhyDelete,
	"config/auth/create":         "This creates a Home Assistant account",
	"hacs/repository/download":   "This installs code from HACS that runs in Home Assistant",
	"lovelace/resources/create":  "This adds code that runs in every browser signed in to Home Assistant",
	"lovelace/resources/update":  "This changes code that runs in every browser signed in to Home Assistant",
	"labs/update":                "This turns an experimental Home Assistant feature on or off",
	"recorder/import_statistics": "This writes long-term statistics; rows already stored for the same hours are overwritten",
}

var rawSceneConfigPath = regexp.MustCompile(`^/api/config/scene/config/[A-Za-z0-9_.-]+$`)

// rawItemTier: the item's tier and, for a typed one, why (one sentence).
// The item's own typed flag only ever raises it.
func rawItemTier(n *rawNamer, item rawRequest, payload map[string]any) (tierLevel, string) {
	level, why := rawRequestTier(n, item.Endpoint, payload)
	if item.Endpoint == "supervisor" {
		level, why = rawSupervisorTier(payload)
	}
	if level < tierTyped && item.Typed {
		return tierTyped, "This step needs your own confirmation, as the skill asks"
	}
	return level, why
}

func rawRequestTier(n *rawNamer, endpoint string, payload map[string]any) (tierLevel, string) {
	action, _ := payload["action"].(string)
	switch endpoint {
	case "ws":
		wsType, _ := payload["type"].(string)
		_, carries := payload["new_entity_id"]
		for _, key := range []string{"area_id", "labels"} {
			_, set := payload[key]
			carries = carries || set
		}
		_, follows := payload["device_trackers"]
		access, clock := false, false
		for _, key := range []string{"group_ids", "is_active", "local_only"} {
			_, set := payload[key]
			access = access || set
		}
		for _, key := range []string{"time_zone", "unit_system", "update_units"} {
			_, set := payload[key]
			clock = clock || set
		}
		switch {
		case strings.HasSuffix(wsType, "/delete") || strings.HasSuffix(wsType, "/remove"):
			return tierTyped, rawWhyDelete
		case rawTypedWS[wsType] != "":
			return tierTyped, rawTypedWS[wsType]
		case wsType == "energy/save_prefs" && n.energyEmptied(payload):
			return tierTyped, "This empties a list of the Energy dashboard or drops several of its entries"
		case wsType == "config/entity_registry/update" && carries && n.accessEntity(payload["entity_id"]):
			return tierTyped, "This carries a new ID, area, or label onto a lock, alarm panel, or door, which can change what an automation that opens it targets"
		case wsType == "config/device_registry/update" && carries && n.accessDevice(payload["device_id"]):
			return tierTyped, "This carries a new area or label onto a device with a lock, alarm panel, or door, which can change what an automation that opens it targets"
		case wsType == "person/update" && follows:
			return tierTyped, "This changes which devices a person follows, and an unlock or disarm may wait for that person"
		case wsType == "config/auth/update" && access:
			return tierTyped, "This changes what a Home Assistant account may do, or whether it can sign in"
		case wsType == "backup/config/update" && rawRetains(payload):
			return tierTyped, "This changes how many automatic backups Home Assistant keeps, and older ones are deleted for good"
		case wsType == "config/core/update" && clock:
			return tierTyped, "This changes Home Assistant's time zone or units, which shifts how history and long-term statistics read"
		}
	case "core":
		target, _ := payload["path"].(string)
		target, _, _ = strings.Cut(target, "#")
		target, _, _ = strings.Cut(target, "?")
		switch {
		case payload["method"] != http.MethodDelete:
		case rawEntryDeletePath.MatchString(target):
			return tierTyped, "This removes an integration and its devices from Home Assistant"
		case rawSceneConfigPath.MatchString(target):
			return tierTyped, rawWhyDelete
		}
	case "files":
		if action == "delete_file" {
			return tierTyped, "This deletes a file in Home Assistant's config folder"
		}
	case "backups":
		if action == "delete" {
			return tierTyped, "This deletes a saved config copy"
		}
	}
	return tierPreview, ""
}

// rawRetains: a backup settings change that sets a retention, global or
// per location; Home Assistant deletes the automatic backups beyond it.
func rawRetains(payload map[string]any) bool {
	_, retains := payload["retention"]
	agents, _ := payload["agents"].(map[string]any)
	for _, agent := range agents {
		fields, _ := agent.(map[string]any)
		_, set := fields["retention"]
		retains = retains || set
	}
	return retains
}

// accessEntity: the entity is a lock, an alarm panel, or an access cover,
// or sits on a device with one (read live; a failed read counts as one).
func (n *rawNamer) accessEntity(id any) bool {
	entityID, _ := id.(string)
	var rows []struct {
		EntityID string `json:"entity_id"`
		DeviceID string `json:"device_id"`
	}
	data, err := rawRead(n.s, "ws", map[string]any{"type": "config/entity_registry/list"})
	if err != nil || json.Unmarshal(data, &rows) != nil {
		return true
	}
	device := ""
	for _, row := range rows {
		if row.EntityID == entityID {
			device = row.DeviceID
		}
	}
	for _, row := range rows {
		if (row.EntityID == entityID || device != "" && row.DeviceID == device) && accessCapable(row.EntityID, coverClass(n.s, row.EntityID)) {
			return true
		}
	}
	return accessCapable(entityID, coverClass(n.s, entityID))
}

// accessDevice: the device has a lock, an alarm panel, or an access cover
// (read live; a failed read counts as one).
func (n *rawNamer) accessDevice(id any) bool {
	device, _ := id.(string)
	var rows []struct {
		EntityID string `json:"entity_id"`
		DeviceID string `json:"device_id"`
	}
	data, err := rawRead(n.s, "ws", map[string]any{"type": "config/entity_registry/list"})
	if err != nil || json.Unmarshal(data, &rows) != nil || rows == nil {
		return true
	}
	for _, row := range rows {
		if device != "" && row.DeviceID == device && accessCapable(row.EntityID, coverClass(n.s, row.EntityID)) {
			return true
		}
	}
	return false
}

// energyEmptied: the save empties a list that has entries now, or drops
// more than one of them (save_prefs replaces each list it names; a single
// add, change, or removal is a preview, skills/energy → Config edit). A
// failed read, or a value that is not a list, counts as emptied: fail
// closed. Numbers compare by value (0.30 is 0.3).
func (n *rawNamer) energyEmptied(payload map[string]any) bool {
	prefs, ok := n.energyPrefs()
	for key, value := range payload {
		if key == "type" {
			continue
		}
		next, isList := numbersByValue(value).([]any)
		if !ok || !isList {
			return true
		}
		before, _ := numbersByValue(prefs[key]).([]any)
		gone := 0
		for _, old := range before {
			kept := false
			for _, now := range next {
				kept = kept || reflect.DeepEqual(old, now)
			}
			if !kept {
				gone++
			}
		}
		if len(before) > 0 && len(next) == 0 || gone > 1 {
			return true
		}
	}
	return false
}

// numbersByValue turns every json.Number into a float64, so 0.30 and 0.3
// are the same entry.
func numbersByValue(v any) any {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return t.String()
		}
		return f
	case map[string]any:
		out := map[string]any{}
		for key, value := range t {
			out[key] = numbersByValue(value)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, value := range t {
			out[i] = numbersByValue(value)
		}
		return out
	}
	return v
}

// energyPrefs reads energy/get_prefs once (numbers kept as written); never
// configured (not_found) reads as no lists.
func (n *rawNamer) energyPrefs() (map[string]any, bool) {
	if n.prefsRead {
		return n.prefs, n.prefsOK
	}
	n.prefsRead, n.prefs = true, map[string]any{}
	data, err := rawRead(n.s, "ws", map[string]any{"type": "energy/get_prefs"})
	var call *relayCallError
	switch {
	case err == nil:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		n.prefsOK = dec.Decode(&n.prefs) == nil && n.prefs != nil
	case errors.As(err, &call) && strings.Contains(strings.ToLower(call.Error()), "not_found"):
		n.prefsOK = true
	}
	return n.prefs, n.prefsOK
}

// snapshotListed: a config copy a delete names must be a file the store
// lists now (skills/ha-nova/config-snapshots.md → Batch delete); its listed
// entry goes into the basis, so a copy gone or changed since the preview
// is drift.
func (n *rawNamer) snapshotListed(payload map[string]any) (refusal, err error) {
	if n.snapshots == nil {
		data, err := rawSnapshotList(n.s)
		var rows []map[string]any
		if err == nil {
			err = json.Unmarshal(data, &rows)
		}
		if err != nil {
			return nil, fmt.Errorf("cannot list the config copies, so nothing was planned: %w", err)
		}
		n.snapshots = map[string]any{}
		for _, row := range rows {
			if file, _ := row["file"].(string); file != "" {
				n.snapshots[file] = row
			}
		}
	}
	file, _ := payload["file"].(string)
	if _, listed := n.snapshots[file]; !listed {
		return fmt.Errorf("the config copy %q is not in the snapshot store; list the copies again and name one exactly", file), nil
	}
	if n.deleting == nil {
		n.deleting = map[string]any{}
	}
	n.deleting[file] = n.snapshots[file]
	return nil, nil
}

// rawSnapshotList reads the Relay's snapshot list, unfiltered.
func rawSnapshotList(s *relaySession) (json.RawMessage, error) {
	return s.sendWithin("backups", []byte(`{"action":"list"}`), relaySessionRequestTimeout)
}

// basis: the energy lists a save replaces, so a source another client added
// since the preview is drift, never silently dropped (skills/energy →
// Config edit), the listed entry of every config copy a delete names, and
// what each device delete removes (its devices and entities: one more since
// the preview is drift); nil when the plan holds none.
func (n *rawNamer) basis() map[string]any {
	basis := map[string]any{}
	switch {
	case !n.prefsRead:
	case !n.prefsOK:
		basis["energy"] = "unreadable"
	default:
		basis["energy"] = numbersByValue(n.prefs) // a stored plan reads its numbers back as float64
	}
	if n.deleting != nil {
		basis["snapshots"] = n.deleting
	}
	if n.removing != nil {
		basis["devices"] = n.removing
	}
	if len(basis) == 0 {
		return nil
	}
	return basis
}
