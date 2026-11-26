package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Warm reads for a call plan (MAR-279, the lamp under 5 s). The engine reads
// one thing after another: the registries, the service list, the event
// listeners, the integrations, and the listener census each cost a round
// trip, and over Home Assistant Link those round trips were most of a plan's
// time. These functions only send the same reads earlier and together, into
// the session memo; the engine still makes every read itself, in its own
// order, and decides from those answers. Never in a replay, which reads only
// what its preview read, and evaluate waits for them (e.warming) before the
// plan is sealed, so its reads are settled.

// warmCallReads sends the first reads every call plan makes, and the states
// of the first named targets, within the session's request slots. Not for
// --match, which resolves serially so its plan reads only what its own
// checks read (#1261).
func (e *tierEngine) warmCallReads(req planCallRequest) {
	if e.s == nil || e.s.replaying() || req.Match != "" {
		return
	}
	ws := func(wsType string) func() {
		return func() { e.s.warmWS(map[string]any{"type": wsType}) }
	}
	core := func(path string) func() {
		return func() { _, _, _ = e.s.warmCoreGET(path) }
	}
	var reads []func()
	for _, id := range req.EntityIDs[:min(len(req.EntityIDs), 2)] {
		reads = append(reads, core("/api/states/"+id))
	}
	reads = append(reads, ws("config/entity_registry/list_for_display"), core("/api/services"), ws("config/device_registry/list"),
		ws("config/area_registry/list"), core("/api/events"), ws("manifest/list"))
	e.warm(func() { prefetch(reads) })
}

// warmCensus starts what the listener census and the helper check will read
// when a call sets off call_service listeners: every state, every automation
// config (listenerTable), and the entity sources and registry, with the
// YAML helpers where a thermostat or hygrostat runs from YAML (helperCalls,
// yamlHelpersRun). The census reads every state anyway, so from here that
// one read serves each state the walk reads too, in a replay as in its
// preview. Only once the call's
// own target is known not to be typed: such a call skips the census
// (censusMoot).
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
	e.warm(func() {
		if _, helpers, _ := helperComponents(e.s.warmCoreGET("/api/config")); helpers && e.s.yamlHelpersRun() {
			_, _ = e.s.warmRead(yamlHelpersEndpoint, map[string]any{})
		}
	})
	if !e.s.gateExempt() { // readBatched never batches for the App's own callers
		e.warm(func() { e.s.wsBatch() })
	}
	e.warm(func() {
		status, body, err := e.s.warmCoreGET("/api/states")
		if rows, err := censusRows(status, body, err); err == nil && len(rows) <= tierMaxAutomations {
			e.readCensus(rows, relaySessionMaxConcurrent/2, true) // the walk keeps the other slots
		}
	})
}

// warmQuiet sends the search/related reads of every place loud checks for
// id at once; loud reads them one by one from the memo.
func (e *tierEngine) warmQuiet(id string) {
	if e.s == nil || e.listeners == nil || e.s.replaying() || !e.placesWarm() { // without listeners loud reads nothing
		return
	}
	items, _, _ := e.relatedItems(id) // an unreadable registry: loud says so
	var reads []func()
	for _, item := range items {
		reads = append(reads, func() { e.s.warmWS(e.relatedRequest(item[0], item[1])) })
	}
	prefetch(reads)
}

// warmConfigs reads the configs loud reads one by one for a place's
// automations, together.
func (e *tierEngine) warmConfigs(automations []string) {
	if e.s.replaying() {
		return
	}
	var reads []func()
	for _, automation := range automations {
		reads = append(reads, func() { e.warmConfig(automation) })
	}
	prefetch(reads)
}

// recheckListeners reads the listener counts once more, past the memo, just
// before the plan is sealed: the census read its states and configs earlier
// in the check, so a listener added since (a script that started and now
// waits on call_service) must not leave the tier lower. A higher count, or
// no answer, is a gap; a listener gone adds no reaction the census missed.
func (e *tierEngine) recheckListeners() {
	if e.listeners == nil { // no census: nothing it counted can be stale
		return
	}
	data, err := e.s.readFresh("core", map[string]any{"method": http.MethodGet, "path": "/api/events"}, relaySessionRequestTimeout)
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err == nil {
		err = json.Unmarshal(data, &core)
	}
	now, err := eventListeners(core.Status, core.Body, err)
	if err != nil {
		e.readFailed("", viaListenerScan, "event listeners before sealing")
		return
	}
	for _, t := range []string{"call_service", "automation_triggered", "script_started", "state_changed"} {
		if now[t] > e.events[t] {
			e.gap(reasonListener, "", viaListenerScan, fmt.Sprintf("%s: %d listeners at the census, %d before sealing", t, e.events[t], now[t]))
		}
	}
}

// warmState sends the read rawState makes for id, ahead of it.
func (e *tierEngine) warmState(id string) { _, _, _ = e.s.warmCoreGET(e.statePath(id)) }

// statePath: what rawState reads for id, the one state list once it serves
// every state (useStateList).
func (e *tierEngine) statePath(id string) string {
	if e.stateList != nil {
		return "/api/states"
	}
	return "/api/states/" + id
}

// placesWarm: the device and area registries relatedItems reads answered a
// warm read, so a warm caller of it pins no failure nobody used.
func (e *tierEngine) placesWarm() bool {
	for _, kind := range []string{"device", "area"} {
		if _, err := e.s.warmRead("ws", map[string]any{"type": "config/" + kind + "_registry/list"}); err != nil && !answered(err) {
			return false
		}
	}
	return true
}

// warm runs read in the background; evaluate waits for every one.
func (e *tierEngine) warm(read func()) {
	e.warming.Add(1)
	go func() {
		defer e.warming.Done()
		read()
	}()
}

// helperComponents: the integration names GET /api/config lists as loaded
// ("flux.switch" is flux), and whether a thermostat or hygrostat helper is
// among them; ok false when unread.
func helperComponents(status int, body json.RawMessage, err error) (names []string, helpers, ok bool) {
	var config struct {
		Components []string `json:"components"`
	}
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &config) != nil {
		return nil, false, false
	}
	for _, component := range config.Components {
		name, _, _ := strings.Cut(component, ".")
		names = append(names, name)
		helpers = helpers || helperOptionFields[name] != ""
	}
	return names, helpers, len(names) > 0 // Home Assistant always loads components: none is no answer (MAR-272)
}
