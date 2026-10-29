package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// Exit codes of the typed verbs (typed-verbs spec §2).
const (
	verbExitOK           = 0
	verbExitError        = 1
	verbExitDrift        = 2
	verbExitConfirmation = 3
	verbExitRefused      = 4
	verbExitNotFound     = 5
	verbExitUnverified   = 6
	verbExitUnknown      = 7
)

// verbEnvelope is the schema_version 1 output of every typed verb; empty
// fields are omitted. Result carries the verb-specific read payload.
type verbEnvelope struct {
	SchemaVersion int       `json:"schema_version"`
	Verb          string    `json:"verb"`
	Server        string    `json:"server,omitempty"`
	Status        string    `json:"status"`
	Tier          string    `json:"tier,omitempty"`
	Plan          *verbPlan `json:"plan,omitempty"`
	// Say: the plain text for the user (CLI-built, one shape: say_lines.go):
	// a plan's confirmation, or an apply's or call's result line.
	Say string `json:"say,omitempty"`
	// NextStep: the agent's next step (a preview plan: wait for the user's
	// reply; a window plan: apply without a code; its fallback: a new code plan).
	NextStep     string         `json:"next_step,omitempty"`
	Action       *planAction    `json:"action,omitempty"`
	Resolved     []verbTarget   `json:"resolved,omitempty"`
	Deltas       []planDelta    `json:"deltas,omitempty"`
	Members      []tierMember   `json:"members,omitempty"`
	Consumers    []tierConsumer `json:"consumers,omitzero"` // [] = a complete delete scan found no automation/script/scene; absent = no complete scan
	Reasons      []tierReason   `json:"reasons,omitempty"`
	Coverage     *tierCoverage  `json:"coverage,omitempty"`
	Verification []verifyRow    `json:"verification,omitempty"`
	ReauthOpened []string       `json:"reauth_flows_opened,omitempty"`
	// OutcomeUnknown: the request may have reached Home Assistant (exit 7);
	// status stays in the outcome vocabulary as "unverified".
	OutcomeUnknown bool `json:"outcome_unknown,omitempty"`
	// Config plans (PR5): the operation, the rendered diff, the undo-stack
	// receipt of an update, and the pre-delete snapshot file.
	Config   *configAction        `json:"config,omitempty"`
	Diff     []string             `json:"diff,omitempty"`
	Undo     *snapshotSaveReceipt `json:"undo,omitempty"`
	Snapshot string               `json:"snapshot,omitempty"`
	// Restore: after a delete with a snapshot, the one command that undoes it.
	Restore string `json:"restore,omitempty"`
	// BeforeConfig: the config an update replaced, whenever it may have landed.
	BeforeConfig json.RawMessage `json:"before_config,omitempty"`
	Result       any             `json:"result,omitempty"`
	Image        *verbImage      `json:"image,omitempty"` // ha_read camera (mcp_read.go)
	Error        *verbError      `json:"error,omitempty"`
	Notices      []verbNotice    `json:"notices,omitempty"`
	exit         int             // success exit code; 0 unless a verb sets it (plan: 3)
	ask          actionGoal      // a typed plan's phone question: its goal and the reasons the title doesn't say (sayAsk)
	everyday     *everydayLine   // an MQTT or Supervisor call's everyday line (confirm_everyday.go), set with the plan
	done         string          // what a config or rooms apply did, for its result line (resultSay)
}

type verbTarget struct {
	EntityID string `json:"entity_id"`
	Name     string `json:"name,omitempty"`
	AreaID   string `json:"area_id,omitempty"`
	Area     string `json:"area,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	State    string `json:"state,omitempty"` // find --state: the current state
	Group    bool   `json:"group,omitempty"` // find: a group (it counts its members again)
	By       string `json:"by"`
}

type verbError struct {
	Code       string       `json:"code"`
	Message    string       `json:"message"`
	Candidates []verbTarget `json:"candidates,omitempty"`
	Next       string       `json:"next,omitempty"`
}

type verbNotice struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// verbFailure is an expected verb outcome with its own exit code and error
// code; any other error is an operational failure (exit 1).
type verbFailure struct {
	exit       int
	status     string // envelope status; "error" when empty
	code       string
	message    string
	next       string
	candidates []verbTarget
	cause      error // optional sentinel for errors.Is
}

func (f *verbFailure) Error() string { return f.message }
func (f *verbFailure) Unwrap() error { return f.cause }

type verbOptions struct {
	server, via       string
	serverSet, viaSet bool
}

func addVerbFlags(fs *flag.FlagSet, opts *verbOptions) {
	fs.StringVar(&opts.server, "server", "", "server profile name (multi-server installs)")
	fs.StringVar(&opts.via, "via", "", "relay transport override: local or cloud")
	fs.Bool("json", true, "JSON output (always on; accepted for symmetry)")
}

// parseVerbArgs parses flags placed before, between, or after positionals
// and validates the shared --server/--via flags like `relay` does.
func parseVerbArgs(fs *flag.FlagSet, opts *verbOptions, args []string, usage string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if helpRequested(err, fs, usage) {
				return nil, errHelpShown
			}
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	fs.Visit(func(f *flag.Flag) {
		opts.serverSet = opts.serverSet || f.Name == "server"
		opts.viaSet = opts.viaSet || f.Name == "via"
	})
	if opts.serverSet && strings.TrimSpace(opts.server) == "" {
		return nil, errors.New("--server requires a non-empty profile name")
	}
	if opts.viaSet {
		if _, err := parseRelayVia(opts.via); err != nil {
			return nil, err
		}
	}
	return positional, nil
}

// runReadVerb owns the envelope: one session, one JSON document on stdout,
// one exit code. Notices come from the update nudge cache and the Relay
// version header; a stale cache may start the existing background refresh,
// as `relay` already does, but the verb itself waits for no network call.
func runReadVerb(paths runtimePaths, verb string, opts verbOptions, body func(*relaySession, *verbEnvelope) error) int {
	env := verbEnvelope{SchemaVersion: 1, Verb: verb, Status: "ok"}
	relayVersion := ""
	session, err := newRelaySession(paths, opts)
	if err == nil {
		env.Server = session.server
		err = body(session, &env)
		relayVersion = session.version()
		session.close()
	}
	env.Notices = append(env.Notices, verbNotices(paths, relayVersion)...)
	exit := env.exit
	if err != nil {
		exit = setVerbError(&env, err)
	}
	if !printVerbEnvelope(env) {
		return verbExitError
	}
	return exit
}

func verbUsageError(verb string, err error) int {
	if errors.Is(err, errHelpShown) {
		return verbExitOK
	}
	env := verbEnvelope{SchemaVersion: 1, Verb: verb}
	setVerbError(&env, &verbFailure{exit: verbExitError, code: "USAGE", message: err.Error() + "; nothing was sent"})
	printVerbEnvelope(env)
	return verbExitError
}

func setVerbError(env *verbEnvelope, err error) int {
	env.Status = "error"
	env.Result = nil
	var failure *verbFailure
	if !errors.As(err, &failure) {
		failure = &verbFailure{exit: verbExitError, code: "RELAY_ERROR", message: err.Error()}
	}
	if failure.status != "" {
		env.Status = failure.status
	}
	if env.Status != "drift" { // only a drift result carries a stored plan to confirm
		env.Say, env.NextStep = "", "" // a plan that failed to store has nothing to show or apply
	}
	if unreachable := relayUnreachableError(""); errors.As(err, &unreachable) && !activeServerIsDemo() {
		env.Say = relayUnreachableSay
	}
	env.Error = &verbError{Code: failure.code, Message: failure.message, Candidates: failure.candidates, Next: failure.next}
	return failure.exit
}

func verbNotices(paths runtimePaths, relayVersion string) []verbNotice {
	var notices []verbNotice
	if activeServerIsDemo() {
		notices = append(notices, verbNotice{Kind: "demo", Text: demoNoticeText})
		if text := demoResetText(verbNow()); text != "" {
			notices = append(notices, verbNotice{Kind: "demo_reset", Text: text})
		}
	} else if demoProfileOn(paths) {
		notices = append(notices, verbNotice{Kind: "home", Text: "This is the user's own home, not the demo house; the demo house needs --server demo (apply follows its plan)."})
	}
	if n := checkRelayVersionValue(paths, relayVersion); !n.empty() {
		notices = append(notices, verbNotice{Kind: "relay_outdated", Text: n.message})
	}
	if n := skillUpdateNudgeNotice(paths, false); !n.empty() {
		notices = append(notices, verbNotice{Kind: "update", Text: n.message})
	}
	return notices
}

func printVerbEnvelope(env verbEnvelope) bool {
	env.Say = resultSay(&env)
	out, err := json.Marshal(env)
	if err == nil {
		_, err = os.Stdout.Write(append(out, '\n'))
	}
	if err != nil {
		printErr("cannot write %s output: %s", env.Verb, err)
		return false
	}
	return true
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

// errNothingSent: the write failed before reaching Home Assistant, so the
// claimed plan is released and a retry is safe.
var errNothingSent = errors.New("nothing was sent")

// nothingSent words a failure before anything reached Home Assistant.
func nothingSent(err error) *verbFailure {
	next := "Nothing was sent, so retrying is safe: run the same command again once the Relay answers, and tell the user it was retried."
	return &verbFailure{exit: verbExitError, code: "RELAY_ERROR", message: err.Error() + "; nothing was sent to Home Assistant", next: next, cause: errors.Join(errNothingSent, err)}
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
	"SERVER_MISMATCH":         "Run ha-nova apply --plan <id> again without --server or HA_NOVA_SERVER: apply follows its plan's server. Or plan again for this server.",
	"UNRESOLVED_TARGET":       "Name the entities explicitly, or check the area or device, then plan again.",
	"NO_SNAPSHOT_NOT_OFFERED": "Apply without --no-snapshot: the config snapshot is taken first.",
	"CODE_REQUIRED":           "Ask the user to type only the four digits of the one-time code from HA NOVA shown with the plan, then pass exactly what they typed with --confirm, written before --plan.",
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
