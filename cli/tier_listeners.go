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

// Event-listener consumers (typed-verbs spec §2.2 step 5b): search/related
// does not index bus listeners, so live counts are compared with the event
// triggers of every readable automation.

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
		enumerated := table.counts[t] + baselineFor(t)
		switch detail := fmt.Sprintf("%s: %d live, %d enumerated", t, live[t], table.counts[t]); {
		case frontendListenedEvents[t] != "" && live[t] > enumerated:
			// An open logbook stream or a raw event subscription listens here
			// too: not an automation, not enumerable — preview, like Q2
			// (maintainer default 2026-10-14). Attributed automations still
			// classify below and keep their own (typed) class.
			e.complete = false
			e.raise(tierPreview, tierReason{Code: frontendListenedEvents[t], Detail: detail})
		case enumerated != live[t]:
			e.gap(reasonListener, entityID, "", detail)
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
		e.readFailed("", "", "automation scan")
		return nil
	}
	type autoRow struct {
		entityID, configID string
		raw                json.RawMessage
		err                error
		loaded             bool
	}
	var rows []*autoRow
	for _, st := range states {
		if strings.HasPrefix(st.EntityID, "automation.") && st.State == "on" { // off automations hold no listeners
			rows = append(rows, &autoRow{entityID: st.EntityID, configID: st.Attributes.ID})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].entityID < rows[j].entityID })
	if len(rows) > tierMaxAutomations {
		e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d automations", tierMaxAutomations))
		return nil
	}
	var wg sync.WaitGroup
	for _, row := range rows {
		wg.Add(1)
		go func(row *autoRow) {
			defer wg.Done()
			row.err = &verbFailure{code: "CONFIG_NOT_FOUND"}
			if row.configID != "" {
				row.raw, row.err = readStoredConfig(e.s, "automation", row.configID)
			}
			var notFound *verbFailure
			if errors.As(row.err, &notFound) { // YAML/package: read what Home Assistant loaded
				if loaded, ok := loadedConfig(e.s, row.entityID, "automation"); ok {
					row.raw, row.err, row.loaded = loaded, nil, true
				}
			}
		}(row)
	}
	wg.Wait()
	table := &listenerTable{counts: map[string]int{}, hits: map[string][]listenerHit{}}
	for _, row := range rows {
		// An enabled automation this scan cannot read may hold exactly the
		// listener the live count shows: unread is typed, never "unexplained".
		var notFound *verbFailure
		switch {
		case errors.As(row.err, &notFound) && row.configID == "":
			e.gap(reasonYAMLNoID, row.entityID, "listener scan", "")
			continue
		case errors.As(row.err, &notFound):
			e.gap(reasonUnreadConfig, row.entityID, "listener scan", row.configID)
			continue
		}
		if row.err != nil {
			e.readFailed(row.entityID, "", "config")
			return nil
		}
		cfg, cached := e.configs[row.entityID]
		if !cached {
			var ok bool
			key := row.configID
			if row.loaded {
				key = loadedConfigKey(row.entityID)
			}
			if cfg, ok = e.parseConfig(row.entityID, "automation", key, row.raw, "listener scan"); !ok {
				return nil
			}
		} else if cfg == nil {
			return nil // already flagged by an earlier read of this automation
		}
		for _, trigger := range flattenTriggers(firstPresent(cfg, "triggers", "trigger")) {
			if firstPresent(trigger, "trigger", "platform") != "event" {
				continue
			}
			filter, _ := trigger["event_data"].(map[string]any)
			entityFilter, _ := filter["entity_id"].(string)
			if isTemplated(entityFilter) {
				entityFilter = "" // a runtime filter may match anything: count it as matching
			}
			types, isList := trigger["event_type"].([]any)
			if !isList {
				types = []any{trigger["event_type"]}
			}
			for _, t := range types {
				name, isString := t.(string)
				if !isString || name == "" || isTemplated(name) {
					e.gap(reasonListener, row.entityID, "listener scan", "templated event_type")
					continue
				}
				table.counts[name]++
				table.hits[name] = append(table.hits[name], listenerHit{automation: row.entityID, entityFilter: entityFilter})
			}
		}
	}
	e.listeners = table
	return table
}
