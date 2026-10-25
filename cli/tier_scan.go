package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
)

// Consumer scan (typed-verbs spec §2.2 step 5): relation consumers, event
// listeners, consumer managers. A failed or cut scan is typed, never clean.

// fullRegistryRow: config/entity_registry/list carries no device_class and
// no aliases; device classes always come from state attributes.
type fullRegistryRow struct {
	EntityID      string `json:"entity_id"`
	Platform      string `json:"platform"`
	UniqueID      string `json:"unique_id"`
	ConfigEntryID string `json:"config_entry_id"`
	DeviceID      string `json:"device_id"`
	DisabledBy    string `json:"disabled_by"`
}

func (e *tierEngine) fullRegistry() ([]fullRegistryRow, error) {
	if !e.fullRead {
		if err := e.s.wsDecode(map[string]any{"type": "config/entity_registry/list"}, &e.full, "the entity registry"); err != nil {
			return nil, err
		}
		e.fullRead = true
	}
	return e.full, nil
}

// related reads search/related; every list comes back sorted (HA builds
// them from sets).
func (e *tierEngine) related(itemType, itemID string) (map[string][]string, error) {
	var out map[string][]string
	err := e.s.wsDecode(e.relatedRequest(itemType, itemID), &out, "search/related for "+itemID)
	for _, list := range out {
		sort.Strings(list)
	}
	return out, err
}

func (e *tierEngine) relatedRequest(itemType, itemID string) map[string]any {
	return map[string]any{"type": "search/related", "item_type": itemType, "item_id": itemID}
}

// areaEntities expands a stored area_id for classification only.
func (e *tierEngine) areaEntities(area, domain, via string) []string {
	related, err := e.related("area", area)
	if err != nil {
		e.readFailed("", via, "related area")
		return nil
	}
	return e.primaryEntities(filterDomain(related["entity"], domain), area, domain)
}

// deviceEntities filters the compact registry by device_id only — never
// the device's area (membership-resolution.md → Devices).
func (e *tierEngine) deviceEntities(device, domain string) []string {
	var ids []string
	for id, row := range e.reg {
		if row.di == device {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return filterDomain(ids, domain)
}

// filterDomain keeps the service domain's entities; a generic or an
// integration service may act on any of them, so it keeps all.
func filterDomain(ids []string, domain string) []string {
	out := []string{}
	for _, id := range ids {
		if domain == "homeassistant" || !coreDomains[domain] || strings.HasPrefix(id, domain+".") {
			out = append(out, id)
		}
	}
	return out
}

// scanConsumers runs once per acted entity: automations related to the
// entity or its device whose triggers reference it are read and classified;
// d (the entity's planned change) only words the consumer list.
func (e *tierEngine) scanConsumers(id string, depth int, d stateDelta) {
	needles := []string{id}
	items := [][2]string{{"entity", id}}
	if device := e.reg[id].di; device != "" {
		needles = append(needles, device)
		items = append(items, [2]string{"device", device})
	}
	if slices.Contains(e.acted, id) { // scanned already: this change may fire what the first did not
		for automation, ons := range e.firedOn {
			if cfg := e.configs[automation]; cfg != nil && slices.ContainsFunc(ons, func(f fired) bool { return f.on == id }) {
				e.matchConsumer(automation, triggerMatch(cfg, id, needles, d), id)
			}
		}
		return
	}
	e.acted = append(e.acted, id)
	if len(e.acted) > tierMaxActed {
		e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d acted entities", tierMaxActed))
		return
	}
	var reads []func()
	for _, item := range items {
		reads = append(reads, func() { _, _ = e.s.ws(e.relatedRequest(item[0], item[1])) })
	}
	prefetch(reads)
	seen := map[string]bool{}
	var automations, containers []string
	for _, item := range items {
		related, err := e.related(item[0], item[1])
		if err != nil {
			e.readFailed(id, "", "related")
			return
		}
		for _, automation := range related["automation"] {
			if !seen[automation] {
				seen[automation] = true
				automations = append(automations, automation)
			}
		}
		if item[0] == "entity" {
			containers = append(containers, related["group"]...)
		}
	}
	// A group's state follows its members: its consumers react to this
	// entity too. Containers come from search/related and from the group
	// states this run already read (bounded by the acted-entity budget).
	for group, st := range e.states {
		if groupDomain, _ := splitID(group); slices.Contains(groupMembers(groupDomain, st), id) {
			containers = append(containers, group)
		}
	}
	sort.Strings(containers)
	sort.Strings(automations)
	e.prefetchStates(automations)
	var configReads []func()
	for _, automation := range automations {
		if st, err := e.state(automation); err == nil && st.str("id") != "" && len(configReads) < tierMaxConsumers-e.reads {
			configID := st.str("id")
			configReads = append(configReads, func() { _, _ = readStoredConfig(e.s, "automation", configID) })
		}
	}
	prefetch(configReads)
	for _, automation := range automations {
		e.consumer(automation, "", id, depth, func(cfg map[string]any) string { return triggerMatch(cfg, id, needles, d) })
	}
	for _, group := range slices.Compact(containers) {
		e.scanConsumers(group, depth, stateDelta{}) // a group's own change is not derived
	}
}

// flattenTriggers returns the enabled trigger maps, descending into nested
// {triggers: [...]} lists.
func flattenTriggers(node any) []map[string]any {
	var out []map[string]any
	switch n := node.(type) {
	case []any:
		for _, child := range n {
			out = append(out, flattenTriggers(child)...)
		}
	case map[string]any:
		if n["enabled"] == false {
			return nil
		}
		if nested, present := n["triggers"]; present {
			return flattenTriggers(nested)
		}
		out = append(out, n)
	}
	return out
}

// consumer classifies one automation's actions as a consumer of the run;
// its class is the maximum it reaches (Q1: fully read + ordinary stays
// ordinary). fires (nil for event listeners, which keep match) returns ""
// for no reference to on, else the trigger verdict that becomes match.
func (e *tierEngine) consumer(automation, match, on string, depth int, fires func(map[string]any) string) {
	key := "consumer " + automation
	if e.visited[key] {
		e.lift(e.done[key])
		if cfg := e.configs[automation]; cfg != nil && fires != nil {
			match = fires(cfg)
		}
		e.matchConsumer(automation, match, on) // an event listener keeps its own match
		return
	}
	if _, cached := e.configs[automation]; !cached {
		if e.reads++; e.reads > tierMaxConsumers {
			e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d consumer configs", tierMaxConsumers))
			return
		}
	}
	cfg, ok := e.runConfig(automation, "automation", "consumer")
	if ok && fires != nil {
		match = fires(cfg)
	}
	if !ok || match == "" {
		return
	}
	e.visited[key] = true
	for _, trigger := range flattenTriggers(firstPresent(cfg, "triggers", "trigger")) {
		if firstPresent(trigger, "trigger", "platform") == "template" {
			e.note(noteTemplates) // its dependencies are computed at runtime
		}
	}
	row := len(e.consumers) // listed before the walk, so a nested repeat can upgrade its match
	e.consumers = append(e.consumers, tierConsumer{Automation: automation})
	e.matchConsumer(automation, match, on)
	class := tierOrdinary
	e.scopes = append(e.scopes, &class)
	e.walk(automationActions(cfg), automation+"/actions", depth+1)
	e.checkEvents("automation_triggered", automation) // a consumer run fires one too
	e.scopes = e.scopes[:len(e.scopes)-1]
	e.done[key] = class
	e.consumers[row].Class = class.String()
}

func firstPresent(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

// liveEvents reads GET /api/events once: listener counts per event type.
func (e *tierEngine) liveEvents() map[string]int {
	if e.events != nil {
		return e.events
	}
	e.events = map[string]int{}
	status, body, err := e.s.coreGET("/api/events")
	var rows []struct {
		Event         string `json:"event"`
		ListenerCount int    `json:"listener_count"`
	}
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/events", status)
	}
	if err == nil {
		err = json.Unmarshal(body, &rows)
	}
	if err != nil {
		e.readFailed("", "", "event listeners")
		return e.events
	}
	for _, row := range rows {
		e.events[row.Event] = row.ListenerCount
	}
	return e.events
}

// checkManagers detects local consumer managers per run (Q2: present →
// preview). Absence is a note: an external one leaves no local entry.
func (e *tierEngine) checkManagers() {
	var entries []struct {
		Domain string `json:"domain"`
	}
	err := e.s.wsDecode(map[string]any{"type": "config_entries/get"}, &entries, "the config entries")
	rows, regErr := e.fullRegistry()
	if err == nil {
		err = regErr
	}
	if err != nil {
		e.readFailed("", "", "consumer managers")
		return
	}
	var present []string
	for _, entry := range entries {
		if managerEntryDomains[entry.Domain] {
			present = append(present, entry.Domain)
		}
	}
	for _, row := range rows {
		slug, isApp := strings.CutSuffix(row.UniqueID, "_version_latest")
		for _, known := range managerAppSlugs {
			if isApp && row.Platform == "hassio" && (slug == known || strings.HasSuffix(slug, "_"+known)) {
				present = append(present, slug)
			}
		}
	}
	if len(present) == 0 {
		e.notes = append(e.notes, noteNoManager)
		return
	}
	e.complete = false
	e.raise(tierPreview, tierReason{Code: reasonManager, Detail: strings.Join(present, ", ")})
}

// switchAsX: a switch wrapped by an access-capable switch_as_x entity (same
// device, or both device-less and so not attributable) actuates that lock or
// cover; the switch inherits its class.
func (e *tierEngine) switchAsX(id, via string) tierLevel {
	var wrappers []string
	for other, row := range e.reg {
		if row.pl == "switch_as_x" && row.di == e.reg[id].di {
			wrappers = append(wrappers, other)
		}
	}
	sort.Strings(wrappers)
	e.prefetchStates(wrappers)
	for _, wrapper := range wrappers {
		st, err := e.state(wrapper)
		if err != nil {
			e.readFailed(wrapper, via, "state")
			return tierTyped
		}
		if dc := st.str("device_class"); accessCapable(wrapper, dc) {
			e.gap(reasonSwitchAsX, id, via, wrapper)
			return tierTyped
		}
	}
	return tierOrdinary
}

// readFailed records a failed read as typed: LIMIT_CUT once the 30 s budget
// is spent, SCAN_FAILED otherwise. Details are stable words, never error
// text, so the plan hash stays deterministic.
func (e *tierEngine) readFailed(entityID, via, what string) {
	if e.s.ctx.Err() != nil {
		e.gap(reasonLimitCut, "", "", "read budget")
		return
	}
	e.gap(reasonScanFailed, entityID, via, what)
}
