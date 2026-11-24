package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

// Warm reads for a call plan (MAR-279, the lamp under 5 s). The engine reads
// one thing after another: the registries, the service list, the event
// listeners, the integrations, and /api/config each cost a round trip, and
// over Home Assistant Link those round trips were most of a plan's time.
// These functions only send the same reads earlier and together, into the
// session memo; the engine still makes every read itself, in its own order,
// and decides from those answers, so the plan is the one the serial reads
// made. Never in a replay, which reads only what its preview read.

// warmCallReads sends the reads every call plan makes, and the states of the
// named targets, without waiting for them.
func (e *tierEngine) warmCallReads(req planCallRequest) {
	if e.s == nil || e.s.replaying() {
		return
	}
	ws := func(wsType string) func() {
		return func() { _, _ = e.s.ws(map[string]any{"type": wsType}) }
	}
	core := func(path string) func() {
		return func() { _, _, _ = e.s.coreGET(path) }
	}
	var first []func() // what the engine reads first, within the session's request slots
	for _, id := range req.EntityIDs[:min(len(req.EntityIDs), 2)] {
		first = append(first, core("/api/states/"+id))
	}
	first = append(first, ws("config/entity_registry/list_for_display"), core("/api/services"), ws("config/device_registry/list"),
		ws("config/area_registry/list"), core("/api/events"), func() { e.s.wsBatch() })
	go func() {
		prefetch(first)
		prefetch([]func(){ws("config/entity_registry/list"), ws("config_entries/get"), ws("manifest/list")})
	}()
}

// warmCensus starts, without waiting, what the listener census and the
// helper check will read when a call sets off call_service listeners: every
// state, every automation config (listenerTable), and the entity sources
// (helperCalls). The census reads every state anyway, so from here that one
// read serves each state the walk reads too. Only once the call's own target
// is known not to be typed: such a call skips the census (censusMoot). A
// replay switches to the state list as its preview did, and reads nothing
// ahead.
func (e *tierEngine) warmCensus() {
	if e.censusMoot || e.liveEvents()["call_service"] <= baselineFor("call_service") {
		return
	}
	if e.stateList == nil {
		e.useStateList()
	}
	if e.s.replaying() {
		return
	}
	go func() {
		go e.warmSources()
		status, body, err := e.s.coreGET("/api/states")
		if rows, err := censusRows(status, body, err); err == nil && len(rows) <= tierMaxAutomations {
			e.readCensus(rows)
		}
	}()
}

// warmSources reads the entity sources when a thermostat or hygrostat
// helper is loaded (/api/config components), as helperCalls will.
func (e *tierEngine) warmSources() {
	var config struct {
		Components []string `json:"components"`
	}
	status, body, err := e.s.coreGET("/api/config")
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &config) != nil {
		return
	}
	if slices.ContainsFunc(config.Components, func(c string) bool { name, _, _ := strings.Cut(c, "."); return helperOptionFields[name] != "" }) {
		_, _ = e.s.ws(map[string]any{"type": "entity/source"})
	}
}

// warmQuiet sends what loud reads for id at once: the search/related reads
// of where it sits, then the state and config of every script and
// automation they name. loud then reads them one by one from the memo.
func (e *tierEngine) warmQuiet(id string) {
	if e.s == nil || e.listeners == nil || e.s.replaying() { // without listeners loud reads nothing
		return
	}
	items, _, _ := e.relatedItems(id) // an unreadable registry: loud says so
	var reads []func()
	for _, item := range items {
		reads = append(reads, func() { _, _ = e.s.ws(e.relatedRequest(item[0], item[1])) })
	}
	prefetch(reads)
	reads = nil
	for _, item := range items {
		related, _ := e.related(item[0], item[1])
		for _, script := range related["script"] {
			reads = append(reads, func() { _, _, _ = e.rawState(script) })
		}
		for _, automation := range related["automation"] {
			reads = append(reads, func() { _, _, _ = e.rawState(automation) }, func() { e.warmConfig(automation) })
		}
	}
	prefetch(reads)
}
