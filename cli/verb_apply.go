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

Plans the call like "plan call". An ordinary call — up to 10 named targets
or a targetless call, nothing behind it — runs now: exactly one request,
never retried, then verified. Anything else stores the plan and exits 3; run "ha-nova apply"
after the user confirms the preview (or types the code).
Exit: 0 verified/accepted, 3 needs confirmation, 4 refused, 5 ambiguous or
not found, 6 unverified/failed, 7 outcome unknown (verify; never retry).`

const applyUsage = "ha-nova apply --plan <id> [--confirm <code>]"

const applyHelp = applyUsage + `

Executes a stored plan once. Apply claims the plan, checks expiry and server,
re-plans from the stored request, and sends nothing when anything changed
(exit 2 with a new plan). A preview plan needs no code: running apply IS the
confirmation step, so run it only after the user confirmed that preview. A
typed plan needs --confirm with the exact code the user typed (three wrong
codes discard the plan; that counter guards against slips, not tampering).
Config plans (plan write/delete/revert) save the config once, reload, and
verify the read-back; an update lands on the undo stack, and a delete takes
a config snapshot first (--no-snapshot only after the user chose that).
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
		if frozeNothing(req, plan.Action) { // defense in depth: such a plan is typed anyway
			return refused("UNRESOLVED_TARGET", "the call named targets but froze none")
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
	noSnapshot := fs.Bool("no-snapshot", false, "delete plans: proceed without the pre-delete config snapshot (only after the user chose that)")
	positional, err := parseVerbArgs(fs, &opts, args, applyHelp)
	if err == nil && (len(positional) != 0 || !planIDPattern.MatchString(*id)) {
		err = errors.New("Usage: " + applyUsage)
	}
	if err != nil {
		return verbUsageError("apply", err)
	}
	env, exit := applyPlan(paths, opts, *id, *code, *noSnapshot)
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
func applyPlan(paths runtimePaths, opts verbOptions, id, code string, noSnapshot bool) (env verbEnvelope, exit int) {
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
	request, config := plan.Request, plan.ConfigRequest
	// The file is untrusted beyond its hash: kind, version, and the stored
	// request are re-validated exactly as a fresh plan would be.
	valid := err == nil && timeErr == nil && plan.ID == id && plan.SchemaVersion == 1
	isCall := valid && plan.Kind == "call" && config == nil && request.validate() == nil
	isConfig := valid && config != nil && config.Op == plan.Kind && config.ConfigID != "" && config.validate() == nil
	switch {
	case !isCall && !isConfig:
		return fail(refused("PLAN_INVALID", "plan "+id+" cannot be read or is not a valid plan"))
	case isCall && staticRefusal(request) != nil:
		return fail(refuse(&env, *staticRefusal(request)))
	case !verbNow().Before(expires):
		return fail(refused("PLAN_EXPIRED", "plan "+id+" expired at "+plan.ExpiresAt))
	case plan.Server != s.server:
		return fail(refused("SERVER_MISMATCH", fmt.Sprintf("plan %s belongs to server %q", id, plan.Server)))
	case isCall && frozeNothing(request, plan.Action):
		return fail(refused("UNRESOLVED_TARGET", "the plan named targets but froze none"))
	}

	// Recompute the whole plan from the stored request (I3).
	var e *tierEngine
	var fresh storedPlan
	var ctx configContext
	if isCall {
		e, fresh, err = computeCallPlan(s, &env, request)
	} else {
		e, fresh, ctx, err = computeConfigPlan(paths, s, &env, *config)
	}
	var expected *verbFailure
	if err != nil && !errors.As(err, &expected) && os.Rename(claimed, open) == nil {
		consumed = false // the Relay is unreachable: nothing was sent, the plan stays usable
	}
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
	// Identity, tier, and code all come from the recomputed plan and the
	// stored nonce, never from file fields (I2).
	idHex := planIdentity(strings.TrimPrefix(fresh.Hash, "sha256:"), plan.Nonce)
	if plan.ID != "p_"+idHex[:12] || plan.Tier != fresh.Tier {
		return fail(refused("PLAN_INVALID", "plan "+id+" does not match its own hash"))
	}
	wantCode := ""
	if fresh.Tier == tierTyped.String() {
		service, objects := codeParts(fresh)
		wantCode = confirmCode(service, objects, idHex)
		switch typed := strings.TrimSpace(code); {
		case typed == "":
			if os.Rename(claimed, open) == nil {
				consumed = false
			}
			return fail(refused("CODE_REQUIRED", "this plan needs the confirmation code the user types"))
		case typed != wantCode:
			// The counter lives in the local plan file: it limits accidents,
			// not anyone who can edit that file.
			if plan.Mismatches++; plan.Mismatches >= 3 {
				return fail(refused("PLAN_INVALIDATED", "three wrong confirmation codes; the plan is gone"))
			}
			if writePlanFile(claimed, plan) == nil && os.Rename(claimed, open) == nil {
				consumed = false
			}
			return fail(refused("CODE_MISMATCH", fmt.Sprintf("the confirmation code does not match (%d of 3 attempts used)", plan.Mismatches)))
		}
	}
	env.Plan = &verbPlan{ID: plan.ID, Hash: fresh.Hash, ExpiresAt: plan.ExpiresAt, Confirm: fresh.Confirm, Code: wantCode}
	if isConfig {
		err := executeConfig(paths, s, &env, *fresh.ConfigAction, ctx, noSnapshot)
		if errors.Is(err, errSnapshotFailed) && os.Rename(claimed, open) == nil {
			consumed = false // nothing was deleted: the typed code stays valid for --no-snapshot
		}
		if err != nil {
			return fail(err)
		}
		return env, env.exit
	}
	if frozeNothing(request, fresh.Action) {
		return fail(refused("UNRESOLVED_TARGET", "the plan named targets but froze none"))
	}
	if err := executeAction(s, e, &env, fresh.Action); err != nil {
		return fail(err)
	}
	return env, env.exit
}

// frozeNothing: a request that named targets (ids, --match, --area,
// --device) but froze no entity must never become a targetless call, which
// Home Assistant would apply to every entity of the domain.
func frozeNothing(req planCallRequest, action planAction) bool {
	named := len(req.EntityIDs) > 0 || req.Match != "" || req.Area != "" || req.Device != ""
	return named && len(action.EntityIDs) == 0
}

// Home Assistant statuses that prove the call was rejected before running.
var rejectedStatuses = map[int]bool{400: true, 404: true, 422: true}

// executeAction sends the frozen action exactly once — the payload is the
// frozen entity_ids plus the planned data, nothing else (I6, I7) — then
// verifies it on a fresh read budget.
func executeAction(s *relaySession, e *tierEngine, env *verbEnvelope, action planAction) error {
	body := map[string]any{}
	for key, value := range action.Data {
		body[key] = value
	}
	if len(action.EntityIDs) > 0 {
		body["entity_id"] = slices.Clone(action.EntityIDs)
	}
	s.renewBudget()
	flowsBefore := s.reauthFlows() // best effort, bounded; nil means no correlation
	result, err := s.postService(action.Service, body)
	s.renewBudget() // verification and correlation get their own budget
	var unknown *relayOutcomeUnknownError
	switch {
	case errors.As(err, &unknown):
		env.OutcomeUnknown = true
		return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN", message: err.Error(), next: "Verify the target state; never retry automatically."}
	case err != nil:
		return fmt.Errorf("%w; Home Assistant did not receive the call", err)
	case rejectedStatuses[result.status]:
		return &verbFailure{exit: verbExitUnverified, status: "failed", code: "SERVICE_REJECTED",
			message: fmt.Sprintf("Home Assistant rejected the call (HTTP %d): %s", result.status, truncate(string(result.body), 300)),
			next:    "Nothing ran; check the service data and the target."}
	}
	rows, outcome := verifyCall(s, e, action)
	env.Verification = rows
	if result.status >= 400 { // e.g. 500: it may have partly run — probes shown, never verified
		if result.status >= 500 && flowsBefore != nil {
			env.ReauthOpened = newReauthFlows(s, e, action.EntityIDs, flowsBefore)
		}
		return &verbFailure{exit: verbExitUnverified, status: "unverified", code: "SERVICE_ERROR",
			message: fmt.Sprintf("Home Assistant answered HTTP %d: %s", result.status, truncate(string(result.body), 300)),
			next:    "Do not retry automatically; check the target and the integration."}
	}
	env.Status, env.exit = outcome, verbExitOK
	if outcome == "unverified" || outcome == "failed" {
		env.exit = verbExitUnverified
	}
	return nil
}

// newReauthFlows reports reauth flows that opened during the call and belong
// to a target: same config entry, or same integration with exactly one
// entry. This is the rule's source since the service-call skill moved to
// `ha-nova call` (formerly its Generic 500 with a reauth side effect section).
func newReauthFlows(s *relaySession, e *tierEngine, targets []string, before map[string]reauthFlow) []string {
	time.Sleep(reauthSettle)
	rows, _ := e.fullRegistry()
	var entries []struct {
		EntryID string `json:"entry_id"`
		Domain  string `json:"domain"`
	}
	_ = s.wsDecode(map[string]any{"type": "config_entries/get"}, &entries, "the config entries")
	perDomain := map[string]int{}
	for _, entry := range entries {
		perDomain[entry.Domain]++
	}
	targetEntries, targetPlatforms := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		if slices.Contains(targets, row.EntityID) {
			targetEntries[row.ConfigEntryID], targetPlatforms[row.Platform] = true, true
		}
	}
	var opened []string
	for id, flow := range s.reauthFlows() {
		_, existed := before[id]
		matches := (flow.entryID != "" && targetEntries[flow.entryID]) || (targetPlatforms[flow.handler] && perDomain[flow.handler] == 1)
		if !existed && matches {
			opened = append(opened, flow.handler)
		}
	}
	slices.Sort(opened)
	return opened
}

func truncate(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[:n] + "…"
}
