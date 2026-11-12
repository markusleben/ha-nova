package main

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Raw writes the skills marked typed and the CLI now rates itself
// (docs/work/2026-11-12-raw-typed-parity-spec.md), so plan raw, relay, and
// the App's gate agree without the plan file's mark: a rename that takes
// over an id hands that id's automations and scripts to this entity, and an
// area or label carried onto an entity or device widens the automations
// whose triggers target that place. Each is walked as a consumer; a read
// that fails is typed.

// rawTakeover walks what search/related lists for the id a registry rename
// takes (new_entity_id): every automation and script that names it, also
// through a group or person that holds it, whatever their triggers.
func (e *tierEngine) rawTakeover(endpoint string, payload map[string]any) {
	id := rawEntityID(payload["new_entity_id"])
	if endpoint != "ws" || payload["type"] != "config/entity_registry/update" || id == "" {
		return
	}
	related, err := e.related("entity", id)
	if err != nil {
		e.readFailed(id, "", "related")
		return
	}
	automations, scripts := related["automation"], related["script"]
	holders, seen := append(slices.Clone(related["group"]), related["person"]...), map[string]bool{id: true}
	for len(holders) > 0 { // a group in a group, or a person through a group, holds it too
		holder := holders[0]
		if holders = holders[1:]; seen[holder] {
			continue
		}
		seen[holder] = true
		if e.reads++; e.reads > tierMaxConsumers {
			e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d consumer configs", tierMaxConsumers))
			return
		}
		up, err := e.related("entity", holder)
		if err != nil {
			e.readFailed(holder, "", "related")
			return
		}
		automations, scripts = append(automations, up["automation"]...), append(scripts, up["script"]...)
		holders = append(append(holders, up["group"]...), up["person"]...)
	}
	sort.Strings(automations)
	for _, automation := range slices.Compact(automations) {
		e.consumer(automation, "", id, 0, func(map[string]any) string { return firesMay })
	}
	sort.Strings(scripts)
	for _, script := range slices.Compact(scripts) {
		if e.visited["takeover "+script] {
			continue
		}
		if e.reads++; e.reads > tierMaxConsumers {
			e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d consumer configs", tierMaxConsumers))
			return
		}
		e.visited["takeover "+script] = true
		e.expandRun(script, "script", 1)
	}
}

// rawCarry walks the automations whose triggers target a place a registry
// update newly puts something in: for an entity or device, the area it lands
// in (an empty own area is its device's, then its parent device's), that
// area's floor and labels, and its labels; for an area, its floor and
// labels. A place it is already in widens nothing; one it can't tell is
// walked.
func (e *tierEngine) rawCarry(endpoint string, payload map[string]any) {
	wsType, _ := payload["type"].(string)
	_, moves := payload["area_id"]
	_, labeled := payload["labels"]
	_, floored := payload["floor_id"]
	text := func(v any) string { s, _ := v.(string); return s }
	texts := func(v any) []string {
		var out []string
		list, _ := v.([]any)
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	var subject string
	switch {
	case endpoint != "ws":
		return
	case wsType == "config/entity_registry/update" && (moves || labeled):
		subject = rawEntityID(payload["entity_id"])
	case wsType == "config/device_registry/update" && (moves || labeled):
		subject = text(payload["device_id"])
	case wsType == "config/area_registry/update" && (floored || labeled):
		subject = text(payload["area_id"])
	default:
		return
	}
	areas, devices := e.placeRows("area"), e.placeRows("device")
	if areas == nil || devices == nil {
		e.readFailed(subject, "", "registry")
		return
	}
	in := func(area string, labels []string) map[[2]string]bool { // the places something there is in
		places := map[[2]string]bool{}
		if area != "" {
			places[[2]string{"area", area}] = true
			if floor := areas[area].FloorID; floor != "" {
				places[[2]string{"floor", floor}] = true
			}
			for _, label := range areas[area].Labels {
				places[[2]string{"label", label}] = true
			}
		}
		for _, label := range labels {
			places[[2]string{"label", label}] = true
		}
		return places
	}
	inherited := func(device string) string {
		for seen := map[string]bool{}; device != "" && !seen[device]; device = e.deviceGraph().parent[device] {
			if seen[device] = true; devices[device].AreaID != "" {
				return devices[device].AreaID
			}
		}
		return ""
	}
	var before, after map[[2]string]bool
	switch wsType {
	case "config/area_registry/update":
		now := areas[subject]
		floor, labels := now.FloorID, now.Labels
		if floored {
			floor = text(payload["floor_id"])
		}
		if labeled {
			labels = texts(payload["labels"])
		}
		before, after = in("", now.Labels), in("", labels)
		if now.FloorID != "" {
			before[[2]string{"floor", now.FloorID}] = true
		}
		if floor != "" {
			after[[2]string{"floor", floor}] = true
		}
	default:
		own, labels, up, known := "", []string(nil), "", false
		if wsType == "config/entity_registry/update" {
			row, listed := e.reg[subject]
			own, labels, up, known = row.ai, row.lb, e.deviceOf(subject), listed
		} else {
			row, listed := devices[subject]
			own, labels, up, known = row.AreaID, row.Labels, e.deviceGraph().parent[subject], listed
		}
		if known { // a disabled entity, which the compact registry leaves out, is walked in full
			lands := own
			if lands == "" {
				lands = inherited(up)
			}
			before = in(lands, labels)
		}
		if moves {
			own = text(payload["area_id"])
		}
		if labeled {
			labels = texts(payload["labels"])
		}
		if own == "" {
			own = inherited(up)
		}
		after = in(own, labels)
	}
	var places [][2]string
	for place := range after {
		if !before[place] {
			places = append(places, place)
		}
	}
	slices.SortFunc(places, func(a, b [2]string) int { return strings.Compare(a[0]+" "+a[1], b[0]+" "+b[1]) })
	var automations []string
	for _, place := range places {
		related, err := e.related(place[0], place[1])
		if err != nil {
			e.readFailed(subject, "", "related")
			return
		}
		automations = append(automations, related["automation"]...)
	}
	sort.Strings(automations)
	for _, automation := range slices.Compact(automations) {
		if cfg := e.warmConfig(automation); cfg != nil && !targetsPlace(cfg, places) {
			continue // there for what its actions target, or for another entity: no consumer
		}
		e.consumer(automation, "", subject, 0, func(cfg map[string]any) string {
			if !targetsPlace(cfg, places) {
				return ""
			}
			return firesMay
		})
	}
}

// A repair fix flow (its start and each step) and a subentry flow's step or
// reconfigure start: what they change is the integration's own code, and
// the gate keeps no state to tell a flow id's issue or kind
// (docs/work/2026-11-12-raw-typed-parity-spec.md).
var (
	rawRepairFlowPath   = regexp.MustCompile(`^/api/repairs/issues/fix(/[A-Za-z0-9_-]+)?$`)
	rawSubentryStepPath = regexp.MustCompile(`^/api/config/config_entries/subentries/flow/[A-Za-z0-9_-]+$`)
)
