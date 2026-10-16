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
	if found := e.deviceAccessEntity(e.reg[id].di); found != "" {
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
// user-authored anyway). The caller skips access-capable targets.
func (e *tierEngine) accessSibling(id, via, service string, data map[string]any) tierLevel {
	domain, name := splitID(service)
	opens := (service == "button.press" && !e.reg[id].categorized) || service == "switch.turn_on" || service == "switch.toggle" ||
		(domain == "cover" && coverCanOpen(name, data))
	if !opens {
		return tierOrdinary
	}
	found := e.deviceAccessEntity(e.reg[id].di)
	if found == "" {
		return tierOrdinary
	}
	e.raise(tierTyped, tierReason{Code: reasonDeviceAccessSibling, EntityID: id, Via: via, Detail: found})
	return tierTyped
}

// deviceAccessEntity returns the first access-capable entity on one device
// (never its parent or children: a hub links to every node), disabled ones
// included (the hardware still opens), or "". Only covers need a state; an
// unread or absent one has no class and counts (fail closed).
func (e *tierEngine) deviceAccessEntity(device string) string {
	if device == "" {
		return ""
	}
	ids := e.deviceEntities(device, "homeassistant")
	rows, err := e.fullRegistry()
	if err != nil {
		e.readFailed("", "", "registry")
	}
	for _, row := range rows {
		if row.DeviceID == device && !slices.Contains(ids, row.EntityID) {
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

// deviceFamily is a targeted device plus its direct child devices, as Home
// Assistant expands a device target (helpers/target.py, since 2026.9). An
// unreadable device registry is typed; the device itself still counts.
func (e *tierEngine) deviceFamily(device string) []string {
	if e.children == nil {
		e.children = map[string][]string{}
		var rows []struct {
			ID     string `json:"id"`
			Parent string `json:"via_device_id"`
		}
		if err := e.s.wsDecode(map[string]any{"type": "config/device_registry/list"}, &rows, "the device registry"); err != nil {
			e.readFailed("", "", "device registry")
		}
		for _, row := range rows {
			if row.Parent != "" {
				e.children[row.Parent] = append(e.children[row.Parent], row.ID)
			}
		}
	}
	return append([]string{device}, e.children[device]...)
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
// on for a core service (helpers/target.py async_extract_referenced_entity_ids):
// no hidden or categorized entity and, for an area, none assigned to another
// area. An integration resolves its targets itself, so its list stays whole;
// an id without a registry row is kept: nothing proves it excluded.
func (e *tierEngine) primaryEntities(ids []string, area, domain string) []string {
	if !coreDomains[domain] {
		return ids
	}
	out := []string{}
	for _, id := range ids {
		row, registered := e.reg[id]
		if !registered || !(row.hidden || row.categorized || (area != "" && row.ai != "" && row.ai != area)) {
			out = append(out, id)
		}
	}
	return out
}
