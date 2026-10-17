package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const callUsage = `ha-nova call <domain.service> [<entity_id>…] [--match "<phrase>"] [--area <area_id>] [--device <device_id>] [--set k=v]… [--data-file <file>] [--response] [--skill <owner>]`

const callHelp = callUsage + `

Plans the call like "plan call". An ordinary call — up to 10 named targets
or a targetless call, nothing behind it — runs now: exactly one request,
never retried, then verified. Anything else stores the plan and exits 3; run "ha-nova apply"
after the user confirms the preview (or types the one-time code).
Exit: 0 verified/accepted, 3 needs confirmation, 4 refused, 5 ambiguous or
not found, 6 unverified/failed, 7 outcome unknown (verify; never retry).`

const applyUsage = "ha-nova apply --plan <id> [--confirm <code>]"

const applyHelp = applyUsage + `

Executes a stored plan once. Apply claims the plan, checks expiry and server,
re-plans from the stored request, and sends nothing when anything changed
(exit 2 with a new plan). A preview plan needs no code: running apply IS the
confirmation step, so run it only after the user confirmed that preview. A
typed plan needs --confirm with the four-digit one-time code the user typed
(spaces ignored; three wrong codes discard the plan; that counter guards
against slips, not tampering). A typed plan with "channel": "window" takes
no code: apply opens a confirmation window on this computer and waits for
the user (up to 90 s); if it can't open, apply returns a new plan with a code.
Config plans (plan write/delete/revert) save the config once, reload, and
verify the read-back; an update lands on the undo stack, and a delete (and
every dashboard save) takes a config snapshot first (--no-snapshot only
after that snapshot failed and the user chose to proceed without one).
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
	code := fs.String("confirm", "", "the one-time code from HA NOVA, exactly as the user typed it (typed plans only)")
	noSnapshot := fs.Bool("no-snapshot", false, "delete plans: proceed without the pre-delete config snapshot (only after the user chose that)")
	positional, err := parseVerbArgs(fs, &opts, args, applyHelp)
	if err == nil && (len(positional) != 0 || !planIDPattern.MatchString(*id)) {
		err = errors.New("Usage: " + applyUsage)
	}
	if err != nil {
		return verbUsageError("apply", err)
	}
	env, exit, asked := confirmByWindow(paths, opts, *id, *noSnapshot)
	if !asked {
		env, exit = applyPlan(paths, opts, *id, *code, *noSnapshot)
	}
	defer func() { humanAnswer = "" }()
	if !printVerbEnvelope(env) {
		return verbExitError
	}
	return exit
}

func refused(code, message string) *verbFailure {
	next, known := refusalNext[code]
	if !known {
		next = replan
	}
	return refusedWith(code, message, next)
}

func refusedWith(code, message, next string) *verbFailure {
	return &verbFailure{exit: verbExitRefused, code: code, message: message + "; nothing was sent", next: next}
}

// A re-plan always shows its new preview and asks again: no plan is ever
// applied without a fresh confirmation.
const replan = "Plan again (ha-nova plan call|write|delete|revert), show its preview, and get confirmation again."

// A used or claimed plan: the claim precedes recompute and send, so the
// winner may still be sending — an immediate read proves nothing.
const mayHaveRun = "Another apply holds or used this plan — its outcome is unknown. Do not plan or apply again; tell the user it may have run and let them check."

// refusalNext is the one next step per refusal code; anything not listed
// is a plan that must be made again.
var refusalNext = map[string]string{
	"PLAN_NOT_FOUND":          mayHaveRun,
	"PLAN_CLAIMED":            mayHaveRun,
	"PLAN_EXPIRED":            "Plan again, show the new preview, and get confirmation again.",
	"PLAN_INVALIDATED":        "Stop and tell the user; plan again only on their explicit request, show the new preview, and get confirmation again.",
	"SERVER_MISMATCH":         "Apply it with the plan's own server (--server), or plan again for this one.",
	"UNRESOLVED_TARGET":       "Name the entities explicitly, or check the area or device, then plan again.",
	"NO_SNAPSHOT_NOT_OFFERED": "Apply without --no-snapshot: the config snapshot is taken first.",
	"CODE_REQUIRED":           "Ask the user to type only the four digits of the one-time code from HA NOVA shown with the plan, then pass exactly what they typed with --confirm.",
	"CODE_MISMATCH":           "Ask the user to type only the four digits of the one-time code from HA NOVA again, as shown with the plan.",
	"CONFIRM_IN_FORM":         "Apply it in the app where it was planned (its confirmation form asks the user), or plan it again here and show the new preview.",
	"NO_UNDO_SNAPSHOT":        "Only the last verified update of an automation, script, or dashboard can be reverted; there is nothing to revert.",
	"REVERT_UNSUPPORTED":      "To undo an automation, script, or dashboard: plan revert --target <config_id> (for a dashboard, its url_path). The newest checkpoint belongs to a helper: revert that through ha-nova:helper.",
	"CONFIG_ID_UNSUPPORTED":   "Pass the config_id from `ha-nova get <entity_id> --config`, not the entity_id; a YAML item is edited through ha-nova:yaml-config.",
	"CONFIG_NOT_UI_MANAGED":   "It is defined in YAML or a package: edit it there through ha-nova:yaml-config.",
}

// configIDInUseNext: a collision usually means an earlier create already
// landed (a retry after a timeout); otherwise a script's key is its name, so
// another one is the user's choice — never an overwrite without them
// meaning it.
func configIDInUseNext(domain string) string {
	const earlier = "It may be the item an earlier apply created — read it first (`ha-nova find \"<alias>\"`); if it is, it exists: do not create another. "
	if domain == "script" {
		return earlier + "Otherwise ask the user for another name, then plan again with --id <new> --create; update the existing script only if they mean to replace it."
	}
	return earlier + "Otherwise plan again without --id (a new id is minted); update the existing automation only if the user means to replace it."
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
	defer func() { env.Notices = append(env.Notices, verbNotices(paths, s.version())...) }()

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
	valid := err == nil && timeErr == nil && plan.ID == id && plan.SchemaVersion >= 1 &&
		(plan.Channel == channelChat || plan.Channel == channelForm || plan.Channel == channelWindow || plan.SchemaVersion < planSchemaVersion)
	isCall := valid && plan.Kind == "call" && config == nil && request.validate() == nil
	isConfig := valid && config != nil && config.Op == plan.Kind && config.ConfigID != "" && config.validate() == nil
	switch {
	case !isCall && !isConfig:
		return fail(refused("PLAN_INVALID", "plan "+id+" cannot be read or is not a valid plan"))
	case isCall && staticRefusal(request) != nil:
		return fail(refuse(&env, *staticRefusal(request)))
	case !verbNow().Before(expires):
		return fail(refused("PLAN_EXPIRED", "plan "+id+" expired at "+plan.ExpiresAt))
	case expires.After(verbNow().Add(planTTL + time.Minute)):
		// The plan file is user-writable; a far-future expiry would turn the
		// 4-digit code into an unbounded online brute force. No plan is ever
		// valid longer than its TTL, so refuse one claiming to be.
		return fail(refused("PLAN_EXPIRED", "plan "+id+" has an expiry beyond its lifetime"))
	case plan.SchemaVersion != planSchemaVersion: // its code scheme is gone: never typed against a new one
		return fail(refused("PLAN_EXPIRED", "plan "+id+" was made by an earlier HA NOVA version"))
	case plan.Server != s.server:
		return fail(refused("SERVER_MISMATCH", fmt.Sprintf("plan %s belongs to server %q", id, plan.Server)))
	case isCall && frozeNothing(request, plan.Action):
		return fail(refused("UNRESOLVED_TARGET", "the plan named targets but froze none"))
	case noSnapshot && !plan.SnapshotFailed: // only a delete or a dashboard save takes a snapshot that can fail
		if os.Rename(claimed, open) == nil {
			consumed = false
		}
		return fail(refused("NO_SNAPSHOT_NOT_OFFERED", "--no-snapshot applies only after this plan's snapshot failed and the user chose to proceed without one"))
	case plan.Tier == tierTyped.String() && plan.Channel == channelForm && humanAnswer == "":
		// Made where the client shows forms: only the human's answer in that
		// form confirms it, never a code from argv (spec §3). No attempt is used.
		if os.Rename(claimed, open) == nil {
			consumed = false
		}
		return fail(refused("CONFIRM_IN_FORM", "plan "+id+" is confirmed in the confirmation form of the app it was planned in, not with a typed code"))
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
	// release hands the claimed plan back after a failure before anything
	// reached Home Assistant, so the same apply can be retried.
	release := func(err error) error {
		var failure *verbFailure
		if errors.Is(err, errNothingSent) && errors.As(err, &failure) {
			if consumed = os.Rename(claimed, open) != nil; consumed {
				failure.next = "Nothing was sent, so retrying is safe: " + replan
			}
		}
		return err
	}
	var expected *verbFailure
	if err != nil && !errors.As(err, &expected) {
		return fail(release(nothingSent(err))) // the Relay is unreachable: nothing was sent
	}
	if err != nil {
		return fail(err)
	}
	if fresh.Hash != plan.Hash {
		if fresh.Prompt != "" { // a confirmation form shows it without the preview (mcp_confirm.go)
			fresh.Prompt = "Something changed since the last preview. " + fresh.Prompt
		}
		if err := savePlan(paths, fresh); err != nil {
			return fail(err)
		}
		if env.Say == "" && isCall { // the new plan may need no confirmation; a drift still does
			env.Say = confirmationSay(e, &env, fresh, "")
		}
		if env.Say != "" {
			env.Say = "Something changed since the last preview. " + env.Say
		}
		return fail(&verbFailure{exit: verbExitDrift, status: "drift", code: "PLAN_DRIFT",
			message: "something changed since the preview; nothing was sent and the old plan is gone",
			next:    "Show the new preview and ask again (with its code; a plan without one: follow next_step)."})
	}
	// From here every envelope shows the STORED plan: the recompute's own
	// id and code were never saved, and typing them would burn this plan.
	env.Say, env.NextStep = "", "" // they name the recompute's code or id
	env.Plan = &verbPlan{ID: plan.ID, Hash: fresh.Hash, ExpiresAt: plan.ExpiresAt, Confirm: fresh.Confirm}
	// Identity, tier, and code come from the recompute and nonce, never file fields (I2).
	idHex := planIdentity(strings.TrimPrefix(fresh.Hash, "sha256:"), plan.Nonce)
	if plan.ID != "p_"+idHex[:12] || plan.Tier != fresh.Tier {
		return fail(refused("PLAN_INVALID", "plan "+id+" does not match its own hash"))
	}
	wantCode := ""
	if humanAnswer != "" { // the human's answer in a form or window replaces any argv code
		code = humanAnswer
	}
	if fresh.Tier == tierTyped.String() {
		wantCode = confirmCode(s.transport.Credential, idHex, plan.Channel, plan.ExpiresAt)
		if plan.Channel == channelWindow && humanAnswer == "" { // its code was never shown: no attempt is used
			if os.Rename(claimed, open) == nil {
				consumed = false
			}
			return fail(refusedWith("CONFIRM_IN_WINDOW", "plan "+id+" is confirmed in a window on this computer, not with a typed code",
				"Run ha-nova apply --plan "+id+" again without --confirm: it opens the confirmation window on this computer. Never ask the user for a code."))
		}
		switch typed := typedCode(code); {
		case !fourDigits.MatchString(typed): // nothing, or not four digits: it can never match, so no attempt is used
			if os.Rename(claimed, open) == nil {
				consumed = false
			}
			return fail(refused("CODE_REQUIRED", "this plan needs the four digits of its one-time code from HA NOVA, typed by the user"))
		case typed != wantCode:
			// The counter lives in the plan file: it limits slips, not editors.
			if plan.Mismatches++; plan.Mismatches >= 3 {
				return fail(refused("PLAN_INVALIDATED", "three wrong one-time codes; the plan is gone"))
			}
			if writePlanFile(claimed, plan) == nil && os.Rename(claimed, open) == nil {
				consumed = false
			}
			return fail(refused("CODE_MISMATCH", fmt.Sprintf("the one-time code does not match (%d of 3 attempts used)", plan.Mismatches)))
		}
	}
	if plan.Channel == channelWindow {
		wantCode = "" // never shown, not even once used
	}
	env.Plan = &verbPlan{ID: plan.ID, Hash: fresh.Hash, ExpiresAt: plan.ExpiresAt, Confirm: fresh.Confirm, Code: wantCode}
	if isConfig {
		err := executeConfig(paths, s, &env, *fresh.ConfigAction, ctx, noSnapshot)
		if errors.Is(err, errSnapshotFailed) {
			plan.SnapshotFailed = true // nothing was deleted or saved: the plan (and a typed code) stays valid for --no-snapshot
			if writePlanFile(claimed, plan) == nil && os.Rename(claimed, open) == nil {
				consumed = false
			}
		}
		if err != nil {
			return fail(release(err))
		}
		return env, env.exit
	}
	if frozeNothing(request, fresh.Action) {
		return fail(refused("UNRESOLVED_TARGET", "the plan named targets but froze none"))
	}
	if err := executeAction(s, e, &env, fresh.Action); err != nil {
		return fail(release(err))
	}
	return env, env.exit
}

// errNothingSent: the write failed before reaching Home Assistant, so the
// claimed plan is released and a retry is safe.
var errNothingSent = errors.New("nothing was sent")

// nothingSent words a failure before anything reached Home Assistant.
func nothingSent(err error) *verbFailure {
	next := "Nothing was sent, so retrying is safe: run the same command again once the Relay answers, and tell the user it was retried."
	return &verbFailure{exit: verbExitError, code: "RELAY_ERROR", message: err.Error() + "; nothing was sent to Home Assistant", next: next, cause: errNothingSent}
}

// afterSend turns a panic once the request may have reached Home Assistant
// into an unknown outcome (exit 7), never "nothing sent".
func afterSend(sent *bool, env *verbEnvelope, err *error) {
	if p := recover(); p != nil {
		if !*sent {
			panic(p)
		}
		env.OutcomeUnknown = true
		*err = &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN",
			message: fmt.Sprintf("an internal error after the request was sent: %v", p), next: "Verify the target state; never retry automatically."}
	}
}

// frozeNothing: a request that named targets (ids, --match, --area,
// --device) but froze no entity must never become a targetless call, which
// Home Assistant would apply to every entity of the domain.
func frozeNothing(req planCallRequest, action planAction) bool {
	named := len(req.EntityIDs) > 0 || req.Match != "" || req.Area != "" || req.Device != ""
	return named && len(action.EntityIDs) == 0
}
