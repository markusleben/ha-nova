package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	update := action.Op != "delete" && ctx.current != nil
	if create {
		var shell map[string]any
		_ = json.Unmarshal(action.Shell, &shell)
		shell["type"] = "lovelace/dashboards/create"
		sent = true
		if _, err := s.write("ws", shell); err != nil {
			return dashboardWriteFailure(env, err, "the dashboard was not created", action.ConfigID)
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
	_, err = s.write("ws", payload)
	var unknown *relayOutcomeUnknownError
	if update && (err == nil || errors.As(err, &unknown)) { // it may have landed: keep the way back
		saveCheckpoint(paths, s, env, action, ctx, action.Body)
	}
	if err != nil {
		what := "nothing was saved"
		if action.Op == "delete" {
			what = "nothing was deleted"
		}
		var call *relayCallError
		if action.Op != "delete" && !errors.As(err, &unknown) && errors.As(err, &call) {
			// Home Assistant swaps its in-memory config before the disk write:
			// a rejection is reported with what it serves now.
			s.renewBudget()
			row := verifyDashboard(s, action)
			env.Verification = []verifyRow{row}
			if row.Outcome == "verified" {
				what = "yet Home Assistant now serves the planned config (it may not survive a restart)"
				if update {
					saveCheckpoint(paths, s, env, action, ctx, action.Body)
				}
			}
		}
		failure := dashboardWriteFailure(env, err, what, action.ConfigID)
		var shown *verbFailure
		if create && errors.As(failure, &shown) && shown.exit != verbExitUnknown {
			// The shell landed: never "nothing happened".
			shown.exit, shown.status = verbExitUnverified, "unverified"
			shown.message = "the dashboard " + action.ConfigID + " was created but its content was not saved (" + err.Error() + ")"
			shown.next = "It exists and is empty: plan write again without --create, or delete it."
		} else if create && shown == nil {
			return &verbFailure{exit: verbExitUnverified, status: "unverified", code: "CONFIG_ERROR",
				message: "the dashboard " + action.ConfigID + " was created but its content was not saved (" + err.Error() + ")",
				next:    "It exists and is empty: plan write again without --create, or delete it."}
		}
		return failure
	}
	s.renewBudget()
	row := verifyDashboard(s, action)
	if action.Op == "delete" && env.Snapshot != "" {
		env.Restore = dashboardRestore(action, env.Snapshot, s.server)
	}
	env.Verification, env.Status, env.exit = []verifyRow{row}, row.Outcome, verbExitOK
	if row.Outcome != "verified" {
		env.exit = verbExitUnverified
	}
	return nil
}

// dashboardWriteFailure maps one dashboard write's outcome: Home Assistant's
// own error rejected it; a transport failure after sending is unknown.
func dashboardWriteFailure(env *verbEnvelope, err error, rejected, urlPath string) error {
	var unknown *relayOutcomeUnknownError
	var call *relayCallError
	switch {
	case errors.As(err, &unknown):
		env.OutcomeUnknown = true
		return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN", message: err.Error(),
			next: "Read the dashboard back (`ha-nova get --dashboard " + urlPath + "`) before anything else; never retry automatically."}
	case errors.As(err, &call) && call.code == "UPSTREAM_WS_COMMAND_ERROR":
		return &verbFailure{exit: verbExitUnverified, status: "failed", code: "CONFIG_REJECTED",
			message: "Home Assistant rejected it: " + truncate(call.message, 300) + "; " + rejected,
			next:    "Fix the cause, plan again, and show the new preview."}
	}
	return fmt.Errorf("%w; Home Assistant did not receive the write", err)
}

// verifyDashboard: a save is verified when the fresh read-back equals the
// draft (a create also needs the dashboard listed); a delete when the
// dashboard is no longer listed.
func verifyDashboard(s *relaySession, action configAction) verifyRow {
	row := verifyRow{EntityID: "dashboard." + action.ConfigID, Outcome: "unverified"}
	listed := func() (bool, error) {
		data, err := s.readFresh("ws", map[string]any{"type": "lovelace/dashboards/list"}, relaySessionRequestTimeout)
		var entries []dashboardEntry
		if err == nil {
			err = json.Unmarshal(data, &entries)
		}
		return findDashboard(entries, action.ConfigID) != nil, err
	}
	if action.Op == "delete" {
		row.Expect = "dashboard no longer listed"
		present, err := listed()
		row.Observed = fmt.Sprintf("listed: %v", present)
		if err != nil { // an unanswered read is not an absence
			row.Observed = "read failed"
		} else if !present {
			row.Outcome = "verified"
		}
		return row
	}
	row.Expect = "read-back equals the plan"
	readback, err := readDashboardConfig(s, action.ConfigID, action.Default, true)
	switch {
	case err != nil:
		row.Observed = "read-back failed"
	case readback == nil || dashboardDiffers(action.Body, readback):
		row.Observed, row.Note = "read-back differs", "the stored dashboard is not the planned one (changed by someone else, or adjusted by Home Assistant)"
	default:
		row.Observed, row.Outcome = "read-back equals the plan", "verified"
	}
	if action.Shell != nil && row.Outcome == "verified" {
		if present, err := listed(); err != nil || !present {
			row.Outcome, row.Note = "unverified", "the content is saved, but the new dashboard is not listed"
		}
	}
	return row
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
func dashboardRestore(action configAction, snapshot, server string) string {
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
	command += " --file <config file>"
	if server != "" {
		command += " --server " + server
	}
	return command
}
