package main

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
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
	for _, holder := range append(slices.Clone(related["group"]), related["person"]...) {
		up, err := e.related("entity", holder)
		if err != nil {
			e.readFailed(holder, "", "related")
			return
		}
		automations, scripts = append(automations, up["automation"]...), append(scripts, up["script"]...)
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

// rawCarry walks the automations whose triggers target a place an entity or
// device registry update moves it to: the new area (an entity's cleared area
// is its device's), that area's floor and labels, and the labels it sets.
func (e *tierEngine) rawCarry(endpoint string, payload map[string]any) {
	wsType, _ := payload["type"].(string)
	area, moves := payload["area_id"]
	labels, labeled := payload["labels"].([]any)
	if endpoint != "ws" || wsType != "config/entity_registry/update" && wsType != "config/device_registry/update" || !moves && !labeled {
		return
	}
	subject, _ := payload["device_id"].(string)
	if wsType == "config/entity_registry/update" {
		subject = rawEntityID(payload["entity_id"])
	}
	var places [][2]string
	to, _ := area.(string)
	if moves && area == nil && wsType == "config/entity_registry/update" { // it sits where its device does
		devices := e.placeRows("device")
		if devices == nil {
			e.readFailed(subject, "", "registry")
			return
		}
		for cur, seen := e.deviceOf(subject), map[string]bool{}; to == "" && cur != "" && !seen[cur]; cur = e.deviceGraph().parent[cur] {
			seen[cur], to = true, devices[cur].AreaID
		}
	}
	if to != "" {
		areas := e.placeRows("area")
		if areas == nil {
			e.readFailed(subject, "", "registry")
			return
		}
		places = append(places, [2]string{"area", to})
		if floor := areas[to].FloorID; floor != "" {
			places = append(places, [2]string{"floor", floor})
		}
		for _, label := range areas[to].Labels {
			places = append(places, [2]string{"label", label})
		}
	}
	for _, label := range labels {
		if text, ok := label.(string); ok {
			places = append(places, [2]string{"label", text})
		}
	}
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
