package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Config execution (typed-verbs spec §2.3 config writes): one write, a
// reload, a read-back compared with the plan, the registry entity and its
// state. A delete is preceded by a pre-delete config snapshot.

// errSnapshotFailed: nothing was deleted, so apply releases the plan (the
// typed code stays valid for --no-snapshot).
var errSnapshotFailed = errors.New("pre-delete snapshot failed")

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// snapshotName is the relay-safe auto-snapshot name (config-snapshots.md):
// auto-<id lowercased, every other run of characters a dash>, ≤ 64 chars.
func snapshotName(id string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(id), "-"), "-")
	if len(slug) > 59 {
		slug = strings.TrimRight(slug[:59], "-")
	}
	return "auto-" + slug
}

func executeConfig(paths runtimePaths, s *relaySession, env *verbEnvelope, action configAction, ctx configContext, noSnapshot bool) error {
	s.renewBudget()
	if action.Op == "delete" && !noSnapshot {
		// The live read-back goes in as raw JSON: no number or order round trip.
		file, err := s.saveConfigSnapshot(map[string]string{"automation": "automations", "script": "scripts"}[action.Domain], snapshotName(action.EntityID), ctx.current)
		if err != nil {
			return &verbFailure{exit: verbExitError, code: "SNAPSHOT_FAILED",
				message: fmt.Sprintf("no pre-delete config snapshot could be taken (%s); nothing was deleted and the plan stays valid", err),
				next:    "Prune the snapshot store, take a full backup (ha-nova:backup), or — if the user chooses to — apply again with --no-snapshot.",
				cause:   errSnapshotFailed}
		}
		env.Snapshot = file
	}
	var result serviceResult
	var err error
	if action.Op == "delete" {
		result, err = s.writeCore(http.MethodDelete, action.Path, nil)
	} else {
		result, err = s.writeCore(http.MethodPost, action.Path, action.Body)
	}
	// An update or revert that may have landed keeps its way back: the
	// checkpoint is saved for any 2xx and for an unknown outcome, and the
	// before-config is in the output either way.
	update := action.Op != "delete" && ctx.current != nil
	var unknown *relayOutcomeUnknownError
	if update && (errors.As(err, &unknown) || (err == nil && result.status < 300)) {
		env.BeforeConfig = ctx.current
	}
	alreadyGone := action.Op == "delete" && err == nil && (result.status == http.StatusBadRequest || result.status == http.StatusNotFound)
	if failure := writeFailure(env, result, err); failure != nil && !alreadyGone {
		if errors.As(err, &unknown) && update {
			saveCheckpoint(paths, s, env, action, ctx, action.Body)
		}
		return failure
	}
	reloadData := map[string]any{}
	if action.Domain == "automation" { // the scope Home Assistant's own config hook reloads
		reloadData["id"] = action.ConfigID
	}
	reload, reloadErr := s.postService(action.Domain+".reload", reloadData)
	reloaded := reloadErr == nil && reload.status < 400
	s.renewBudget()
	row, readback := verifyConfig(s, action, reloaded)
	if alreadyGone && row.Outcome == "verified" {
		row.Note = "already gone when the delete arrived (removed concurrently)"
	}
	env.Verification, env.Status, env.exit = []verifyRow{row}, row.Outcome, verbExitOK
	if row.Outcome != "verified" {
		env.exit = verbExitUnverified
	}
	if update {
		expected := readback
		if expected == nil {
			expected = action.Body
		}
		saveCheckpoint(paths, s, env, action, ctx, expected)
	}
	return nil
}

// saveCheckpoint puts an update (a revert is one too, as in update-revert.md)
// on this server's undo stack: one step back to ctx.current.
func saveCheckpoint(paths runtimePaths, s *relaySession, env *verbEnvelope, action configAction, ctx configContext, expectedAfter json.RawMessage) {
	record, _ := json.Marshal(map[string]any{"op": "update", "domain": action.Domain, "target_id": action.ConfigID, "server": s.server,
		"name": ctx.alias, "before_config": ctx.current, "expected_after": expectedAfter})
	if receipt, err := saveUndoSnapshotBytes(paths, record); err == nil {
		env.Undo = &receipt
	} else {
		env.Notices = append(env.Notices, verbNotice{Kind: "undo", Text: "the update is not revertible: " + err.Error()})
	}
}

// writeFailure maps one config write's outcome like a service call's.
func writeFailure(env *verbEnvelope, result serviceResult, err error) error {
	var unknown *relayOutcomeUnknownError
	switch {
	case errors.As(err, &unknown):
		env.OutcomeUnknown = true
		return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN", message: err.Error(), next: "Read the config back; never retry automatically."}
	case err != nil:
		return fmt.Errorf("%w; Home Assistant did not receive the write", err)
	case rejectedStatuses[result.status]:
		return &verbFailure{exit: verbExitUnverified, status: "failed", code: "CONFIG_REJECTED",
			message: fmt.Sprintf("Home Assistant rejected the config (HTTP %d): %s", result.status, truncate(string(result.body), 300)),
			next:    "Nothing was saved; fix the draft and plan again."}
	case result.status >= 400:
		return &verbFailure{exit: verbExitUnverified, status: "unverified", code: "CONFIG_ERROR",
			message: fmt.Sprintf("Home Assistant answered HTTP %d: %s", result.status, truncate(string(result.body), 300)),
			next:    "Read the config back before anything else; never retry automatically."}
	}
	return nil
}

// verifyConfig: a write is verified only when the read-back equals the
// planned body (normalized), the reload succeeded, and the registry entity
// for the config id exists with a state; a delete when config and entity
// are gone.
func verifyConfig(s *relaySession, action configAction, reloaded bool) (verifyRow, json.RawMessage) {
	row := verifyRow{EntityID: action.EntityID, Outcome: "unverified"}
	status, readback, err := coreGETFresh(s, action.Path)
	if action.Op == "delete" {
		row.Expect = "config and entity gone"
		st, stateErr := s.stateFresh(action.EntityID, relaySessionRequestTimeout)
		row.Observed = fmt.Sprintf("config HTTP %d, entity present: %v", status, st != nil)
		if err == nil && stateErr == nil && status == http.StatusNotFound && st == nil {
			row.Outcome = "verified"
		}
		return row, nil
	}
	row.Expect = "read-back equals the plan, reload ok, entity present"
	planned, _ := configObjectFromBytes(action.Body)
	live, parseErr := configObjectFromBytes(readback)
	if err != nil || status != http.StatusOK || parseErr != nil {
		row.Observed = fmt.Sprintf("read-back failed (HTTP %d)", status)
		return row, nil
	}
	differences := append(renderConfigChanges(planned, live), orderChanges(action.Body, readback)...)
	entityID := registryEntityFor(s, action.Domain, action.ConfigID)
	row.EntityID = entityID
	var st *entityState
	if entityID != "" {
		st, _ = s.stateFresh(entityID, relaySessionRequestTimeout)
	}
	row.Observed = fmt.Sprintf("%d differences, reload ok: %v, entity: %q present: %v", len(differences), reloaded, entityID, st != nil)
	switch {
	case !reloaded:
		row.Note = "saved, but the reload barrier failed: whether the change is running is unknown"
	case len(differences) > 0:
		row.Note = "the read-back differs: " + strings.Join(differences, " ")
	case st != nil && st.State == "unavailable": // Home Assistant could not load the config
		row.Note = "the entity is unavailable: Home Assistant rejected the config when loading it"
	case st != nil:
		row.Outcome = "verified"
	}
	return row, readback
}

func coreGETFresh(s *relaySession, path string) (int, json.RawMessage, error) {
	data, err := s.readFresh("core", map[string]any{"method": http.MethodGet, "path": path}, relaySessionRequestTimeout)
	if err != nil {
		return 0, nil, err
	}
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(data, &core); err != nil || core.Status == 0 {
		return 0, nil, errors.New("no upstream status")
	}
	return core.Status, core.Body, nil
}

// registryEntityFor matches the config id to the registry unique_id — the
// real entity_id, never the slug (relay-api.md post-write verification).
func registryEntityFor(s *relaySession, domain, configID string) string {
	data, err := s.readFresh("ws", map[string]any{"type": "config/entity_registry/list"}, relaySessionRequestTimeout)
	var rows []fullRegistryRow
	if err != nil || json.Unmarshal(data, &rows) != nil {
		return ""
	}
	for _, row := range rows {
		if row.Platform == domain && row.UniqueID == configID && strings.HasPrefix(row.EntityID, domain+".") {
			return row.EntityID
		}
	}
	return ""
}
