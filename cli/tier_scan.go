package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Consumer scan (typed-verbs spec §2.2 step 5): relation consumers, event
// listeners, consumer managers. A failed or cut scan is typed, never clean.

// fullRegistryRow: config/entity_registry/list carries no device_class and
// no aliases; device classes always come from state attributes.
type fullRegistryRow struct {
	EntityID string `json:"entity_id"`
	Platform string `json:"platform"`
	UniqueID string `json:"unique_id"`
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
	return filterDomain(related["entity"], domain)
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
// entity or its device whose triggers reference it are read and classified.
func (e *tierEngine) scanConsumers(id string, depth int) {
	for _, acted := range e.acted {
		if acted == id {
			return
		}
	}
	e.acted = append(e.acted, id)
	if len(e.acted) > tierMaxActed {
		e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d acted entities", tierMaxActed))
		return
	}
	needles := []string{id}
	items := [][2]string{{"entity", id}}
	if device := e.reg[id].di; device != "" {
		needles = append(needles, device)
		items = append(items, [2]string{"device", device})
	}
	var reads []func()
	for _, item := range items {
		reads = append(reads, func() { _, _ = e.s.ws(e.relatedRequest(item[0], item[1])) })
	}
	prefetch(reads)
	seen := map[string]bool{}
	var automations []string
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
	}
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
		e.consumer(automation, "trigger", depth, func(cfg map[string]any) bool { return triggersReference(cfg, needles) })
	}
}

// triggersReference: a trigger naming the entity or its device, or one
// targeted by a selector this scan does not resolve (area, floor, label,
// device, or a templated target) — counted as a match, never dropped.
func triggersReference(cfg map[string]any, needles []string) bool {
	for _, trigger := range flattenTriggers(firstPresent(cfg, "triggers", "trigger")) {
		if target, present := trigger["target"]; present {
			if m, isMap := target.(map[string]any); !isMap || m["area_id"] != nil || m["floor_id"] != nil || m["label_id"] != nil || m["device_id"] != nil {
				return true
			}
		}
		raw, _ := json.Marshal(trigger)
		for _, needle := range needles {
			if strings.Contains(string(raw), needle) {
				return true
			}
		}
	}
	return false
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
// its class is the maximum it reaches (Q1: fully read + ordinary stays ordinary).
func (e *tierEngine) consumer(automation, match string, depth int, matches func(map[string]any) bool) {
	key := "consumer " + automation
	if e.visited[key] {
		e.lift(e.done[key])
		return
	}
	if _, cached := e.configs[automation]; !cached {
		if e.reads++; e.reads > tierMaxConsumers {
			e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d consumer configs", tierMaxConsumers))
			return
		}
	}
	cfg, ok := e.runConfig(automation, "automation", "consumer")
	if !ok || (matches != nil && !matches(cfg)) {
		return
	}
	e.visited[key] = true
	class := tierOrdinary
	e.scopes = append(e.scopes, &class)
	e.walk(automationActions(cfg), automation+"/actions", depth+1)
	e.scopes = e.scopes[:len(e.scopes)-1]
	e.done[key] = class
	e.consumers = append(e.consumers, tierConsumer{Automation: automation, Match: match, Class: class.String()})
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
