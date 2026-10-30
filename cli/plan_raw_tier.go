package main

import (
	"bytes"
	"cmp"
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
//	         history and statistics) or language (every later confirmation
//	         speaks it), a backup retention (it deletes older
//	         automatic backups), a Labs feature, a statistics import,
//	         and any item the skill marks typed
//	preview: everything else, a flow cancel and a snapshot prune included
//	         (skills/ha-nova/config-snapshots.md: prune is natural)

const (
	reasonRawWrite = "RAW_WRITE" // a previewed raw item; the preview line says it
	reasonRawTyped = "RAW_TYPED" // Detail says why it needs the user (rawWhys)
)

// rawWhys: why a typed raw item needs the user, by id. Its English text is
// the reason's detail, so the plan's JSON and hash stay English; sayReason
// finds the id again by that text to say it in the plan's language.
var rawWhys = map[string]func(lang string) string{
	"delete":             func(l string) string { return confirmT(l, "raw.why.delete") },
	"account_create":     func(l string) string { return confirmT(l, "raw.why.account_create") },
	"hacs_install":       func(l string) string { return confirmT(l, "raw.why.hacs_install") },
	"resource_create":    func(l string) string { return confirmT(l, "raw.why.resource_create") },
	"resource_update":    func(l string) string { return confirmT(l, "raw.why.resource_update") },
	"labs":               func(l string) string { return confirmT(l, "raw.why.labs") },
	"statistics_import":  func(l string) string { return confirmT(l, "raw.why.statistics_import") },
	"energy_emptied":     func(l string) string { return confirmT(l, "raw.why.energy_emptied") },
	"access_entity":      func(l string) string { return confirmT(l, "raw.why.access_entity") },
	"access_device":      func(l string) string { return confirmT(l, "raw.why.access_device") },
	"access_area":        func(l string) string { return confirmT(l, "raw.why.access_area") },
	"access_shown":       func(l string) string { return confirmT(l, "raw.why.access_shown") },
	"person_follows":     func(l string) string { return confirmT(l, "raw.why.person_follows") },
	"account_access":     func(l string) string { return confirmT(l, "raw.why.account_access") },
	"backup_retention":   func(l string) string { return confirmT(l, "raw.why.backup_retention") },
	"core_clock":         func(l string) string { return confirmT(l, "raw.why.core_clock") },
	"core_language":      func(l string) string { return confirmT(l, "raw.why.core_language") },
	"integration_remove": func(l string) string { return confirmT(l, "raw.why.integration_remove") },
	"file_delete":        func(l string) string { return confirmT(l, "raw.why.file_delete") },
	"copy_delete":        func(l string) string { return confirmT(l, "raw.why.copy_delete") },
	"skill_typed":        func(l string) string { return confirmT(l, "raw.why.skill_typed") },
	"app_install":        func(l string) string { return confirmT(l, "raw.why.app_install") },
	"app_uninstall":      func(l string) string { return confirmT(l, "raw.why.app_uninstall") },
	"app_settings":       func(l string) string { return confirmT(l, "raw.why.app_settings") },
}

// reasonHACSInstall: update.install on a HACS entity installs third-party
// code like hacs/repository/download, so a call takes the code too
// (maintainer decision 3, parity spec; tier_engine.go classify).
const reasonHACSInstall = "HACS_INSTALL"

// rawTypedWS: the WS writes typed besides the deletes and removals, and why
// (rawWhys).
var rawTypedWS = map[string]string{
	"recorder/clear_statistics":  "delete",
	"config/auth/create":         "account_create",
	"hacs/repository/download":   "hacs_install",
	"lovelace/resources/create":  "resource_create",
	"lovelace/resources/update":  "resource_update",
	"labs/update":                "labs",
	"recorder/import_statistics": "statistics_import",
}

var rawSceneConfigPath = regexp.MustCompile(`^/api/config/scene/config/[A-Za-z0-9_.-]+$`)

// rawItemTier: the item's tier and, for a typed one, why (rawWhys). The
// item's own typed flag only ever raises it.
func rawItemTier(n *rawNamer, item rawRequest, payload map[string]any) (tierLevel, string) {
	level, why := rawRequestTier(n, item.Endpoint, payload)
	if item.Endpoint == "supervisor" {
		level, why = rawSupervisorTier(payload)
	}
	if level < tierTyped && item.Typed {
		return tierTyped, "skill_typed"
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
		shows := false
		for _, key := range []string{"hidden_by", "disabled_by"} {
			value, set := payload[key]
			shows = shows || set && value == nil
		}
		_, labeled := payload["labels"]
		_, floored := payload["floor_id"]
		retargets := labeled || floored
		_, follows := payload["device_trackers"]
		_, language := payload["language"]
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
			return tierTyped, "delete"
		case rawTypedWS[wsType] != "":
			return tierTyped, rawTypedWS[wsType]
		case wsType == "energy/save_prefs" && n.energyEmptied(payload):
			return tierTyped, "energy_emptied"
		case wsType == "config/entity_registry/update" && carries && n.accessEntity(payload["entity_id"]):
			return tierTyped, "access_entity"
		case wsType == "config/entity_registry/update" && shows && !carries && n.accessEntity(payload["entity_id"]):
			return tierTyped, "access_shown"
		case wsType == "config/device_registry/update" && carries && n.accessDevice(payload["device_id"]):
			return tierTyped, "access_device"
		case wsType == "config/device_registry/update" && shows && !carries && n.accessDevice(payload["device_id"]):
			return tierTyped, "access_shown"
		case wsType == "config/area_registry/update" && retargets && n.accessArea(payload["area_id"]):
			return tierTyped, "access_area"
		case wsType == "person/update" && follows:
			return tierTyped, "person_follows"
		case wsType == "config/auth/update" && access:
			return tierTyped, "account_access"
		case wsType == "backup/config/update" && rawRetains(payload):
			return tierTyped, "backup_retention"
		case wsType == "config/core/update" && clock:
			return tierTyped, "core_clock"
		case wsType == "config/core/update" && language:
			return tierTyped, "core_language"
		}
	case "core":
		target, _ := payload["path"].(string)
		target, _, _ = strings.Cut(target, "#")
		target, _, _ = strings.Cut(target, "?")
		switch {
		case payload["method"] != http.MethodDelete:
		case rawEntryDeletePath.MatchString(target):
			return tierTyped, "integration_remove"
		case rawSceneConfigPath.MatchString(target):
			return tierTyped, "delete"
		}
	case "files":
		if action == "delete_file" {
			return tierTyped, "file_delete"
		}
	case "backups":
		if action == "delete" {
			return tierTyped, "copy_delete"
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
// or sits in the device family of one (familyAccess). Home Assistant
// lowercases the id (cv.entity_id). Read live; a failed read counts as one.
func (n *rawNamer) accessEntity(id any) bool {
	entityID, _ := id.(string)
	entityID = strings.ToLower(entityID)
	entities, devices, ok := n.registries()
	if !ok {
		return true
	}
	for _, e := range entities {
		if e.EntityID == entityID && n.familyAccess(entities, devices, e.DeviceID) {
			return true
		}
	}
	return accessCapable(entityID, coverClass(n.s, entityID))
}

// accessDevice: the device's family has a lock, an alarm panel, or an access
// cover (familyAccess). A device move must not do at preview what moving one
// of its entities does only as typed (accessEntity). Read live; a failed
// read counts as one.
func (n *rawNamer) accessDevice(id any) bool {
	device, _ := id.(string)
	entities, devices, ok := n.registries()
	return !ok || n.familyAccess(entities, devices, device)
}

// accessArea: an entity in the area is one accessEntity counts. Its area is
// its own, else its device's, else, for a child device without one, its
// parent's (Home Assistant 2026.9+, async_entries_for_area): a label or
// floor target reaches every entity there (helpers/target.py). Read live; a
// failed read counts as one.
func (n *rawNamer) accessArea(id any) bool {
	area, _ := id.(string)
	entities, devices, ok := n.registries()
	if !ok {
		return true
	}
	own, parent := map[string]string{}, map[string]string{}
	for _, d := range devices {
		own[d.ID], parent[d.ID] = d.Area, d.Parent
	}
	for _, e := range entities {
		if area == "" || cmp.Or(e.AreaID, own[e.DeviceID], own[parent[e.DeviceID]]) != area {
			continue
		}
		if accessCapable(e.EntityID, coverClass(n.s, e.EntityID)) || n.familyAccess(entities, devices, e.DeviceID) {
			return true
		}
	}
	return false
}

// familyAccess: a lock, an alarm panel, or an access cover sits on the
// device or in its family: its parent and that parent's child devices, as
// the sibling guard reads it (deviceAccessEntity). Home Assistant child
// devices are one level deep.
func (n *rawNamer) familyAccess(entities []rawEntityRow, devices []rawDeviceRow, device string) bool {
	if device == "" {
		return false
	}
	root := device
	for _, d := range devices {
		if d.ID == device && d.Parent != "" {
			root = d.Parent
		}
	}
	family := map[string]bool{root: true}
	for _, d := range devices {
		if d.Parent == root {
			family[d.ID] = true
		}
	}
	for _, e := range entities {
		if family[e.DeviceID] && accessCapable(e.EntityID, coverClass(n.s, e.EntityID)) {
			return true
		}
	}
	return false
}

type rawEntityRow struct {
	EntityID string `json:"entity_id"`
	DeviceID string `json:"device_id"`
	AreaID   string `json:"area_id"`
}

type rawDeviceRow struct {
	ID     string `json:"id"`
	Parent string `json:"parent_device_id"`
	Area   string `json:"area_id"`
}

// registries: the entity and device registries, read live; ok is false when
// either can't be read (a real empty registry is []) or has a row without
// its id (a null element decodes to an empty row).
func (n *rawNamer) registries() (entities []rawEntityRow, devices []rawDeviceRow, ok bool) {
	data, err := rawRead(n.s, "ws", map[string]any{"type": "config/entity_registry/list"})
	if err == nil {
		err = json.Unmarshal(data, &entities)
	}
	if err == nil {
		data, err = rawRead(n.s, "ws", map[string]any{"type": "config/device_registry/list"})
	}
	if err == nil {
		err = json.Unmarshal(data, &devices)
	}
	if err != nil || entities == nil || devices == nil {
		return nil, nil, false
	}
	for _, e := range entities {
		if e.EntityID == "" {
			return nil, nil, false
		}
	}
	for _, d := range devices {
		if d.ID == "" {
			return nil, nil, false
		}
	}
	return entities, devices, true
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
