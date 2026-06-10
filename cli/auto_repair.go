package main

import (
	"fmt"
	"os/exec"
)

// autoRepairOutcome describes what auto-repair did for a single client.
type autoRepairOutcome struct {
	ClientID    string
	ClientLabel string
	Repaired    bool
	Skipped     bool
	SkipReason  string
	Err         error
}

// attemptClientAutoRepair tries to silently reinstall a client that has
// drifted out of an attached state. It is idempotent: when nothing needs
// repair, it returns Skipped=true with no error.
//
// Auto-repair is intentionally conservative:
//   - Skip if the client is already attached (no work needed).
//   - Skip if the client runtime is missing — the user must install the
//     client first.
//   - Skip Claude Code in dev/local marketplace mode — developers handle
//     their own setup explicitly.
//   - Skip Claude Code if the `claude` CLI is not on PATH — re-attach
//     requires it; falling through would surface a confusing error.
//   - Re-validate Claude state immediately before mutating: the caller's
//     status may be stale or based on a torn read of Claude's state files
//     (the session-start hook fires while Claude Code itself is writing
//     them). Unreadable state never drives a destructive reinstall.
//
// Failure to repair is reported but does not abort caller workflows.
func attemptClientAutoRepair(paths runtimePaths, client clientStatus) autoRepairOutcome {
	outcome := autoRepairOutcome{ClientID: client.ID, ClientLabel: client.Label}

	if client.Ready {
		outcome.Skipped = true
		outcome.SkipReason = "already ready"
		return outcome
	}
	if !client.RuntimeDetected {
		outcome.Skipped = true
		outcome.SkipReason = "client runtime not detected"
		return outcome
	}
	if client.Attached {
		outcome.Skipped = true
		outcome.SkipReason = "already attached"
		return outcome
	}

	state := loadStateOrDefault(paths)
	if normalizeInstallSource(detectInstallSource(paths, state)) == installSourceDev {
		outcome.Skipped = true
		outcome.SkipReason = "dev install mode"
		return outcome
	}

	sourceRoot := resolveSourceRoot(paths)

	if client.ID == "claude" {
		if _, err := exec.LookPath("claude"); err != nil {
			outcome.Skipped = true
			outcome.SkipReason = "claude CLI not on PATH"
			return outcome
		}
		snapshot := inspectClaudeInstallSnapshot(paths, state)
		if snapshot.StateUnreadable {
			outcome.Skipped = true
			outcome.SkipReason = "claude plugin state unreadable; skipped destructive repair"
			return outcome
		}
		if snapshot.Attached {
			outcome.Skipped = true
			outcome.SkipReason = "already attached"
			return outcome
		}
	}

	if _, err := installClient(paths, sourceRoot, client.ID); err != nil {
		outcome.Err = fmt.Errorf("auto-repair %s: %w", client.ID, err)
		return outcome
	}

	outcome.Repaired = true
	return outcome
}

// runClientAutoRepair iterates over configured clients and attempts to
// repair any whose attachment has drifted. Returns the per-client outcomes.
//
// Repairs are serialized across processes: the session-start hook fires one
// background run per starting client session, and concurrent runs would race
// on Claude's plugin state files.
//
// Callers (typically `ha-nova doctor --auto-repair`) decide how to render
// the outcomes. The function itself prints nothing.
func runClientAutoRepair(paths runtimePaths, statuses []clientStatus) []autoRepairOutcome {
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		outcomes := make([]autoRepairOutcome, 0, len(statuses))
		for _, status := range statuses {
			outcomes = append(outcomes, autoRepairOutcome{
				ClientID:    status.ID,
				ClientLabel: status.Label,
				Skipped:     true,
				SkipReason:  "another auto-repair run is in progress",
			})
		}
		return outcomes
	}
	defer release()

	outcomes := make([]autoRepairOutcome, 0, len(statuses))
	for _, status := range statuses {
		outcomes = append(outcomes, attemptClientAutoRepair(paths, status))
	}
	return outcomes
}
