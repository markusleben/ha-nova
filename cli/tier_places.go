package main

import (
	"encoding/json"
	"slices"
	"sort"
)

// Where an acted entity sits, for its consumer scan (MAR-142). Home
// Assistant indexes a trigger that targets an area, floor, or label under
// that area, floor, or label in search/related, never under the entities it
// covers, so the scan reads those too. A target covers the entities in an
// area (their own area, else their device's, else its parent's), on a floor's areas, on a
// device or its parent device, and with a label on the entity, its device or
// that device's parent, or its area (helpers/target.py). An exclude block
// names the same keys: it counts as a match, since Home Assistant leaves the
// excluded ones out only at run time.

type placeRow struct {
	ID      string   `json:"id"`
	AreaID  string   `json:"area_id"`
	FloorID string   `json:"floor_id"`
	Labels  []string `json:"labels"`
}

// placeRows reads the device or area registry once per run, by id; nil when
// it is unreadable.
func (e *tierEngine) placeRows(kind string) map[string]placeRow {
	if rows, read := e.places[kind]; read {
		return rows
	}
	var list []placeRow
	var rows map[string]placeRow
	if e.s.wsDecode(map[string]any{"type": "config/" + kind + "_registry/list"}, &list, "the "+kind+" registry") == nil {
		rows = map[string]placeRow{}
		for _, row := range list {
			if kind == "area" {
				row.ID = row.AreaID
			}
			rows[row.ID] = row
		}
	}
	e.places[kind] = rows
	return rows
}

// relatedItems: the search/related reads of an entity's consumer scan: the
// entity and its device first (direct of them), then where it sits: the
// device's parent, the entity's area, that area's floor, and the labels of
// the entity, its device and that device's parent, and its area. ok is false
// when a registry it needs is unreadable (the scan is then typed, never
// clean). A disabled entity, which the compact registry leaves out, sits
// where the full registry says.
func (e *tierEngine) relatedItems(id string) (items [][2]string, direct int, ok bool) {
	items = [][2]string{{"entity", id}}
	entry, listed := e.reg[id]
	area, labels := entry.ai, slices.Clone(entry.lb)
	if !listed && e.fullRead {
		for _, row := range e.full {
			if row.EntityID == id {
				area, labels = row.AreaID, slices.Clone(row.Labels)
			}
		}
	}
	var places [][2]string
	if device := e.deviceOf(id); device != "" {
		items = append(items, [2]string{"device", device})
		devices := e.placeRows("device")
		if devices == nil {
			return items, len(items), false
		}
		labels = append(labels, devices[device].Labels...)
		if parent := e.deviceGraph().parent[device]; parent != "" { // a device or label target covers its child devices (2026.9+)
			places = append(places, [2]string{"device", parent})
			labels = append(labels, devices[parent].Labels...)
		}
		for cur, seen := device, map[string]bool{}; area == "" && cur != "" && !seen[cur]; cur = e.deviceGraph().parent[cur] {
			seen[cur], area = true, devices[cur].AreaID // a child device without an area sits in its parent's
		}
	}
	direct = len(items)
	if area != "" {
		places = append(places, [2]string{"area", area})
		areas := e.placeRows("area")
		if areas == nil {
			return append(items, places...), direct, false
		}
		if floor := areas[area].FloorID; floor != "" {
			places = append(places, [2]string{"floor", floor})
		}
		labels = append(labels, areas[area].Labels...)
	}
	sort.Strings(labels)
	for _, label := range slices.Compact(labels) {
		places = append(places, [2]string{"label", label})
	}
	return append(items, places...), direct, true
}

// targetsPlace: whether a trigger of cfg targets one of places (the ids of
// relatedItems beyond its direct ones), or targets something by template,
// which can't be ruled out. An automation found only where the entity sits
// is a consumer only then: one found there for what its actions target is
// not.
func targetsPlace(cfg map[string]any, places []string) bool {
	for _, trigger := range flattenTriggers(firstPresent(cfg, "triggers", "trigger")) {
		target, present := trigger["target"]
		if !present {
			continue
		}
		m, isMap := target.(map[string]any)
		if raw, _ := json.Marshal(target); !isMap || isTemplated(string(raw)) {
			return true
		}
		for _, key := range []string{"device_id", "area_id", "floor_id", "label_id"} {
			if slices.ContainsFunc(literalIDs(m[key]), func(id string) bool { return slices.Contains(places, id) }) {
				return true
			}
		}
	}
	return false
}
