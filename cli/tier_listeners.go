package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Event-listener consumers (typed-verbs spec §2.2 step 5b): search/related
// does not index bus listeners, so live counts are compared with the event
// triggers of every readable automation.

// viaListenerScan marks what the scan of every automation's event triggers
// found; how far it gets depends on the read budget (plan_drift.go).
const viaListenerScan = "listener scan"

type listenerHit struct {
	automation, entityFilter string
	filter                   map[string]any // the trigger's event_data
	entity                   string         // the entity_id its event_data provably names (literalEntity)
	literal                  bool
}

type listenerTable struct {
	counts map[string]int
	hits   map[string][]listenerHit
	waits  map[string]bool // event types a running automation or script may wait on; "" for a templated one
}

// checkEvents compares enumerated automation listeners with the live count
// for one event type (or every type under a "timer." prefix). Listeners
// that cannot be enumerated make the run typed (indirect-actuation.md).
func (e *tierEngine) checkEvents(eventType, entityID string) {
	table, types := e.eventCounts(eventType, entityID)
	for _, t := range types {
		for _, hit := range table.hits[t] {
			if entityID == "" || hit.entityFilter == "" || hit.entityFilter == entityID {
				e.consumer(hit.automation, "event:"+t, "", 1, nil)
			}
		}
	}
}

// eventCounts compares the counts and answers the listener table and the
// matching event types; nil when no listener beyond the baseline listens.
func (e *tierEngine) eventCounts(eventType, entityID string) (*listenerTable, []string) {
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
		return nil, nil
	}
	if e.censusMoot { // the call asks anyway; as after a cut census, the person hears these listeners went unread
		e.gap(reasonListener, entityID, "", "listeners not read: the call asks anyway")
		return nil, nil
	}
	table := e.listenerTable()
	if table == nil {
		return nil, nil
	}
	types := map[string]bool{}
	for t := range live {
		types[t] = matchesType(t)
	}
	for t := range table.counts {
		types[t] = types[t] || matchesType(t)
	}
	matched := sortedBoolKeys(types)
	for _, t := range matched {
		enumerated := table.counts[t] + baselineFor(t)
		switch detail := fmt.Sprintf("%s: %d live, %d enumerated", t, live[t], table.counts[t]); {
		case frontendListenedEvents[t] && live[t] > enumerated && (table.waits[t] || table.waits[""]):
			// A running automation or script waits on this event: what it
			// does after the wait is outside the census, so every listener
			// counts, as before decision A.
			e.gap(reasonListener, entityID, "", detail+", a running wait_for_trigger listens")
		case frontendListenedEvents[t] && live[t] > enumerated:
			// An open logbook stream listens here too: a listener that
			// doesn't act, so a note, and the census stays complete, so each
			// automation's own filter decides (maintainer decision A,
			// 2026-11-18). Attributed automations still classify below.
			e.note(noteUnexplainedListener)
		case enumerated != live[t]:
			e.gap(reasonListener, entityID, "", detail)
		}
	}
	return table, matched
}

// callTargetTyped: a call whose own target classifies typed (an unlock, a
// garage opening) asks whatever else it sets off, so the call skips the
// listener census (one read per automation over Home Assistant Link) and
// says, as a cut census does, that those listeners went unread. Only call
// plans set it; a raw item keeps its own class (scopes).
func (e *tierEngine) callTargetTyped(req planCallRequest, targets []verbTarget) bool {
	for _, t := range targets {
		st := e.states[t.EntityID]
		if st == nil {
			continue
		}
		domain, _ := splitID(t.EntityID)
		if level, code := classifyOnEntity(t.EntityID, effectiveService(req.Service, domain), req.Data, st); level == tierTyped && code != reasonOwnedMember {
			return true
		}
	}
	return false
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
	rows, err := censusRows(status, body, err)
	if err != nil {
		e.readFailed("", "", "automation scan")
		return nil
	}
	if len(rows) > tierMaxAutomations {
		e.gap(reasonLimitCut, "", "", fmt.Sprintf("more than %d automations", tierMaxAutomations))
		return nil
	}
	e.s.censusReads(true)
	e.readCensus(rows, 0, false)
	e.s.censusReads(false)
	table := &listenerTable{counts: map[string]int{}, hits: map[string][]listenerHit{}, waits: map[string]bool{}}
	for _, row := range rows {
		domain, _ := splitID(row.entityID)
		// An enabled automation this scan cannot read may hold exactly the
		// listener the live count shows: unread is typed, never "unexplained".
		var notFound *verbFailure
		switch {
		case errors.As(row.err, &notFound) && domain == "script": // a running script it can't read may be waiting
			table.waits[""] = true
			continue
		case errors.As(row.err, &notFound) && row.configID == "":
			e.gap(reasonYAMLNoID, row.entityID, viaListenerScan, "")
			continue
		case errors.As(row.err, &notFound):
			e.gap(reasonUnreadConfig, row.entityID, viaListenerScan, row.configID)
			continue
		}
		if row.err != nil {
			e.readFailed(row.entityID, viaListenerScan, "config")
			return nil
		}
		cfg, cached := e.configs[row.entityID]
		if !cached {
			var ok bool
			key := row.configID // named as runConfig names it
			if key == "" {
				key = loadedConfigKey(row.entityID)
			}
			if cfg, ok = e.parseConfig(row.entityID, domain, key, row.raw, viaListenerScan); !ok {
				return nil
			}
		} else if cfg == nil {
			return nil // already flagged by an earlier read of this automation
		}
		if row.running {
			collectWaits(cfg, table.waits)
		}
		if row.waitsOnly {
			continue
		}
		for _, trigger := range flattenTriggers(firstPresent(cfg, "triggers", "trigger")) {
			if firstPresent(trigger, "trigger", "platform") != "event" {
				continue
			}
			filter, _ := rendered(trigger["event_data"]).(map[string]any)
			entity, literal := literalEntity(filter, cfg["trigger_variables"])
			entityFilter, _ := filter["entity_id"].(string)
			if isTemplated(entityFilter) {
				entityFilter = "" // a runtime filter may match anything: count it as matching
			}
			types, isList := rendered(trigger["event_type"]).([]any)
			if !isList {
				types = []any{rendered(trigger["event_type"])}
			}
			for _, t := range types {
				name, isString := t.(string)
				if !isString || name == "" || isTemplated(name) {
					e.gap(reasonListener, row.entityID, viaListenerScan, "templated event_type")
					continue
				}
				table.counts[name]++
				table.hits[name] = append(table.hits[name], listenerHit{automation: row.entityID, entityFilter: entityFilter, filter: filter, entity: entity, literal: literal})
			}
		}
	}
	e.listeners = table
	return table
}

// censusRow is one automation or script the census reads.
type censusRow struct {
	entityID, configID string
	running, waitsOnly bool
	raw                json.RawMessage
	err                error
}

// censusRows picks from GET /api/states what the census reads: every
// automation that is on or still running, and every running script.
func censusRows(status int, body json.RawMessage, err error) ([]*censusRow, error) {
	var states []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			ID      registryID `json:"id"`
			Current any        `json:"current"` // an automation's run count; any other entity's attribute may be anything
		} `json:"attributes"`
	}
	if err := stateListError(status, body, err); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &states); err != nil {
		return nil, err
	}
	var rows []*censusRow
	for _, st := range states {
		runs, _ := st.Attributes.Current.(float64)
		switch domain, _ := splitID(st.EntityID); {
		case domain == "automation" && (st.State == "on" || runs > 0): // an off automation's triggers hold no listeners, but a run it didn't stop may still wait
			rows = append(rows, &censusRow{entityID: st.EntityID, configID: string(st.Attributes.ID), running: runs > 0, waitsOnly: st.State != "on"})
		case domain == "script" && st.State == "on": // a script runs while it is on
			rows = append(rows, &censusRow{entityID: st.EntityID, running: true, waitsOnly: true})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].entityID < rows[j].entityID })
	return rows, nil
}

// readCensus reads each row's loaded config (in batches where the Relay
// takes them) and its blueprint substitution, concurrently within the
// session's request slots, or within inFlight of them (0: all). Session
// reads only: warmCensus runs it too.
func (e *tierEngine) readCensus(rows []*censusRow, inFlight int, warm bool) {
	requests := make([]map[string]any, len(rows))
	for i, row := range rows {
		domain, _ := splitID(row.entityID)
		requests[i] = loadedConfigRequest(row.entityID, domain)
	}
	e.s.readBatched(requests, inFlight) // over Link: a few round trips, not one per eight configs
	if inFlight <= 0 {
		inFlight = len(rows)
	}
	workers := make(chan struct{}, max(inFlight, 1))
	var wg sync.WaitGroup
	for _, row := range rows {
		wg.Add(1)
		go func(row *censusRow) {
			defer wg.Done()
			workers <- struct{}{}
			defer func() { <-workers }()
			domain, _ := splitID(row.entityID)
			if warm { // warmCensus: a config that got no answer is the scan's to read again
				if _, err := e.s.warmRead("ws", loadedConfigRequest(row.entityID, domain)); err != nil && !answered(err) {
					return
				}
			}
			loaded, ok, err := loadedConfig(e.s, row.entityID, domain) // what runs, so what holds the listeners
			switch {
			case err != nil: // a failed read: typed below, never "no config"
				row.err = err
			case !ok:
				row.err = &verbFailure{code: "CONFIG_NOT_FOUND"}
			default:
				row.raw = loaded
				e.warmBlueprint(domain, row.raw) // parseConfig below reads it from the memo
			}
		}(row)
	}
	wg.Wait()
}

// collectWaits adds the event types of every event trigger inside a
// wait_for_trigger of node, the listeners a run holds while it waits.
func collectWaits(node any, waits map[string]bool) {
	switch n := node.(type) {
	case []any:
		for _, item := range n {
			collectWaits(item, waits)
		}
	case map[string]any:
		for _, trigger := range flattenTriggers(n["wait_for_trigger"]) {
			if firstPresent(trigger, "trigger", "platform") != "event" {
				continue
			}
			types, isList := rendered(trigger["event_type"]).([]any)
			if !isList {
				types = []any{rendered(trigger["event_type"])}
			}
			for _, t := range types {
				name, isString := t.(string)
				if !isString || isTemplated(name) {
					name = ""
				}
				waits[name] = true
			}
		}
		for _, value := range n {
			collectWaits(value, waits)
		}
	}
}
