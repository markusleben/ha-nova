package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Dashboard execution (dashboard-write spec): config snapshot, a fresh
// re-read right before the save, one create (a new shell), exactly one save
// or delete, a fresh read-back. An update lands on the undo stack.

func executeDashboard(paths runtimePaths, s *relaySession, env *verbEnvelope, action configAction, ctx configContext, noSnapshot bool) (err error) {
	sent := false
	defer afterSend(&sent, env, &err)
	s.renewBudget()
	create := action.Op != "delete" && action.Shell != nil
	if !create && !noSnapshot {
		data := any(ctx.current)
		if action.Op == "delete" { // shell + content: a later create can rebuild both
			data = map[string]any{"shell": action.Shell, "config": ctx.current}
		}
		if ctx.current != nil || action.Op == "delete" {
			file, err := s.saveConfigSnapshot("dashboards", snapshotName(action.ConfigID), data)
			if err != nil {
				return &verbFailure{exit: verbExitError, code: "SNAPSHOT_FAILED",
					message: fmt.Sprintf("no config snapshot could be taken (%s); nothing was changed and the plan stays valid", err),
					next:    "Prune the snapshot store, take a full backup (ha-nova:backup), or — if the user chooses to — apply again with --no-snapshot.",
					cause:   errSnapshotFailed}
			}
			env.Snapshot = file
		}
	}
	// The snapshot took a round trip: re-read right before the one write, so
	// an edit made in the UI since the recompute is never overwritten.
	if !create {
		live, err := readDashboardConfig(s, action.ConfigID, action.Default, true)
		if err != nil {
			return fmt.Errorf("%w; nothing was saved", err)
		}
		if canonicalJSON(live) != canonicalJSON(ctx.current) {
			return &verbFailure{exit: verbExitDrift, status: "drift", code: "PLAN_DRIFT",
				message: "the dashboard changed since the preview; nothing was saved and the plan is used up",
				next:    "Plan again from the live dashboard, show the new preview, and ask again."}
		}
	}
	if create {
		var shell map[string]any
		_ = json.Unmarshal(action.Shell, &shell)
		shell["type"] = "lovelace/dashboards/create"
		sent = true
		if _, err := s.write("ws", shell); err != nil {
			return dashboardCreateFailure(s, env, action.ConfigID, err)
		}
	}
	var payload map[string]any
	if action.Op == "delete" {
		var entry dashboardEntry
		_ = json.Unmarshal(action.Shell, &entry)
		payload = map[string]any{"type": "lovelace/dashboards/delete", "dashboard_id": entry.ID}
	} else {
		payload = dashboardConfigPayload("lovelace/config/save", action.ConfigID, action.Default)
		payload["config"] = action.Body // raw bytes: the draft as given
	}
	sent = true
	if _, err = s.write("ws", payload); err != nil {
		return dashboardWriteFailure(paths, s, env, action, ctx, create, err)
	}
	s.renewBudget()
	row, _, _ := verifyDashboard(s, action)
	dashboardLanded(paths, s, env, action, ctx)
	env.Verification, env.Status, env.exit = []verifyRow{row}, row.Outcome, verbExitOK
	if row.Outcome != "verified" {
		env.exit = verbExitUnverified
	}
	if strings.HasPrefix(row.Note, readBackDiffers) {
		sayReadBackDiffers(env)
	}
	return nil
}

// dashboardLanded keeps the way back whenever the write may have landed: an
// update's checkpoint and before_config, a delete's restore.
func dashboardLanded(paths runtimePaths, s *relaySession, env *verbEnvelope, action configAction, ctx configContext) {
	switch {
	case action.Op == "delete" && env.Snapshot != "":
		env.Restore = dashboardRestore(action, ctx.current, env.Snapshot, s.server)
	case action.Op != "delete" && ctx.current != nil:
		env.BeforeConfig = ctx.current
		saveCheckpoint(paths, s, env, action, ctx, action.Body)
	}
}

// rejectedByHA: Home Assistant itself answered the command with an error
// (it got the write); any other Relay error never reached it.
func rejectedByHA(err error) (*relayCallError, bool) {
	var call *relayCallError
	return call, errors.As(err, &call) && call.code == "UPSTREAM_WS_COMMAND_ERROR"
}

const readBackNext = "Read the dashboard back (`ha-nova get --dashboard %s`) and tell the user what it holds; never retry automatically."

// dashboardWriteFailure reports a failed save or delete by what Home
// Assistant holds afterwards, never by the error alone: it swaps its
// in-memory config before the disk write, and a delete may land anyway.
func dashboardWriteFailure(paths runtimePaths, s *relaySession, env *verbEnvelope, action configAction, ctx configContext, create bool, err error) error {
	var unknown *relayOutcomeUnknownError
	empty := ""
	if create {
		empty = "the new dashboard " + action.ConfigID + " exists, "
	}
	call, answered := rejectedByHA(err)
	switch {
	case errors.As(err, &unknown):
		dashboardLanded(paths, s, env, action, ctx)
		env.OutcomeUnknown = true
		return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN", message: empty + err.Error(), next: fmt.Sprintf(readBackNext, action.ConfigID)}
	case !answered && create:
		return &verbFailure{exit: verbExitUnverified, status: "unverified", code: "CONFIG_ERROR",
			message: empty + "but its content never reached Home Assistant (" + err.Error() + "): it is empty",
			next:    "Plan write again without --create, or delete it."}
	case !answered:
		return fmt.Errorf("%w; Home Assistant did not receive the write", err)
	}
	s.renewBudget()
	row, readback, readOK := verifyDashboard(s, action)
	env.Verification = []verifyRow{row}
	reason := "Home Assistant answered with an error (" + truncate(call.message, 300) + ")"
	unchanged := readOK && (action.Op == "delete" && row.Outcome != "verified" ||
		action.Op != "delete" && !dashboardDiffers(orEmpty(ctx.current), orEmpty(readback)) && (ctx.current == nil) == (readback == nil))
	switch {
	case row.Outcome == "verified": // it landed anyway
		dashboardLanded(paths, s, env, action, ctx)
		landed := "it now serves the planned config (it may not survive a restart)"
		if action.Op == "delete" {
			landed = "the dashboard is gone"
		}
		return &verbFailure{exit: verbExitUnverified, status: "unverified", code: "CONFIG_REJECTED", message: reason + ", yet " + landed,
			next: "Do not plan again: tell the user, check Home Assistant's log and storage, and read it back (`ha-nova get --dashboard " + action.ConfigID + "`) after a restart."}
	case !readOK:
		dashboardLanded(paths, s, env, action, ctx)
		env.OutcomeUnknown = true
		return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN",
			message: empty + reason + ", and the read-back failed: whether it changed is unknown", next: fmt.Sprintf(readBackNext, action.ConfigID)}
	case unchanged:
		what := "nothing was saved"
		if action.Op == "delete" {
			what = "nothing was deleted"
		} else if create {
			what = empty + "but it is empty"
		}
		return &verbFailure{exit: verbExitUnverified, status: "failed", code: "CONFIG_REJECTED", message: reason + "; " + what,
			next: "Fix the cause, plan again, and show the new preview."}
	}
	return &verbFailure{exit: verbExitUnverified, status: "unverified", code: "CONFIG_REJECTED",
		message: reason + "; the dashboard now holds a config that is neither the planned nor the previous one", next: fmt.Sprintf(readBackNext, action.ConfigID)}
}

func orEmpty(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// dashboardCreateFailure reports a failed shell create by the dashboard list.
func dashboardCreateFailure(s *relaySession, env *verbEnvelope, urlPath string, err error) error {
	var unknown *relayOutcomeUnknownError
	call, answered := rejectedByHA(err)
	switch {
	case errors.As(err, &unknown):
		env.OutcomeUnknown = true
		return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN", message: err.Error(),
			next: "Check whether the dashboard exists (`ha-nova get --dashboards`); never retry automatically."}
	case !answered:
		return fmt.Errorf("%w; Home Assistant did not receive the write", err)
	}
	s.renewBudget()
	entries, listErr := readDashboardsFresh(s)
	reason := "Home Assistant answered with an error (" + truncate(call.message, 300) + ")"
	switch {
	case listErr != nil:
		env.OutcomeUnknown = true
		return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN", message: reason + ", and the dashboard list could not be read",
			next: "Check whether the dashboard exists (`ha-nova get --dashboards`); never retry automatically."}
	case findDashboard(entries, urlPath) != nil:
		return &verbFailure{exit: verbExitUnverified, status: "unverified", code: "CONFIG_REJECTED", message: reason + ", yet the dashboard " + urlPath + " exists; its content was not saved",
			next: "It is empty: plan write again without --create, or delete it."}
	}
	return &verbFailure{exit: verbExitUnverified, status: "failed", code: "CONFIG_REJECTED", message: reason + "; the dashboard was not created",
		next: "Fix the cause, plan again, and show the new preview."}
}

func readDashboardsFresh(s *relaySession) ([]dashboardEntry, error) {
	data, err := s.readFresh("ws", map[string]any{"type": "lovelace/dashboards/list"}, relaySessionRequestTimeout)
	var entries []dashboardEntry
	if err == nil {
		err = json.Unmarshal(data, &entries)
	}
	return entries, err
}

// verifyDashboard: a save is verified when the fresh read-back equals the
// draft (a create also needs the dashboard listed); a delete when the
// dashboard is no longer listed. readOK is false when Home Assistant could
// not be read: then nothing about the outcome is known.
func verifyDashboard(s *relaySession, action configAction) (verifyRow, json.RawMessage, bool) {
	row := verifyRow{EntityID: "dashboard." + action.ConfigID, Outcome: "unverified"}
	listed := func() (bool, error) {
		entries, err := readDashboardsFresh(s)
		return findDashboard(entries, action.ConfigID) != nil, err
	}
	if action.Op == "delete" {
		row.Expect = "dashboard no longer listed"
		present, err := listed()
		row.Observed = fmt.Sprintf("listed: %v", present)
		if err != nil { // an unanswered read is not an absence
			row.Observed = "read failed"
			return row, nil, false
		}
		if !present {
			row.Outcome = "verified"
		}
		return row, nil, true
	}
	row.Expect = "read-back equals the plan"
	readback, err := readDashboardConfig(s, action.ConfigID, action.Default, true)
	changes, compareErr := configChanges(action.Body, readback)
	switch {
	case err != nil:
		row.Observed = "read-back failed"
		return row, nil, false
	case readback == nil:
		row.Observed, row.Note = "read-back differs", "no stored dashboard config was found"
	case compareErr != nil:
		row.Observed, row.Note = "read-back differs", "the stored dashboard could not be compared with the plan"
	case len(changes) > 0:
		row.Observed, row.Note = "read-back differs", readBackDiffers+strings.Join(changes, " ")
	default:
		row.Observed, row.Outcome = "read-back equals the plan", "verified"
	}
	if action.Shell != nil && row.Outcome == "verified" {
		if present, err := listed(); err != nil || !present {
			row.Outcome, row.Note = "unverified", "the content is saved, but the new dashboard is not listed"
		}
	}
	return row, readback, true
}

// dashboardDiffers compares content like the revert drift check: numbers by
// value (1.5 = 1.50), key order ignored.
func dashboardDiffers(planned, readback json.RawMessage) bool {
	changes, err := configChanges(planned, readback)
	return err != nil || len(changes) > 0
}

// dashboardRestore names the way back from a delete: load the snapshot, then
// a create from its shell and config. The default dashboard comes back as a
// plain write (its url_path has no hyphen; once deleted it is the unlisted
// default again).
func dashboardRestore(action configAction, current json.RawMessage, snapshot, server string) string {
	var entry struct {
		URLPath      string `json:"url_path"`
		Title        string `json:"title"`
		RequireAdmin bool   `json:"require_admin"`
		ShowSidebar  *bool  `json:"show_in_sidebar"`
	}
	_ = json.Unmarshal(action.Shell, &entry)
	command := "load " + snapshot + " (relay backups), save its config to a file, then: ha-nova plan write --domain dashboard --id " + entry.URLPath
	if entry.URLPath == defaultDashboard {
		command += " --base <config_digest from get --dashboard " + defaultDashboard + ">"
	} else {
		command += " --create --title " + strconv.Quote(entry.Title) + " [--icon <shell.icon>]"
		if entry.RequireAdmin {
			command += " --require-admin"
		}
		if entry.ShowSidebar != nil && !*entry.ShowSidebar {
			command += " --hide-sidebar"
		}
	}
	if current == nil { // it had no saved content: an empty object recreates it
		command = strings.Replace(command, "save its config to a file", "write a file holding {} (it had no saved content)", 1)
	}
	command += " --file <config file>"
	if server != "" {
		command += " --server " + server
	}
	return command
}
