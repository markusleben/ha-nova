package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
)

// Config execution (typed-verbs spec §2.3 config writes): one write, a
// read-back compared with the plan, the wait for Home Assistant's own reload
// (awaitLoaded), the registry entity and its state. A delete is preceded by
// a pre-delete config snapshot and followed by a reload.

// errSnapshotFailed: nothing was deleted or saved, so apply releases the
// plan (a typed code stays valid for --no-snapshot).
var errSnapshotFailed = errors.New("pre-write snapshot failed")

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

func executeConfig(paths runtimePaths, s *relaySession, env *verbEnvelope, action configAction, ctx configContext, noSnapshot bool) (err error) {
	if action.Domain == "dashboard" {
		return executeDashboard(paths, s, env, action, ctx, noSnapshot)
	}
	sent := false
	defer afterSend(&sent, env, &err)
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
	var added snapshotSaveReceipt
	if ctx.calm { // an ordinary update: its way back first, or nothing is sent
		if added, err = addUndoSnapshot(paths, checkpointRecord(s, action, ctx, action.Body)); err != nil {
			return &verbFailure{exit: verbExitError, code: "UNDO_SAVE_FAILED",
				message: fmt.Sprintf("the checkpoint to put it back could not be saved first (%s); nothing was sent", err),
				next:    "Plan the change again; it will show a preview to confirm."}
		}
	}
	var result serviceResult
	sent = true
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
	createdID := ""
	if action.Op == "write" && ctx.current == nil { // a restore retries as itself: CONFIG_ID_IN_USE once it landed
		createdID = action.ConfigID
	}
	if action.Op == "delete" && env.Snapshot != "" { // the delete was sent: its way back, whatever comes next
		env.Restore = restoreCommand(action, env.Snapshot, s.server)
	}
	if failure := writeFailure(env, result, err, createdID); failure != nil && !alreadyGone {
		switch {
		case ctx.calm && (errors.As(err, &unknown) || err == nil && !rejectedStatuses[result.status]): // it may have landed: the checkpoint saved first stays, with the way back
			env.BeforeConfig, env.Undo = ctx.current, &added
		case ctx.calm: // nothing landed: no way back to a state that never changed
			if err := dropUndoSnapshot(paths, s.server, added); err != nil {
				env.Notices = append(env.Notices, verbNotice{Kind: "undo", Text: fmt.Sprintf("the checkpoint saved for this update stays although nothing changed (%s); the next update of it asks first", err)})
			}
		case errors.As(err, &unknown) && update:
			saveCheckpoint(paths, s, env, action, ctx, action.Body)
		}
		return failure
	}
	if action.Op == "delete" { // Home Assistant's own hook after a delete only drops the registry entry
		_, _ = s.postService(action.Domain+".reload", configReloadData(action.Domain, action.ConfigID), false)
	}
	s.renewBudget()
	row, readback := verifyConfig(s, action)
	if alreadyGone && row.Outcome == "verified" {
		row.Note = "already gone when the delete arrived (removed concurrently)"
	}
	env.Verification, env.Status, env.exit = []verifyRow{row}, row.Outcome, verbExitOK
	if row.Outcome != "verified" {
		env.exit = verbExitUnverified
	}
	switch {
	case strings.HasPrefix(row.Note, readBackDiffers):
		sayReadBackDiffers(env)
	case strings.HasPrefix(row.Note, notLoaded):
		env.NextStep = "Tell the user it is saved but not yet confirmed running. Read it once more in a moment (`ha-nova get " + row.EntityID + " --config` and its state); never apply it again."
	}
	if update {
		expected := readback
		if expected == nil {
			expected = action.Body
		}
		if !ctx.calm {
			saveCheckpoint(paths, s, env, action, ctx, expected)
		} else { // the checkpoint saved first stays the way back if this refresh failed; one Home Assistant kept differently is no calm step
			_ = refreshUndoSnapshot(paths, s.server, added, expected, !strings.HasPrefix(row.Note, readBackDiffers))
			env.Undo, env.undoable = &added, env.Status == "verified"
		}
	}
	return nil
}

// configReloadData: the scope Home Assistant's own config hook reloads.
func configReloadData(domain, configID string) map[string]any {
	if domain == "automation" {
		return map[string]any{"id": configID}
	}
	return map[string]any{}
}

// saveCheckpoint puts an update (a revert is one too, as in update-revert.md)
// on this server's undo stack: one step back to ctx.current.
func saveCheckpoint(paths runtimePaths, s *relaySession, env *verbEnvelope, action configAction, ctx configContext, expectedAfter json.RawMessage) {
	if receipt, err := saveUndoSnapshotBytes(paths, checkpointRecord(s, action, ctx, expectedAfter)); err == nil {
		env.Undo = &receipt
	} else {
		env.Notices = append(env.Notices, verbNotice{Kind: "undo", Text: "the update is not revertible: " + err.Error()})
	}
}

func checkpointRecord(s *relaySession, action configAction, ctx configContext, expectedAfter json.RawMessage) []byte {
	record, _ := json.Marshal(map[string]any{"op": "update", "domain": action.Domain, "target_id": action.ConfigID, "server": s.server,
		"name": ctx.alias, "before_config": ctx.current, "expected_after": expectedAfter, "calm": ctx.calm})
	return record
}

// writeFailure maps one config write's outcome like a service call's;
// createdID names a create's config id (a retry must reuse it).
func writeFailure(env *verbEnvelope, result serviceResult, err error, createdID string) error {
	var unknown *relayOutcomeUnknownError
	switch {
	case errors.As(err, &unknown):
		env.OutcomeUnknown = true
		next := "Read the config back; never retry automatically."
		if createdID != "" {
			next += " If it is missing and the user asks to retry, retry only with the same --id " + createdID + " --create."
		}
		return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN", message: err.Error(), next: next}
	case err != nil: // refused or answered before reaching Home Assistant
		return nothingSent(err)
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
// planned body (normalized), Home Assistant has loaded it (awaitLoaded), and
// its registry entity has a state; a delete when config and entity are gone.
func verifyConfig(s *relaySession, action configAction) (verifyRow, json.RawMessage) {
	row := verifyRow{EntityID: action.EntityID, Outcome: "unverified"}
	status, readback, err := coreGETFresh(s, action.Path)
	if action.Op == "delete" {
		row.Expect = "config and entity gone"
		st, stateErr := s.stateFresh(action.EntityID, relaySessionRequestTimeout)
		row.Observed = fmt.Sprintf("config HTTP %d, entity present: %v", status, st != nil)
		if err != nil || stateErr != nil { // an unanswered read is not an absence
			row.Observed = "read failed"
		}
		if err == nil && stateErr == nil && status == http.StatusNotFound && st == nil {
			row.Outcome = "verified"
		}
		return row, nil
	}
	row.Expect = "read-back equals the plan, loaded by Home Assistant, entity present"
	differences, parseErr := configChanges(action.Body, readback)
	if err != nil || status != http.StatusOK || parseErr != nil {
		row.Observed = fmt.Sprintf("read-back failed (HTTP %d)", status)
		if err != nil {
			row.Observed = "read-back failed"
		}
		return row, nil
	}
	wait := configLoadWait
	switch {
	case len(differences) > 0: // never the plan: one look for the entity, no wait
		wait = 0
	case s.approval() == nil: // an MCP or chat apply job has its own life (gateOpenWait); a spent budget still gets a short look
		wait = min(wait, max(s.askWait(), configLoadFloor))
	}
	entityID, disabled, loaded, waited, registryErr := awaitLoaded(s, action, wait)
	row.EntityID = entityID
	var st *entityState
	var stateErr error
	if loaded {
		st, stateErr = s.stateFresh(entityID, relaySessionRequestTimeout)
	}
	present := fmt.Sprint(st != nil)
	switch {
	case registryErr != nil: // an unanswered read is not an absence
		present = "registry read failed"
	case stateErr != nil:
		present = "read failed"
	}
	row.Observed = fmt.Sprintf("%d differences, loaded: %v, entity: %q present: %s", len(differences), loaded, entityID, present)
	switch {
	case len(differences) > 0:
		row.Note = readBackDiffers + strings.Join(differences, " ")
	case disabled: // a registry-disabled item has no state to check
		row.Outcome, row.Note = "verified", "disabled in the entity registry: saved, not running"
	case !loaded && registryErr != nil:
		row.Note = notLoaded + "HA NOVA couldn't read whether Home Assistant loaded it: whether the change is running is unknown"
	case !loaded:
		row.Note = fmt.Sprintf(notLoaded+"Home Assistant hadn't loaded it within %d s: whether the change is running is unknown", int(math.Ceil(waited.Seconds())))
	case st != nil && st.State == "unavailable": // Home Assistant could not load the config
		row.Note = "the entity is unavailable: Home Assistant rejected the config when loading it"
	case st != nil:
		row.Outcome = "verified"
	}
	return row, readback
}

const readBackDiffers = "the read-back differs: "

// notLoaded: saved, but not seen running (awaitLoaded); never applied again.
const notLoaded = "saved, but "

// sayReadBackDiffers: what Home Assistant kept is not what the user
// approved. Equivalent spellings never get here (configChanges), so this is
// a real difference the agent must not explain away.
func sayReadBackDiffers(env *verbEnvelope) {
	env.Say = sayReadBackDiffersText
	env.NextStep = "Show `say`, then what differs (verification[].note; each row: field | approved | saved) in plain words. It is a real difference: never call it harmless or say it works anyway. Read the saved version again and offer a new plan to fix it."
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
func registryEntityFor(s *relaySession, domain, configID string) (string, bool, error) {
	data, err := s.readFresh("ws", map[string]any{"type": "config/entity_registry/list"}, relaySessionRequestTimeout)
	var rows []fullRegistryRow
	if err == nil {
		err = json.Unmarshal(data, &rows)
	}
	if err != nil {
		return "", false, err
	}
	for _, row := range rows {
		if row.Platform == domain && string(row.UniqueID) == configID && strings.HasPrefix(row.EntityID, domain+".") {
			return row.EntityID, row.DisabledBy != "", nil
		}
	}
	return "", false, nil
}

// applyDone: what a config or rooms apply does, for its result line (a
// call words its read-back instead).
func applyDone(e *tierEngine, env *verbEnvelope, plan storedPlan, ctx configContext) string {
	switch {
	case plan.Kind == "rooms" && plan.RoomsRequest.Undo:
		return "put the devices back where they were"
	case plan.Kind == "rooms":
		return "put the devices in their rooms"
	case plan.ConfigAction == nil:
		return ""
	}
	return sayPast(sayGoal(e, env, plan, cmp.Or(ctx.alias, configAlias(nil, ctx.current))))
}
