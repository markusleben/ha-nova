package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// UI thermostat and hygrostat helpers (MAR-131,
// docs/work/2026-11-10-helper-options-read-spec.md). Both call exactly
// homeassistant.turn_on and turn_off, on the one entity their options name
// (Home Assistant 2026.11: generic_thermostat/climate.py, generic_hygrostat/
// humidifier.py). Their options are read by starting the entry's options
// flow, whose init form suggests the stored values, and aborting it, which
// saves nothing and reloads nothing. No other integration's flow is started.

// helperOptionFields: the allowlisted domains and the options field that
// names the entity each switches. Re-checked in upstream-drift-log.md.
var helperOptionFields = map[string]string{"generic_thermostat": "heater", "generic_hygrostat": "humidifier"}

// helperOptionsEndpoint is the relay session's pseudo-endpoint for that
// read: payload exactly {"entry_id": "<id>"}, answered by helperOptions.
const helperOptionsEndpoint = "helper_options"

const optionsFlowPath = "/api/config/config_entries/options/flow"

// helperAbortTimeout bounds the abort on its own, past the session budget.
// helperStartTimeout bounds the start on its own too: past the longest a
// gated App takes to answer it (its check's 20 s worker wait, then Home
// Assistant's 10 s), so the flow id to abort always arrives (MAR-153). A
// start goes out only while helperStartReserve of the budget is left.
var (
	helperAbortTimeout = 5 * time.Second
	helperStartTimeout = 35 * time.Second
	helperStartReserve = 5 * time.Second
)

// helperOptions answers the pseudo-endpoint: the stored entity of one
// allowlisted, built-in entry, as a JSON string. The flow it starts is
// aborted whenever its answer names one, before anything is checked.
func (s *relaySession) helperOptions(body []byte) (json.RawMessage, error) {
	var req struct {
		EntryID string `json:"entry_id"`
	}
	if json.Unmarshal(body, &req) != nil || !flowIDPattern.MatchString(req.EntryID) {
		return nil, errors.New("helper options: not an entry id; nothing was sent")
	}
	s.mu.Lock()
	ctx, startCheck := s.ctx, s.startCheck
	s.mu.Unlock()
	if startCheck {
		return nil, errors.New("helper options: a check of an options-flow start never starts a flow itself; nothing was sent")
	}
	field, err := s.helperEntry(req.EntryID)
	if err != nil {
		return nil, err
	}
	if deadline, set := ctx.Deadline(); ctx.Err() != nil || set && time.Until(deadline) < helperStartReserve {
		return nil, errors.New("helper options: too little of the read budget is left to start and abort an options flow; nothing was sent")
	}

	start, _ := json.Marshal(map[string]any{"method": http.MethodPost, "path": optionsFlowPath, "body": map[string]any{"handler": req.EntryID}})
	data, err := s.exchange(context.WithoutCancel(ctx), "core", start, helperStartTimeout, false) // once: a retry could open a second flow
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	var form struct {
		Type       string `json:"type"`
		FlowID     string `json:"flow_id"`
		Handler    string `json:"handler"`
		StepID     string `json:"step_id"`
		DataSchema []struct {
			Name        string         `json:"name"`
			Selector    map[string]any `json:"selector"`
			Description struct {
				Suggested any `json:"suggested_value"`
			} `json:"description"`
		} `json:"data_schema"`
	}
	if err == nil {
		_ = json.Unmarshal(data, &core)
		_ = json.Unmarshal(core.Body, &form)
	}
	abort := errors.New("its answer named no flow to abort")
	if form.FlowID != "" {
		abort = s.abortOptionsFlow(ctx, form.FlowID)
	}
	switch {
	case err != nil:
		return nil, err
	case abort != nil:
		return nil, fmt.Errorf("helper options: the options flow of %s was not aborted: %w", req.EntryID, abort)
	case ctx.Err() != nil: // the start may outlast the budget, its answer never
		return nil, fmt.Errorf("HA NOVA read budget of %s exceeded", relaySessionOverallTimeout)
	case core.Status != http.StatusOK || form.Type != "form" || form.Handler != req.EntryID || form.StepID != "init":
		return nil, fmt.Errorf("helper options: entry %s answered HTTP %d, %q step %q", req.EntryID, core.Status, form.Type, form.StepID)
	}
	for _, f := range form.DataSchema {
		stored, literal := f.Description.Suggested.(string)
		if f.Name != field {
			continue
		}
		if _, entity := f.Selector["entity"]; !entity || !literal || !entityIDPattern.MatchString(stored) {
			return nil, fmt.Errorf("helper options: %s of entry %s is not one entity id", field, req.EntryID)
		}
		return json.Marshal(stored)
	}
	return nil, fmt.Errorf("helper options: entry %s shows no %s", req.EntryID, field)
}

// helperEntry: the options field of entryID, read live: an allowlisted,
// built-in entry that has options. helperOptions checks it before its start,
// and the gate before it lets the App's own check send that start
// (gateProbe).
func (s *relaySession) helperEntry(entryID string) (string, error) {
	type entryRow struct {
		EntryID         string `json:"entry_id"`
		Domain          string `json:"domain"`
		SupportsOptions bool   `json:"supports_options"`
	}
	type manifest struct {
		Domain  string `json:"domain"`
		BuiltIn bool   `json:"is_built_in"`
	}
	var entries []entryRow
	if err := s.wsDecode(map[string]any{"type": "config_entries/get"}, &entries, "the config entries"); err != nil {
		return "", err
	}
	i := slices.IndexFunc(entries, func(e entryRow) bool { return e.EntryID == entryID })
	if i < 0 || helperOptionFields[entries[i].Domain] == "" || !entries[i].SupportsOptions {
		return "", fmt.Errorf("helper options: entry %s is not a thermostat or hygrostat helper; nothing was sent", entryID)
	}
	domain := entries[i].Domain
	var manifests []manifest
	if err := s.wsDecode(map[string]any{"type": "manifest/list"}, &manifests, "the integrations"); err != nil {
		return "", err
	}
	if !slices.ContainsFunc(manifests, func(m manifest) bool { return m.Domain == domain && m.BuiltIn }) {
		return "", fmt.Errorf("helper options: %s is not the built-in integration; nothing was sent", domain)
	}
	return helperOptionFields[domain], nil
}

// abortOptionsFlow deletes a started options flow on a context of its own,
// so an expired session budget never skips it.
func (s *relaySession) abortOptionsFlow(parent context.Context, flowID string) error {
	if !flowIDPattern.MatchString(flowID) {
		return fmt.Errorf("flow id %q", flowID)
	}
	body, _ := json.Marshal(map[string]any{"method": http.MethodDelete, "path": optionsFlowPath + "/" + flowID})
	data, err := s.exchange(context.WithoutCancel(parent), "core", body, helperAbortTimeout, false)
	var core struct {
		Status int `json:"status"`
	}
	if err == nil && (json.Unmarshal(data, &core) != nil || core.Status != http.StatusOK) {
		err = fmt.Errorf("HTTP %d", core.Status)
	}
	return err
}

// helperCalls bounds what the UI thermostats and hygrostats in this home
// call (the reactor rule, tierEngine.reactorCalls): homeassistant.turn_on
// and turn_off on the plain switch or fan each one's options name, when
// nothing reacts to the helper's state or that entity's. Anything else
// makes the set unknown.
func (e *tierEngine) helperCalls() {
	if e.calls.unknown {
		return
	}
	if len(e.reg) == 0 && !e.regFailed {
		e.loadRegistry()
	}
	var sources map[string]struct {
		Domain string `json:"domain"`
	}
	rows, err := e.fullRegistry()
	if err == nil {
		err = e.s.wsDecode(map[string]any{"type": "entity/source"}, &sources, "the entity sources")
	}
	if err != nil || sources == nil {
		e.calls.unknown = true
		return
	}
	entryOf := map[string]string{}
	for _, row := range rows {
		entryOf[row.EntityID] = row.ConfigEntryID
	}
	var helpers, entries []string
	for id, source := range sources {
		if helperOptionFields[source.Domain] != "" {
			helpers = append(helpers, id)
		}
	}
	slices.Sort(helpers)
	for _, id := range helpers {
		if entryOf[id] == "" || !e.quiet(id) { // YAML: no options to read
			e.calls.unknown = true
			return
		}
		entries = append(entries, entryOf[id])
	}
	slices.Sort(entries)
	for _, entry := range slices.Compact(entries) {
		var stored string
		data, err := e.s.read(helperOptionsEndpoint, map[string]any{"entry_id": entry})
		if err != nil || json.Unmarshal(data, &stored) != nil || !e.plainSwitched(stored) {
			e.calls.unknown = true
			return
		}
		e.calls.add("homeassistant.turn_on")
		e.calls.add("homeassistant.turn_off")
		e.calledOn(stored)
		if e.calls.unknown {
			return
		}
	}
}

// plainSwitched: id is a switch or fan that only switches, as the helpers'
// forms allow: no group or switch_as_x on either side of it (a device-less
// one counts, as in switchAsX), and quiet.
func (e *tierEngine) plainSwitched(id string) bool {
	domain, _ := splitID(id)
	entry, registered := e.reg[id]
	if domain != "switch" && domain != "fan" || !registered || entry.pl == "group" || entry.pl == "switch_as_x" {
		return false
	}
	for _, row := range e.reg {
		if row.di == entry.di && row.pl == "switch_as_x" {
			return false
		}
	}
	return e.quiet(id)
}

// quiet: nothing reacts to id's state, which would call more: no group that
// search/related ties to it or its device, no script there or where it sits
// (a wait on its state), no automation tied to it or its device whose
// triggers match either (a thermostat sits on its heater's device:
// automations that only set it don't count), none where it sits whose
// triggers target that place (as scanConsumers), and no automation on
// state_changed events, which search/related doesn't index.
func (e *tierEngine) quiet(id string) bool {
	if e.listeners == nil || len(e.listeners.hits["state_changed"]) > 0 {
		return false
	}
	items, direct, ok := e.relatedItems(id)
	if !ok {
		return false
	}
	for i, item := range items {
		related, err := e.related(item[0], item[1])
		if err != nil || len(related["script"]) > 0 || i < direct && len(related["group"]) > 0 {
			return false
		}
		for _, automation := range related["automation"] {
			cfg := e.warmConfig(automation)
			if cfg == nil || i < direct && triggerMatch(cfg, id, e.needles(id), stateDelta{}) != "" || i >= direct && targetsPlace(cfg, items[direct:]) {
				return false
			}
		}
	}
	return true
}
