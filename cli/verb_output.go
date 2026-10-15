package main

import (
	"encoding/json"
	"errors"
	"flag"
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
	SchemaVersion int            `json:"schema_version"`
	Verb          string         `json:"verb"`
	Server        string         `json:"server,omitempty"`
	Status        string         `json:"status"`
	Tier          string         `json:"tier,omitempty"`
	Plan          *verbPlan      `json:"plan,omitempty"`
	Action        *planAction    `json:"action,omitempty"`
	Resolved      []verbTarget   `json:"resolved,omitempty"`
	Deltas        []planDelta    `json:"deltas,omitempty"`
	Members       []tierMember   `json:"members,omitempty"`
	Consumers     []tierConsumer `json:"consumers,omitempty"`
	Reasons       []tierReason   `json:"reasons,omitempty"`
	Coverage      *tierCoverage  `json:"coverage,omitempty"`
	Verification  []verifyRow    `json:"verification,omitempty"`
	ReauthOpened  []string       `json:"reauth_flows_opened,omitempty"`
	// OutcomeUnknown: the request may have reached Home Assistant (exit 7);
	// status stays in the outcome vocabulary as "unverified".
	OutcomeUnknown bool `json:"outcome_unknown,omitempty"`
	// Config plans (PR5): the operation, the rendered diff, the undo-stack
	// receipt of an update, and the pre-delete snapshot file.
	Config   *configAction        `json:"config,omitempty"`
	Diff     []string             `json:"diff,omitempty"`
	Undo     *snapshotSaveReceipt `json:"undo,omitempty"`
	Snapshot string               `json:"snapshot,omitempty"`
	// BeforeConfig: the config an update replaced, whenever it may have landed.
	BeforeConfig json.RawMessage `json:"before_config,omitempty"`
	Result       any             `json:"result,omitempty"`
	Error        *verbError      `json:"error,omitempty"`
	Notices      []verbNotice    `json:"notices,omitempty"`
	exit         int             // success exit code; 0 unless a verb sets it (plan: 3)
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
	env.Notices = verbNotices(paths, relayVersion)
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
	env.Error = &verbError{Code: failure.code, Message: failure.message, Candidates: failure.candidates, Next: failure.next}
	return failure.exit
}

func verbNotices(paths runtimePaths, relayVersion string) []verbNotice {
	var notices []verbNotice
	if n := checkRelayVersionValue(paths, relayVersion); !n.empty() {
		notices = append(notices, verbNotice{Kind: "relay_outdated", Text: n.message})
	}
	if n := skillUpdateNudgeNotice(paths, false); !n.empty() {
		notices = append(notices, verbNotice{Kind: "update", Text: n.message})
	}
	return notices
}

func printVerbEnvelope(env verbEnvelope) bool {
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
