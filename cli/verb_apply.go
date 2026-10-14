package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const callUsage = `ha-nova call <domain.service> [<entity_id>…] [--match "<phrase>"] [--area <area_id>] [--device <device_id>] [--set k=v]… [--data-file <file>]`

const callHelp = callUsage + `

Plans the call like "plan call". An ordinary call (one unambiguous target,
nothing behind it) runs now — exactly one request, never retried — and is
verified. Anything else stores the plan and exits 3; run "ha-nova apply"
after the user confirms the preview (or types the code).
Exit: 0 verified/accepted, 3 needs confirmation, 4 refused, 5 ambiguous or
not found, 6 unverified/failed, 7 outcome unknown (verify; never retry).`

const applyUsage = "ha-nova apply --plan <id> [--confirm <code>]"

const applyHelp = applyUsage + `

Executes a stored plan once. Apply claims the plan, checks expiry and server,
re-plans from the stored request, and sends nothing when anything changed
(exit 2 with a new plan). A preview plan needs no code: running apply IS the
confirmation step, so run it only after the user confirmed that preview. A
typed plan needs --confirm with the exact code the user typed; three wrong
codes invalidate the plan.
Exit: 0 verified/accepted, 2 drift, 4 refused/missing/expired/wrong code,
5 target gone, 6 unverified/failed, 7 outcome unknown (verify; never retry).`

var planIDPattern = regexp.MustCompile(`^p_[0-9a-f]{12}$`)

// reauthSettle: Home Assistant opens a reauth flow asynchronously after the
// failing call; one re-read after this pause correlates it.
var reauthSettle = 2 * time.Second

func runCallCommand(paths runtimePaths, args []string) int {
	return runCallVerb(paths, "call", "call", callHelp, args, func(s *relaySession, env *verbEnvelope, req planCallRequest) error {
		e, plan, err := computeCallPlan(s, env, req)
		if err != nil {
			return err
		}
		if e.level != tierOrdinary {
			return savePlan(paths, plan) // exit 3: confirmation through apply
		}
		env.Plan = nil // fast path: the plan is never stored, so no id to apply
		return executeAction(s, e, env, plan.Action)
	})
}

func runApplyCommand(paths runtimePaths, args []string) int {
	var opts verbOptions
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	id := fs.String("plan", "", "plan id from plan/call (p_ + 12 hex)")
	code := fs.String("confirm", "", "the exact confirmation code the user typed (typed plans only)")
	positional, err := parseVerbArgs(fs, &opts, args, applyHelp)
	if err == nil && (len(positional) != 0 || !planIDPattern.MatchString(*id)) {
		err = errors.New("Usage: " + applyUsage)
	}
	if err != nil {
		return verbUsageError("apply", err)
	}
	env, exit := applyPlan(paths, opts, *id, *code)
	if !printVerbEnvelope(env) {
		return verbExitError
	}
	return exit
}

func refused(code, message string) *verbFailure {
	return &verbFailure{exit: verbExitRefused, code: code, message: message + "; nothing was sent", next: "Plan again: ha-nova plan call … or ha-nova call …"}
}

// applyPlan runs the spec §2.1 apply order: claim (I8), expiry, server,
// empty-target guard, code (I2), recompute (I3), one execution (I6, I7).
func applyPlan(paths runtimePaths, opts verbOptions, id, code string) (env verbEnvelope, exit int) {
	env = verbEnvelope{SchemaVersion: 1, Verb: "apply", Status: "ok"}
	fail := func(err error) (verbEnvelope, int) {
		exit := setVerbError(&env, err)
		return env, exit
	}
	open := filepath.Join(planDir(paths), id+".json")
	peek, err := readPlanFile(open)
	if err != nil {
		return fail(refused("PLAN_NOT_FOUND", "plan "+id+" is missing, expired, or already used"))
	}
	if opts.serverSet && opts.server != peek.Server {
		return fail(refused("SERVER_MISMATCH", fmt.Sprintf("plan %s belongs to server %q, not %q", id, peek.Server, opts.server)))
	}
	opts.server, opts.serverSet = peek.Server, true
	s, err := newRelaySession(paths, opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()
	env.Server = s.server
	defer func() { env.Notices = verbNotices(paths, s.version()) }()

	// Claim: one atomic rename; whoever loses sends nothing (I8).
	claimed := filepath.Join(planDir(paths), fmt.Sprintf("%s.claimed-%d", id, os.Getpid()))
	if err := os.Rename(open, claimed); err != nil {
		return fail(refused("PLAN_CLAIMED", "plan "+id+" was already claimed or used"))
	}
	consumed := true // a claimed plan is never reused, except to retry a mistyped code
	defer func() {
		if consumed {
			_ = os.Remove(claimed)
		}
	}()
	plan, err := readPlanFile(claimed)
	expires, timeErr := time.Parse(time.RFC3339, plan.ExpiresAt)
	request := plan.Request
	named := len(request.EntityIDs) > 0 || request.Match != "" || request.Area != "" || request.Device != ""
	switch {
	case err != nil || timeErr != nil || plan.ID != id:
		return fail(refused("PLAN_INVALID", "plan "+id+" cannot be read"))
	case !verbNow().Before(expires):
		return fail(refused("PLAN_EXPIRED", "plan "+id+" expired at "+plan.ExpiresAt))
	case plan.Server != s.server:
		return fail(refused("SERVER_MISMATCH", fmt.Sprintf("plan %s belongs to server %q", id, plan.Server)))
	case named && len(plan.Action.EntityIDs) == 0:
		return fail(refused("UNRESOLVED_TARGET", "the plan named targets but froze none"))
	}
	if plan.Tier == tierTyped.String() {
		switch typed := strings.TrimSpace(code); {
		case typed == "":
			consumed = false
			_ = os.Rename(claimed, open)
			return fail(refused("CODE_REQUIRED", "this plan needs the confirmation code the user types"))
		case typed != plan.Code:
			if plan.Mismatches++; plan.Mismatches >= 3 {
				return fail(refused("PLAN_INVALIDATED", "three wrong confirmation codes; the plan is gone"))
			}
			if writePlanFile(claimed, plan) == nil && os.Rename(claimed, open) == nil {
				consumed = false
			}
			return fail(refused("CODE_MISMATCH", fmt.Sprintf("the confirmation code does not match (%d of 3 attempts used)", plan.Mismatches)))
		}
	}

	// Recompute the whole plan from the stored request (I3).
	e, fresh, err := computeCallPlan(s, &env, request)
	if err != nil {
		return fail(err)
	}
	if fresh.Hash != plan.Hash {
		if err := savePlan(paths, fresh); err != nil {
			return fail(err)
		}
		return fail(&verbFailure{exit: verbExitDrift, status: "drift", code: "PLAN_DRIFT",
			message: "something changed since the preview; nothing was sent and the old plan is gone",
			next:    "Show the new preview (and code) and ask again."})
	}
	env.Plan = &verbPlan{ID: plan.ID, Hash: plan.Hash, ExpiresAt: plan.ExpiresAt, Confirm: plan.Confirm, Code: plan.Code}
	// Equal hashes mean equal actions; send the recomputed one, never a
	// field read back from the plan file.
	if err := executeAction(s, e, &env, fresh.Action); err != nil {
		return fail(err)
	}
	return env, env.exit
}

// executeAction sends the frozen action exactly once — the payload is the
// frozen entity_ids plus the planned data, nothing else (I6, I7) — then
// verifies it.
func executeAction(s *relaySession, e *tierEngine, env *verbEnvelope, action planAction) error {
	body := map[string]any{}
	for key, value := range action.Data {
		body[key] = value
	}
	if len(action.EntityIDs) > 0 {
		body["entity_id"] = slices.Clone(action.EntityIDs)
	}
	s.renewBudget()
	flowsBefore := s.reauthFlows() // best effort; nil means no correlation
	result, err := s.postService(action.Service, body)
	var unknown *relayOutcomeUnknownError
	switch {
	case errors.As(err, &unknown):
		return &verbFailure{exit: verbExitUnknown, status: "unknown", code: "OUTCOME_UNKNOWN", message: err.Error(), next: "Verify the target state; never retry automatically."}
	case err != nil:
		return fmt.Errorf("%w; Home Assistant did not receive the call", err)
	case result.status >= 400:
		if result.status >= 500 && flowsBefore != nil {
			time.Sleep(reauthSettle)
			for flow, handler := range s.reauthFlows() {
				if _, existed := flowsBefore[flow]; !existed {
					env.ReauthOpened = append(env.ReauthOpened, handler)
				}
			}
			slices.Sort(env.ReauthOpened)
		}
		return &verbFailure{exit: verbExitUnverified, status: "failed", code: "SERVICE_FAILED",
			message: fmt.Sprintf("Home Assistant answered HTTP %d: %s", result.status, truncate(string(result.body), 300)),
			next:    "Do not retry automatically; check the target and the integration."}
	}
	env.Verification, env.Status = verifyCall(s, e, action)
	env.exit = verbExitOK
	if env.Status == "unverified" || env.Status == "failed" {
		env.exit = verbExitUnverified
	}
	return nil
}

func truncate(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[:n] + "…"
}
