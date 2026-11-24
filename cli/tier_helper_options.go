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
// A start goes out only while helperStartReserve of the budget is left, so a
// budget that ends mid-start, and loses the flow id to abort, is rare
// (MAR-153).
var (
	helperAbortTimeout = 5 * time.Second
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
	startCheck := s.startCheck
	s.mu.Unlock()
	if startCheck {
		return nil, errors.New("helper options: a check of an options-flow start never starts a flow itself; nothing was sent")
	}
	field, err := s.helperEntry(req.EntryID)
	if err != nil {
		return nil, err
	}
	form, err := s.optionsForm(req.EntryID)
	if err == nil && form.StepID != "init" {
		err = fmt.Errorf("entry %s answered HTTP %d, %q step %q", req.EntryID, http.StatusOK, form.Type, form.StepID)
	}
	if err != nil {
		return nil, fmt.Errorf("helper options: %w", err)
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

// optionsFormAnswer: the first answer of an entry's options flow.
type optionsFormAnswer struct {
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

// optionsForm starts entryID's options flow once (a retry could open a
// second one), only while helperStartReserve of the budget is left, aborts
// it whenever its answer names one, before anything is checked, and returns
// that answer only when it is a form of that entry (its first step: init for
// most helpers, the entity type for a template or group).
func (s *relaySession) optionsForm(entryID string) (optionsFormAnswer, error) {
	var form optionsFormAnswer
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if deadline, set := ctx.Deadline(); ctx.Err() != nil || set && time.Until(deadline) < helperStartReserve {
		return form, errors.New("too little of the read budget is left to start and abort an options flow; nothing was sent")
	}
	start, _ := json.Marshal(map[string]any{"method": http.MethodPost, "path": optionsFlowPath, "body": map[string]any{"handler": entryID}})
	data, err := s.exchange(ctx, "core", start, relaySessionRequestTimeout, false)
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
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
		return form, err
	case abort != nil:
		return form, fmt.Errorf("the options flow of %s was not aborted: %w", entryID, abort)
	case core.Status != http.StatusOK || form.Type != "form" || form.Handler != entryID:
		return form, fmt.Errorf("entry %s answered HTTP %d, %q step %q", entryID, core.Status, form.Type, form.StepID)
	}
	return form, nil
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

// helperCalls bounds what the thermostats and hygrostats in this home call
// (the reactor rule, tierEngine.reactorCalls): homeassistant.turn_on and
// turn_off on the plain switch or fan each one's options name (UI) or its
// YAML names (yamlHelperCalls), when nothing reacts to the helper's state or
// that entity's. Anything else makes the set unknown.
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
	if err != nil || len(sources) == 0 { // a home with no entity sources is no answer (MAR-272)
		e.calls.unknown = true
		return
	}
	rowOf := map[string]fullRegistryRow{}
	for _, row := range rows {
		rowOf[row.EntityID] = row
	}
	var helpers, entries []string
	yamlHelpers := map[string][]string{} // integration → its YAML entities (no config entry)
	helperOf := map[string]string{}      // config entry → its first helper
	for id, source := range sources {
		if helperOptionFields[source.Domain] != "" {
			helpers = append(helpers, id)
		}
	}
	slices.Sort(helpers)
	for _, id := range helpers {
		entry := rowOf[id].ConfigEntryID
		if !e.quiet(id) {
			e.helperUnbounded(id, "helper_listened")
			return
		}
		if entry == "" {
			yamlHelpers[sources[id].Domain] = append(yamlHelpers[sources[id].Domain], id)
			continue
		}
		if helperOf[entry] == "" {
			helperOf[entry] = id
		}
		entries = append(entries, entry)
	}
	if len(yamlHelpers) > 0 && !e.yamlHelperCalls(yamlHelpers, rowOf) {
		return
	}
	slices.Sort(entries)
	for _, entry := range slices.Compact(entries) {
		e.loudWhy = ""
		var stored string
		data, err := e.s.read(helperOptionsEndpoint, map[string]any{"entry_id": entry})
		switch {
		case err != nil || json.Unmarshal(data, &stored) != nil:
			e.helperUnbounded(helperOf[entry], "options_unread")
			return
		case !e.plainSwitched(stored):
			e.helperUnbounded(helperOf[entry], "heater_not_plain")
			return
		}
		e.calls.add("homeassistant.turn_on")
		e.calls.add("homeassistant.turn_off")
		if e.calledOn(stored); e.calls.unknown {
			e.helperUnbounded(helperOf[entry], "heater_not_plain")
			return
		}
	}
}

// helperUnbounded leaves the calls unknown and keeps why for the plan
// (reasonHelperCalls): the helper, detail, and the quiet check that failed,
// if one did.
func (e *tierEngine) helperUnbounded(id, detail string) {
	if e.loudWhy != "" {
		detail += ": " + e.loudWhy
	}
	e.calls.unknown = true
	e.helperCause = tierReason{Code: reasonHelperCalls, EntityID: id, Detail: detail}
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

// quiet: nothing reacts to id's state, which would call more (loud); the
// last check that failed stays in e.loudWhy for the plan's reason detail.
func (e *tierEngine) quiet(id string) bool {
	e.warmQuiet(id)
	e.loudWhy = e.loud(id)
	return e.loudWhy == ""
}

// loud names the first check that fails, "" when none does: no
// state_changed listener that may name id (search/related doesn't index
// them), no group that search/related ties to it or its device, no running
// script or automation tied to it or where it sits (one at rest reacts only
// through its triggers; an unread state counts as running), no automation
// tied to it or its device whose triggers match either (a thermostat sits on
// its heater's device: automations that only set it don't count, nor one
// that only syncs it after a reconnect, quietSync), and none where it sits
// whose triggers target that place (as scanConsumers).
func (e *tierEngine) loud(id string) string {
	if e.listeners == nil {
		return id + ": listeners unread"
	}
	if why := e.listeners.stateChanged(id); why != "" {
		return id + ": " + why
	}
	items, direct, ok := e.relatedItems(id)
	if !ok {
		return id + ": registry unread"
	}
	for i, item := range items {
		related, err := e.related(item[0], item[1])
		if err != nil {
			return id + ": related unread for " + item[0] + " " + item[1]
		}
		where := " (" + item[0] + " " + item[1] + ")"
		if i < direct && len(related["group"]) > 0 {
			return id + ": group " + related["group"][0]
		}
		e.prefetchStates(slices.Concat(related["script"], related["automation"]))
		e.warmConfigs(related["automation"])
		for _, script := range related["script"] {
			st, err := e.state(script)
			switch {
			case err != nil || st == nil:
				return id + ": state unread of " + script + where
			case st.State != "off":
				return id + ": running script " + script + where
			}
		}
		for _, automation := range related["automation"] {
			st, err := e.state(automation)
			switch cfg := e.warmConfig(automation); {
			case err != nil || st == nil:
				return id + ": state unread of " + automation + where
			case st.attr("current") != float64(0):
				return id + ": running automation " + automation + where
			case cfg == nil:
				return id + ": config unread of " + automation
			case i >= direct:
				if targetsPlace(cfg, items[direct:]) {
					return id + ": place trigger of " + automation + where
				}
			default:
				verdict, read := e.triggerMatch(cfg, id, stateDelta{})
				if !read {
					return id + ": registry unread"
				}
				if verdict != "" && !e.quietSync(cfg, automation, id) {
					return id + ": trigger of " + automation
				}
			}
		}
	}
	return ""
}
