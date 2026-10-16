package main

import (
	"slices"
	"sort"
)

// Device-level rules: what acting on one entity reaches through its device.

// deviceAccess: an integration service acts on the target's DEVICE (a Z-Wave
// node, a Nuki bridge), so any access-capable entity on that device makes it
// typed; otherwise it is at least preview, never below the targetless case.
func (e *tierEngine) deviceAccess(id, via, service string) tierLevel {
	if found := e.deviceAccessEntity(e.reg[id].di, false); found != "" {
		e.gap(reasonDeviceAccess, id, via, service+" on the device of "+found)
		return tierTyped
	}
	e.raise(tierPreview, tierReason{Code: reasonIntegrationSvc, EntityID: id, Via: via, Detail: service})
	return tierPreview
}

// accessSibling: a core-service call that can energize an opener — a press,
// switching on or toggling, opening a cover — on an entity whose device also
// carries a lock, alarm panel, or access cover is typed: that button or relay
// may be the opener (a gate relay, a buzzer). Every other call keeps its own
// tier, and so does a press on a config or diagnostic button (ping, identify,
// restart: the integration fixed it; ESPHome/MQTT ones are typed as
// user-authored anyway). A switch_as_x light, fan, … is that relay, so its
// turn_on/toggle counts as switching it on. The caller skips access-capable
// targets.
func (e *tierEngine) accessSibling(id, via, service string, data map[string]any) tierLevel {
	domain, name := splitID(service)
	opens := (service == "button.press" && !e.reg[id].categorized) || service == "switch.turn_on" || service == "switch.toggle" ||
		(domain == "cover" && coverCanOpen(name, data)) || (e.reg[id].pl == "switch_as_x" && (name == "turn_on" || name == "toggle"))
	if !opens {
		return tierOrdinary
	}
	found := e.deviceAccessEntity(e.reg[id].di, true)
	if found == "" {
		return tierOrdinary
	}
	e.raise(tierTyped, tierReason{Code: reasonDeviceAccessSibling, EntityID: id, Via: via, Detail: found})
	return tierTyped
}

// deviceAccessEntity returns the first access-capable entity on a device,
// disabled ones included (the hardware still opens), or "". With family it
// also searches the rest of the device's parent/child family — for the
// core-service sibling guard: a child device is part of its parent. An
// integration service acts on its target's own device only, and a
// via_device_id hub link never counts (a hub links to every node). Only
// covers need a state; an unread or absent one has no class and counts
// (fail closed).
func (e *tierEngine) deviceAccessEntity(device string, family bool) string {
	if device == "" {
		return ""
	}
	devices := []string{device}
	if family {
		g := e.deviceGraph()
		root := device
		if parent := g.parent[device]; parent != "" {
			root = parent
		}
		devices = append([]string{root}, g.children[root]...)
	}
	var ids []string
	for _, d := range devices {
		ids = append(ids, e.deviceEntities(d, "homeassistant")...)
	}
	rows, err := e.fullRegistry()
	if err != nil {
		e.readFailed("", "", "registry")
	}
	for _, row := range rows {
		if slices.Contains(devices, row.DeviceID) && !slices.Contains(ids, row.EntityID) {
			ids = append(ids, row.EntityID)
		}
	}
	sort.Strings(ids)
	covers := filterDomain(ids, "cover")
	e.prefetchStates(covers)
	for _, id := range ids {
		dc := ""
		if slices.Contains(covers, id) {
			st, _ := e.state(id)
			dc = st.str("device_class")
		}
		if accessCapable(id, dc) {
			return id
		}
	}
	return ""
}

// deviceGraph links devices as config/device_registry/list shows them (Home
// Assistant 2026.9+): a child row carries parent_device_id; via_device_id is
// a hub link, not a child. An unreadable registry is typed and links nothing.
type deviceGraph struct {
	read     bool
	known    map[string]bool
	children map[string][]string
	parent   map[string]string
	splits   map[string][]string // composite id → split device ids; nil until read
}

func (e *tierEngine) deviceGraph() *deviceGraph {
	if e.devices != nil {
		return e.devices
	}
	g := &deviceGraph{known: map[string]bool{}, children: map[string][]string{}, parent: map[string]string{}}
	e.devices = g
	var rows []struct {
		ID     string `json:"id"`
		Parent string `json:"parent_device_id"`
	}
	if err := e.s.wsDecode(map[string]any{"type": "config/device_registry/list"}, &rows, "the device registry"); err != nil {
		e.readFailed("", "", "device registry")
		return g
	}
	g.read = true
	for _, row := range rows {
		g.known[row.ID] = true
		if row.Parent != "" {
			g.children[row.Parent] = append(g.children[row.Parent], row.ID)
			g.parent[row.ID] = row.Parent
		}
	}
	return g
}

// deviceFamily is what a device target reaches, as Home Assistant expands it
// (helpers/target.py): the device plus its direct child devices (since
// 2026.9); a pre-2026.8 merged device id no longer listed resolves to the
// devices it was split into, each with its children.
func (e *tierEngine) deviceFamily(device string) []string {
	g := e.deviceGraph()
	roots := []string{device}
	if g.read && !g.known[device] {
		if splits := e.compositeSplits()[device]; len(splits) > 0 {
			roots = splits
		}
	}
	var out []string
	for _, root := range roots {
		out = append(out, root)
		out = append(out, g.children[root]...)
	}
	return out
}

// compositeSplits reads the merged-device splits once; a core without the
// command (before 2026.8) has none, any other failure is typed.
func (e *tierEngine) compositeSplits() map[string][]string {
	g := e.deviceGraph()
	if g.splits != nil {
		return g.splits
	}
	g.splits = map[string][]string{}
	var rows map[string]struct {
		SplitIDs []string `json:"split_ids"`
	}
	err := e.s.wsDecode(map[string]any{"type": "config/device_registry/list_composite_splits"}, &rows, "the device splits")
	switch {
	case isUpstreamUnknownCommand(err):
	case err != nil:
		e.readFailed("", "", "device splits")
	}
	for id, row := range rows {
		g.splits[id] = row.SplitIDs
	}
	return g.splits
}

// familyEntities: the entities a device target reaches, children included.
func (e *tierEngine) familyEntities(device, domain string) []string {
	var ids []string
	for _, d := range e.deviceFamily(device) {
		ids = append(ids, e.deviceEntities(d, domain)...)
	}
	sort.Strings(ids)
	return slices.Compact(ids)
}

// primaryEntities keeps what Home Assistant's own area/device targeting acts
// on for a core service (helpers/target.py async_extract_referenced_entity_ids).
func (e *tierEngine) primaryEntities(ids []string, area, domain string) []string {
	out := []string{}
	for _, id := range ids {
		if e.skipWhy(id, area, domain) == "" {
			out = append(out, id)
		}
	}
	return out
}

// skipWhy names why that targeting skips id ("" keeps it): hidden, an entity
// category, or for an area an assignment to another area. An integration
// resolves its targets itself, so its list stays whole; an id without a
// registry row is kept: nothing proves it excluded.
func (e *tierEngine) skipWhy(id, area, domain string) string {
	row, registered := e.reg[id]
	switch {
	case !coreDomains[domain] || !registered:
		return ""
	case row.hidden:
		return "hidden"
	case row.categorized:
		return "entity_category"
	case area != "" && row.ai != "" && row.ai != area:
		return "other_area"
	}
	return ""
}
