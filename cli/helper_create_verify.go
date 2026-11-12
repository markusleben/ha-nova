package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The CLI verifies a config-entry helper create itself (GitHub #1105):
// agents skipped the read-back steps after a flow's terminal submit. When
// `relay core` submits a step of a helper's config flow and Home Assistant
// answers create_entry, the CLI reads the new entry back by the result's
// entry_id, lists its entities, and reads its options from an options flow
// it aborts unsubmitted, then adds the outcome to the output as
// "verification". The create already happened: a failed check is
// passed:false, never a failed command, so nobody runs the flow again.

// helperFlowDomains: the config-entry helper domains skills/helper/SKILL.md
// lists in its Scope (TestHelperFlowDomainsMatchTheSkill).
var helperFlowDomains = []string{
	"utility_meter", "derivative", "integration", "min_max", "threshold", "tod", "statistics", "group",
	"history_stats", "template", "generic_thermostat", "switch_as_x", "trend", "random", "filter", "generic_hygrostat",
}

var configFlowStepPath = regexp.MustCompile(`^/api/config/config_entries/flow/[A-Za-z0-9_-]+$`)

type helperCreateVerification struct {
	Passed         bool           `json:"passed"`
	Problem        string         `json:"problem,omitempty"`
	Entry          *helperEntry   `json:"entry,omitempty"`
	LinkedEntities []string       `json:"linked_entities"`
	Options        map[string]any `json:"options"`
}

type helperEntry struct {
	EntryID string `json:"entry_id"`
	Domain  string `json:"domain"`
	Title   string `json:"title"`
	State   string `json:"state"`
}

// helperCreated: the handler and entry id of a helper create this relay
// core reply finished (reply is the Relay envelope before redaction); ok is
// false for anything else, which the CLI leaves as it is.
func helperCreated(method, rawPath string, reply []byte) (handler, entryID string, ok bool) {
	target, valid := relayCorePath(rawPath)
	if !valid || !strings.EqualFold(strings.TrimSpace(method), http.MethodPost) || !configFlowStepPath.MatchString(target) {
		return "", "", false
	}
	var envelope struct {
		Data struct {
			Status int `json:"status"`
			Body   struct {
				Type    string `json:"type"`
				Handler string `json:"handler"`
				Result  struct {
					EntryID string `json:"entry_id"`
				} `json:"result"`
			} `json:"body"`
		} `json:"data"`
	}
	if json.Unmarshal(reply, &envelope) != nil || envelope.Data.Status != http.StatusOK || envelope.Data.Body.Type != "create_entry" ||
		!slices.Contains(helperFlowDomains, envelope.Data.Body.Handler) {
		return "", "", false
	}
	return envelope.Data.Body.Handler, envelope.Data.Body.Result.EntryID, true
}

// helperVerifyBudget bounds the read-back: the create's answer waits for it,
// and a caller that gave up before seeing that answer might create again.
var helperVerifyBudget = 15 * time.Second

// verifyHelperCreate reads the created entry back on the command's own
// transport, within helperVerifyBudget (an options flow's abort has its own).
func verifyHelperCreate(paths runtimePaths, cfg runtimeConfig, transport relayTransportSelection, handler, entryID string) helperCreateVerification {
	ctx, cancel := context.WithTimeout(context.Background(), helperVerifyBudget)
	s := relaySessionOn(paths, cfg, transport, ctx, cancel)
	defer s.cancel()
	v := helperCreateVerification{LinkedEntities: []string{}}
	fail := func(problem string) helperCreateVerification {
		v.Problem = "The helper was created, but HA NOVA could not confirm it: " + problem + ". Don't run the flow again; check it under Settings > Devices & services > Helpers."
		return v
	}
	failErr := func(err error) helperCreateVerification {
		text := err.Error()
		for _, claim := range []string{"; nothing was changed", "; nothing was sent"} { // true of the read, not of the create
			text = strings.ReplaceAll(text, claim, "")
		}
		return fail(text)
	}
	if !flowIDPattern.MatchString(entryID) {
		return fail("Home Assistant's answer named no entry")
	}
	type entryRow struct {
		helperEntry
		SupportsOptions bool `json:"supports_options"`
	}
	type manifest struct {
		Domain  string `json:"domain"`
		BuiltIn bool   `json:"is_built_in"`
	}
	var entries []entryRow
	if err := s.wsDecode(map[string]any{"type": "config_entries/get"}, &entries, "the config entries"); err != nil {
		return failErr(err)
	}
	i := slices.IndexFunc(entries, func(e entryRow) bool { return e.EntryID == entryID })
	if i < 0 || entries[i].Domain != handler {
		return fail("entry " + entryID + " is not a " + handler + " entry in the config entries")
	}
	v.Entry = &entries[i].helperEntry
	// As the tier engine's options read: never trust, or start a flow of, a
	// custom integration that took over a helper's domain.
	var manifests []manifest
	if err := s.wsDecode(map[string]any{"type": "manifest/list"}, &manifests, "the integrations"); err != nil {
		return failErr(err)
	}
	if !slices.ContainsFunc(manifests, func(m manifest) bool { return m.Domain == handler && m.BuiltIn }) {
		return fail(handler + " is not Home Assistant's built-in integration")
	}
	if v.Entry.State != "loaded" {
		return fail("its entry is " + v.Entry.State + ", not loaded")
	}
	var rows []struct {
		EntityID      string `json:"entity_id"`
		ConfigEntryID string `json:"config_entry_id"`
	}
	if err := s.wsDecode(map[string]any{"type": "config/entity_registry/list"}, &rows, "the entity registry"); err != nil {
		return failErr(err)
	}
	for _, row := range rows {
		if row.ConfigEntryID == entryID {
			v.LinkedEntities = append(v.LinkedEntities, row.EntityID)
		}
	}
	slices.Sort(v.LinkedEntities)
	if entries[i].SupportsOptions {
		form, err := s.optionsForm(entryID)
		if err != nil {
			return failErr(err)
		}
		v.Options = map[string]any{}
		for _, f := range form.DataSchema {
			v.Options[f.Name] = f.Description.Suggested
		}
	}
	v.Passed = true
	return v
}

// withHelperVerification adds v to the Relay envelope as "verification".
func withHelperVerification(envelope []byte, v helperCreateVerification) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(envelope, &fields); err != nil {
		return nil, fmt.Errorf("adding the create's verification: %w", err)
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	fields["verification"] = encoded
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false) // a template's > stays >
	if err := enc.Encode(fields); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}
