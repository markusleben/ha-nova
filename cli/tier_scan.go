package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Consumer scan (typed-verbs spec §2.2 step 5): relation consumers, event
// listeners, consumer managers. A failed or cut scan is typed, never clean.

type fullRegistryRow struct {
	EntityID            string `json:"entity_id"`
	Platform            string `json:"platform"`
	UniqueID            string `json:"unique_id"`
	DeviceClass         string `json:"device_class"`
	OriginalDeviceClass string `json:"original_device_class"`
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

func (e *tierEngine) related(itemType, itemID string) (map[string][]string, error) {
	var out map[string][]string
	err := e.s.wsDecode(map[string]any{"type": "search/related", "item_type": itemType, "item_id": itemID}, &out, "search/related for "+itemID)
	return out, err
}

// areaEntities expands a stored area_id for classification only.
func (e *tierEngine) areaEntities(area, domain, via string) []string {
	related, err := e.related("area", area)
	if err != nil {
		e.gap(reasonScanFailed, "", via, err.Error())
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

func filterDomain(ids []string, domain string) []string {
	out := []string{}
	for _, id := range ids {
		if domain == "homeassistant" || strings.HasPrefix(id, domain+".") {
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
	var automations []string
	for _, item := range items {
		related, err := e.related(item[0], item[1])
		if err != nil {
			e.gap(reasonScanFailed, id, "", err.Error())
			return
		}
		automations = append(automations, related["automation"]...)
	}
	sort.Strings(automations)
	for _, automation := range automations {
		e.consumer(automation, "trigger", depth, func(cfg map[string]any) bool {
			triggers, _ := json.Marshal(firstPresent(cfg, "triggers", "trigger"))
			for _, needle := range needles {
				if strings.Contains(string(triggers), needle) {
					return true
				}
			}
			return false
		})
	}
}

// consumer classifies one automation's actions as a consumer of the run;
// its class is the maximum it reaches (Q1: fully read + ordinary stays ordinary).
func (e *tierEngine) consumer(automation, match string, depth int, matches func(map[string]any) bool) {
	if e.visited["consumer "+automation] {
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
	e.visited["consumer "+automation] = true
	class := tierOrdinary
	e.scopes = append(e.scopes, &class)
	e.walk(automationActions(cfg), automation+"/actions", depth+1)
	e.scopes = e.scopes[:len(e.scopes)-1]
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
		e.gap(reasonScanFailed, "", "", "event listeners: "+err.Error())
		return e.events
	}
	for _, row := range rows {
		e.events[row.Event] = row.ListenerCount
	}
	return e.events
}

type listenerHit struct{ automation, entityFilter string }

type listenerTable struct {
	counts map[string]int
	hits   map[string][]listenerHit
}

// checkEvents compares enumerated automation listeners with the live count
// for one event type (or every type under a "timer." prefix). Listeners
// that cannot be enumerated make the run typed (indirect-actuation.md).
func (e *tierEngine) checkEvents(eventType, entityID string) {
	live := e.liveEvents()
	matchesType := func(t string) bool {
		return t == eventType || (strings.HasSuffix(eventType, ".") && strings.HasPrefix(t, eventType))
	}
	surplus := false
	for t, n := range live {
		if matchesType(t) && n > baselineFor(t) {
			surplus = true
		}
	}
	if !surplus {
		return
	}
	table := e.listenerTable()
	if table == nil {
		return
	}
	types := map[string]bool{}
	for t := range live {
		types[t] = matchesType(t)
	}
	for t := range table.counts {
		types[t] = types[t] || matchesType(t)
	}
	for _, t := range sortedBoolKeys(types) {
		if table.counts[t]+baselineFor(t) != live[t] {
			e.gap(reasonListener, entityID, "", fmt.Sprintf("%s: %d live, %d enumerated", t, live[t], table.counts[t]))
		}
		for _, hit := range table.hits[t] {
			if entityID == "" || hit.entityFilter == "" || hit.entityFilter == entityID {
				e.consumer(hit.automation, "event:"+t, 1, nil)
			}
		}
	}
}

func baselineFor(eventType string) int {
	if eventType == "call_service" {
		return callServiceListenerBaseline
	}
	return 0
}

func sortedBoolKeys(m map[string]bool) []string {
	var keys []string
	for k, v := range m {
		if v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// listenerTable reads every automation config once (≤ 2,000, concurrently
// within the session's request slots) and counts the event-trigger
// listeners of the automations that are on.
func (e *tierEngine) listenerTable() *listenerTable {
	if e.listeners != nil {
		return e.listeners
	}
	status, body, err := e.s.coreGET("/api/states")
	var states []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			ID string `json:"id"`
		} `json:"attributes"`
	}
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("Home Assistant answered HTTP %d for /api/states", status)
	}
	if err == nil {
		err = json.Unmarshal(body, &states)
	}
	if err != nil {
		e.gap(reasonScanFailed, "", "", "automation scan: "+err.Error())
		return nil
	}
	type autoRow struct {
		entityID, configID string
		raw                json.RawMessage
		err                error
	}
	var rows []*autoRow
	for _, st := range states {
		if strings.HasPrefix(st.EntityID, "automation.") && st.State == "on" && st.Attributes.ID != "" {
			rows = append(rows, &autoRow{entityID: st.EntityID, configID: st.Attributes.ID})
		}
	}
	if len(rows) > tierMaxAutomations {
		e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d automations", tierMaxAutomations))
		return nil
	}
	var wg sync.WaitGroup
	for _, row := range rows {
		wg.Add(1)
		go func(row *autoRow) {
			defer wg.Done()
			row.raw, row.err = readStoredConfig(e.s, "automation", row.configID)
		}(row)
	}
	wg.Wait()
	table := &listenerTable{counts: map[string]int{}, hits: map[string][]listenerHit{}}
	for _, row := range rows {
		var notFound *verbFailure
		if errors.As(row.err, &notFound) {
			continue // id but no stored config (packages YAML): the count check covers it
		}
		if row.err != nil {
			e.gap(reasonScanFailed, row.entityID, "", row.err.Error())
			return nil
		}
		cfg, cached := e.configs[row.entityID]
		if !cached {
			var ok bool
			if cfg, ok = e.parseConfig(row.entityID, "automation", row.configID, row.raw, "listener scan"); !ok {
				return nil
			}
		} else if cfg == nil {
			return nil // already flagged by an earlier read of this automation
		}
		triggers, isList := firstPresent(cfg, "triggers", "trigger").([]any)
		if !isList {
			triggers = []any{firstPresent(cfg, "triggers", "trigger")}
		}
		for _, raw := range triggers {
			trigger, _ := raw.(map[string]any)
			if trigger == nil || trigger["enabled"] == false || firstPresent(trigger, "trigger", "platform") != "event" {
				continue
			}
			filter, _ := trigger["event_data"].(map[string]any)
			entityFilter, _ := filter["entity_id"].(string)
			types, isList := trigger["event_type"].([]any)
			if !isList {
				types = []any{trigger["event_type"]}
			}
			for _, t := range types {
				if name, isString := t.(string); isString && !isTemplated(name) {
					table.counts[name]++
					table.hits[name] = append(table.hits[name], listenerHit{automation: row.entityID, entityFilter: entityFilter})
				}
			}
		}
	}
	e.listeners = table
	return table
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
		e.gap(reasonScanFailed, "", "", "consumer managers: "+err.Error())
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
