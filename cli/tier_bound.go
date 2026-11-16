package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

// lateServices create entities (a reload is matched by name) or hold the run
// for a time their payload sets; what runs after them may meet new entities.
var lateServices = map[string]bool{"scene.create": true, "group.set": true, "remote.send_command": true, "remote.learn_command": true}

// scheduledTriggers fire at a time, never right after the call that set it.
var scheduledTriggers = map[string]bool{"time": true, "time_pattern": true, "calendar": true, "sun": true}

// lateTrigger: a consumer trigger with a for: anywhere in it (a state
// trigger's own, a purpose-specific trigger's options.for) fires only once a
// state has held; a scheduled one fires later.
func (e *tierEngine) lateTrigger(trigger map[string]any) {
	kind, _ := firstPresent(trigger, "trigger", "platform").(string)
	if raw, _ := json.Marshal(trigger); bytes.Contains(raw, []byte(`"for":`)) || scheduledTriggers[kind] {
		e.late = true
	}
}

// The MAR-209 bound (D6, docs/work/2026-11-16-confirmation-simplification-0.32-spec.md):
// a stored call whose target is a template is rated by every entity it could
// pick in this home, each visited as if the call named it, so the existing
// rules decide. It holds only for calls that run right after the plan's own
// call, so entities added later are never missed.

// boundCandidates lists the live states of the service's own core domain;
// nil keeps today's gap: a check that doesn't settle bounds (only plan call
// and the App's check of a service call do), a config draft or a reload's
// configs, an unknown payload or entity_id all, a service that takes no
// entity target, an empty domain, or a cascade whose calls already aren't
// bounded or run later.
func (e *tierEngine) boundCandidates(name string, dataKnown, all bool, via string) []string {
	domain, _ := splitID(name)
	if !e.bounding || e.draft || e.reloading || !dataKnown || all || e.late || e.calls.unknown ||
		!coreDomains[domain] || domain == "homeassistant" || !e.serviceSpec(name).target {
		return nil
	}
	if e.stateList == nil {
		e.useStateList()
	}
	states, err := e.stateList()
	if err != nil {
		e.readFailed("", via, "states")
		return nil
	}
	var ids []string
	for id := range states {
		if strings.HasPrefix(id, domain+".") {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// settleBounds runs after the whole cascade and its call_service listeners:
// a bound stands only while every call stays bounded and nothing runs later;
// otherwise each is the unresolved target it was, and its calls are unknown.
func (e *tierEngine) settleBounds() {
	for len(e.bounds) > 0 && (e.calls.unknown || e.late) {
		bounds := e.bounds
		e.bounds = nil
		e.calls.unknown = true
		for _, b := range bounds {
			e.gap(b.Code, "", b.Via, b.Detail)
		}
		e.callServiceListeners()
	}
}
