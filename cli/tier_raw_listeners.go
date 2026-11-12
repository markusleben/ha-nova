package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
)

// Raw writes that change what a trigger sees (#946,
// docs/work/2026-11-05-raw-write-listeners-spec.md): Home Assistant writes a
// new state or new attributes for the entity a write changes, so the
// automations that reference that entity are classified as a call's
// consumers are (scanConsumers). One that unlocks, opens, or disarms makes
// the write typed; a read that fails is typed (I4).

// rawStateless: the fields of these writes that change no state or
// attribute. A write that sets only these is not scanned. Any change to an
// integration entry runs its update listeners, which often reload it.
var rawStateless = map[string][]string{
	"config/entity_registry/update": {"type", "entity_id", "aliases", "area_id", "categories", "hidden_by", "labels"},
	"config/device_registry/update": {"type", "device_id", "area_id", "labels"},
	"config_entries/update":         {"type", "entry_id"},
}

// rawListened: the entities whose state or attributes a raw write changes,
// read by the returned func (it records a read it can't make as a gap); nil
// when the write changes none. A write Home Assistant refuses outright (no
// id, or no valid entity_id) changes none. Enabling an entity or a device
// reloads its integration entries 30 s later (Home Assistant's
// EntityRegistryDisabledHandler), and every entity of them goes unavailable
// and back; so does an entry update, and finishing an options, reconfigure,
// or subentry flow, which is checked at its start, where the entry is named
// (rawAccessTarget). Saving a scene reloads every scene.
func rawListened(endpoint string, payload map[string]any) func(e *tierEngine) []string {
	wsType, _ := payload["type"].(string)
	if endpoint == "core" {
		body, _ := json.Marshal(payload)
		method, _ := payload["method"].(string)
		target, _ := payload["path"].(string)
		target, _, _ = strings.Cut(target, "#")
		target, _, _ = strings.Cut(target, "?")
		switch entry := rawAccessTarget(endpoint, body).entry; {
		case entry != "" && entry != "(none)":
			return func(e *tierEngine) []string {
				return e.registryIDs(func(row fullRegistryRow) bool { return row.ConfigEntryID == entry })
			}
		case strings.EqualFold(strings.TrimSpace(method), http.MethodPost) && rawSceneConfigPath.MatchString(target):
			return func(e *tierEngine) []string {
				return e.registryIDs(func(row fullRegistryRow) bool {
					return row.Platform == "homeassistant" && strings.HasPrefix(row.EntityID, "scene.")
				})
			}
		}
		return nil
	}
	if endpoint != "ws" {
		return nil
	}
	_, floored := payload["floor_id"]
	if _, labeled := payload["labels"]; wsType == "config/area_registry/update" && (floored || labeled) {
		return func(*tierEngine) []string { return nil } // a place carry (rawCarry)
	}
	if keep, listed := rawStateless[wsType]; listed {
		changes := false
		for key := range payload {
			changes = changes || !slices.Contains(keep, key)
		}
		_, moves := payload["area_id"]
		_, labeled := payload["labels"]
		switch {
		case !changes && (moves || labeled) && wsType != "config_entries/update": // a place carry: no state changes, the place's triggers widen (rawCarry)
			return func(*tierEngine) []string { return nil }
		case !changes:
			return nil
		}
	}
	text := func(key string) string { s, _ := payload[key].(string); return s }
	sets := func(keys ...string) bool {
		return slices.ContainsFunc(keys, func(key string) bool { _, set := payload[key]; return set })
	}
	located, converts := sets("latitude", "longitude", "elevation", "radius", "location_name"), sets("unit_system", "update_units")
	disabled, toggles := payload["disabled_by"]
	trackers, _ := payload["device_trackers"].([]any)
	enables := toggles && disabled == nil
	collection, op, idKey := rawCollection(wsType)
	movedToo := func(e *tierEngine, id string) { // the scan reads where it sits now, not where the write puts it (MAR-142)
		if (enables || sets("new_entity_id")) && sets("area_id", "labels") {
			e.gap(reasonUnresolvedTarget, "", "", id+" also moves to another area or label")
		}
	}
	switch id := rawEntityID(payload["entity_id"]); {
	case (wsType == "calendar/event/create" || wsType == "calendar/event/update" || wsType == "calendar/event/delete") && id != "":
		return func(*tierEngine) []string { return []string{id} }
	case wsType == "config/entity_registry/remove" && id != "":
		return func(*tierEngine) []string { return []string{id} }
	case wsType == "config/entity_registry/update" && id != "":
		return func(e *tierEngine) []string {
			if _, listed := e.reg[id]; !listed { // a disabled entity and its device are only in the full registry (deviceOf)
				if _, err := e.fullRegistry(); err != nil {
					e.readFailed(id, "", "registry")
				}
			}
			movedToo(e, id)
			ids := []string{id, rawEntityID(payload["new_entity_id"])}
			if enables {
				ids = append(ids, e.entryMates(ids[:1])...)
			}
			return ids
		}
	case (wsType == "config/device_registry/update" || wsType == "config/device_registry/remove") && text("device_id") != "":
		return func(e *tierEngine) []string {
			movedToo(e, text("device_id"))
			family := []string{text("device_id")}
			if toggles || wsType == "config/device_registry/remove" { // its child devices go with it (rawDeviceFamily)
				family = e.deviceFamily(text("device_id"))
			}
			ids := e.registryIDs(func(row fullRegistryRow) bool { return slices.Contains(family, row.DeviceID) })
			if enables {
				ids = append(ids, e.entryMates(ids)...)
			}
			return ids
		}
	case slices.Contains([]string{"config_entries/disable", "config_entries/update", "config_entries/subentries/update", "config_entries/subentries/delete"}, wsType) && text("entry_id") != "":
		return func(e *tierEngine) []string {
			return e.registryIDs(func(row fullRegistryRow) bool { return row.ConfigEntryID == text("entry_id") })
		}
	case wsType == "config/core/update" && (located || converts):
		return func(e *tierEngine) []string {
			var ids []string
			if located {
				ids = append([]string{"sun.sun", "zone.home"}, e.registryIDs(func(row fullRegistryRow) bool { return row.Platform == "sun" })...)
			}
			if converts { // Home Assistant shows these in the unit system's units
				ids = append(ids, e.statesWhere(func(id string, attributes map[string]any) bool {
					domain, _ := splitID(id)
					return attributes["unit_of_measurement"] != nil || domain == "climate" || domain == "water_heater" || domain == "weather"
				})...)
			}
			return ids
		}
	case wsType == "zone/create" || wsType == "person/create" && len(trackers) > 0: // a smaller zone, or a new person, changes whom zone.home counts
		return func(*tierEngine) []string { return []string{"zone.home"} }
	case (op == "update" || op == "delete") && text(idKey) != "" && !strings.HasPrefix(collection, "config/"):
		return func(e *tierEngine) []string {
			ids := e.registryIDs(func(row fullRegistryRow) bool {
				return row.Platform == collection && string(row.UniqueID) == text(idKey)
			})
			if len(ids) != 1 && e.fullRead {
				e.gap(reasonUnresolvedTarget, "", "", collection+" "+text(idKey))
			}
			return ids
		}
	}
	return nil
}

// rawEntityID: an entity_id as Home Assistant reads it (cv.entity_id
// lowercases), or "" for one it refuses.
func rawEntityID(v any) string {
	id, _ := v.(string)
	if id = strings.ToLower(id); !entityIDPattern.MatchString(id) {
		return ""
	}
	return id
}

// rawListeners: the class a raw write reaches through the automations that
// reference what it changes (rawListened), each entity scanned like a call's
// target. Each entity's class is kept (done), since scanConsumers lifts
// nothing for an entity it scanned before; its consumers' classes are
// lifted for one an earlier walk scanned. An unreadable registry is typed.
func (e *tierEngine) rawListeners(endpoint string, payload map[string]any) tierLevel {
	listened := rawListened(endpoint, payload)
	if listened == nil {
		return tierOrdinary
	}
	class := tierOrdinary
	e.scopes = append(e.scopes, &class)
	consumers := len(e.consumers)
	if len(e.reg) == 0 && !e.regFailed {
		e.loadRegistry()
	}
	ids := listened(e)
	if slices.ContainsFunc(ids, func(id string) bool {
		domain, _ := splitID(id)
		return domain == "person" || domain == "device_tracker" || domain == "zone"
	}) { // a zone moves people and trackers into or out of it, a person follows its trackers, and a zone counts the people in it
		ids = append(ids, e.statesWhere(func(id string, attributes map[string]any) bool {
			domain, _ := splitID(id)
			return domain == "person" || domain == "zone" || domain == "device_tracker" && attributes["latitude"] != nil
		})...)
	}
	sort.Strings(ids)
	for _, id := range slices.Compact(slices.DeleteFunc(ids, func(id string) bool { return id == "" })) {
		key := "entity listeners " + id
		if own, scanned := e.done[key]; scanned {
			e.lift(own)
			continue
		}
		own := tierOrdinary
		e.scopes = append(e.scopes, &own)
		e.scanConsumers(id, 0, stateDelta{})
		for automation, ons := range e.firedOn {
			if slices.ContainsFunc(ons, func(f fired) bool { return f.on == id }) {
				e.lift(e.done["consumer "+automation])
			}
		}
		e.scopes = e.scopes[:len(e.scopes)-1]
		e.done[key] = own
	}
	e.rawTakeover(endpoint, payload)
	e.rawCarry(endpoint, payload)
	if len(e.consumers) > consumers { // a raw write calls nothing itself: only what its consumers call may set off a call_service listener (MAR-148)
		e.callServiceListeners()
	}
	e.scopes = e.scopes[:len(e.scopes)-1]
	if e.regFailed {
		class = tierTyped
	}
	return class
}

// registryIDs: the entities of the full registry, disabled ones included,
// that picks; a failed read is a gap.
func (e *tierEngine) registryIDs(picks func(fullRegistryRow) bool) []string {
	rows, err := e.fullRegistry()
	if err != nil {
		e.readFailed("", "", "registry")
		return nil
	}
	var ids []string
	for _, row := range rows {
		if picks(row) {
			ids = append(ids, row.EntityID)
		}
	}
	return ids
}

// entryMates: every entity of the integration entries the ids belong to.
func (e *tierEngine) entryMates(ids []string) []string {
	rows, err := e.fullRegistry()
	if err != nil {
		e.readFailed("", "", "registry")
		return nil
	}
	entries := map[string]bool{}
	for _, row := range rows {
		if row.ConfigEntryID != "" && slices.Contains(ids, row.EntityID) {
			entries[row.ConfigEntryID] = true
		}
	}
	return e.registryIDs(func(row fullRegistryRow) bool { return entries[row.ConfigEntryID] })
}

// deviceOf: the entity's device from the compact registry, else from the
// full one once read: only it lists a disabled entity.
func (e *tierEngine) deviceOf(id string) string {
	if device := e.reg[id].di; device != "" || !e.fullRead {
		return device
	}
	for _, row := range e.full {
		if row.EntityID == id {
			return row.DeviceID
		}
	}
	return ""
}

// statesWhere: the entities of the live state list that picks; a failed
// read is a gap.
func (e *tierEngine) statesWhere(picks func(id string, attributes map[string]any) bool) []string {
	status, body, err := e.s.coreGET("/api/states")
	var states []struct {
		EntityID   string         `json:"entity_id"`
		Attributes map[string]any `json:"attributes"`
	}
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/states", status)
	}
	if err == nil {
		err = json.Unmarshal(body, &states)
	}
	if err != nil {
		e.readFailed("", "", "states")
		return nil
	}
	var ids []string
	for _, st := range states {
		if picks(st.EntityID, st.Attributes) {
			ids = append(ids, st.EntityID)
		}
	}
	return ids
}

// relayListeners refuses a write, or a collect_events envelope's message,
// whose listeners a plan rates typed (rawListeners): ha-nova relay sends at
// once, and only a plan asks the user. It checks live; a failed read is
// typed, so refused too.
func relayListeners(paths runtimePaths, opts verbOptions, endpoint string, payload, message map[string]any) (refusal, err error) {
	var s *relaySession
	defer func() {
		if s != nil {
			s.close()
		}
	}()
	for i, request := range []map[string]any{payload, message} {
		if rawListened(endpoint, request) == nil {
			continue
		}
		if s == nil {
			if s, err = newRelaySession(paths, opts); err != nil {
				return nil, err
			}
		}
		if newTierEngine(s).rawListeners(endpoint, request) < tierTyped {
			continue
		}
		what, _ := request["type"].(string)
		if endpoint == "core" {
			what = fmt.Sprint(request["method"], " ", request["path"])
		}
		shown := map[bool]string{true: "<the message itself, without a collect_events envelope>", false: "<this request>"}[i == 1]
		return rawRefusal(what, "an automation that listens to what it changes may unlock, open, or disarm, or HA NOVA could not read everything that listens to it, so only the user may confirm it, and only a plan asks them: "+
			fmt.Sprintf(`put [{"endpoint":%q,"payload":%s}] in a file, run ha-nova plan raw --file <file>, show the user its say, and follow its next_step`, endpoint, shown)), nil
	}
	return nil, nil
}
