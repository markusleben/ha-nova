package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A typed plan's human channel outside an MCP form (docs/work/2026-10-17-
// human-confirmation-channels-spec.md): a native confirmation window on this
// computer, else the code in chat (fallback E). Only the CLI detects it — no
// flag, no environment variable picks it — and the answer comes from the
// window's own process or call, never from stdin, argv, or a file.

type windowAnswer int

const (
	windowNoAnswer  windowAnswer = iota // timed out, closed, or ended without an answer: nothing is sent
	windowConfirmed                     // the human pressed Confirm (OK on Windows)
	windowCancelled                     // the human pressed Cancel
	windowFailed                        // it could not open at all: fallback E
)

const (
	// A late Confirm still re-plans, sends, and verifies within the host's limit.
	cliWindowWait = 90 * time.Second // under Claude Code's 120 s default command timeout
	mcpWindowWait = 40 * time.Second // under the common 60 s MCP tool-call timeout
	windowMinWait = 15 * time.Second // less left before expiry: no window
	windowOpened  = "A confirmation window opened on this computer — confirm there."
	windowAsked   = "Requested by your AI assistant. Go ahead only if you want this."
	windowSetting = "confirm_window" // config.json top level; "off" = codes in chat
)

var (
	windowWait = cliWindowWait
	formsShown bool // set by the MCP server: its client shows confirmation forms
	// The OS window (confirm_window_<os>.go); tests replace both.
	windowAvailable = nativeWindowAvailable
	windowRunner    = nativeWindow
)

// detectChannel picks a typed plan's channel: the window when this computer
// can show one and the user has not turned it off, else an MCP client's
// form, else chat. The window comes first because a model with a shell can
// start ha-nova mcp itself and answer its own form, but not an OS window.
// The phone (B) slots in before chat in its own change.
func detectChannel(paths runtimePaths) string {
	switch {
	case !windowOff(paths) && windowAvailable():
		return channelWindow
	case formsShown:
		return channelForm
	}
	return channelChat
}

// windowOff: the user turned confirmation windows off
// (`ha-nova setup confirm-window off`, which the window itself confirms);
// typed plans then use a form where the app shows one, else chat.
func windowOff(paths runtimePaths) bool {
	doc, err := loadConfigDocument(paths.ConfigFile)
	var value string
	return err == nil && json.Unmarshal(doc.top[windowSetting], &value) == nil && value == "off"
}

const confirmWindowUsage = "ha-nova setup confirm-window on|off"

// runConfirmWindowSetting turns confirmation windows on or off. Off moves
// every typed plan to a code in chat, which the model sees too, so the
// window itself confirms it: a model that runs this command can't click it.
// On needs no confirmation.
func runConfirmWindowSetting(paths runtimePaths, args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: " + confirmWindowUsage + "\n\nWhere a typed change (unlock, open, disarm, delete) asks you: \"on\" (default) opens a\nconfirmation window on this computer when one can open; \"off\" uses your app's\nconfirmation form, or a one-time code in chat that your AI assistant can see too. Turning it off is confirmed in that window.")
		return 0
	}
	if len(args) != 1 || args[0] != "on" && args[0] != "off" {
		printHumanErr("Usage: %s", confirmWindowUsage)
		return 1
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		printHumanErr("cannot read the HA NOVA configuration (run ha-nova setup first): %s", err)
		return 1
	}
	if args[0] == "off" && !windowOff(paths) {
		if !windowAvailable() {
			printHumanInfo("No confirmation window can open here, so HA NOVA already asks in your app's form or with a one-time code in chat. Nothing changed.")
			return 0
		}
		switch askWindow("Turn off confirmation windows? HA NOVA then asks in your app's confirmation form, or shows a one-time code in chat that your AI assistant can see too.", cliWindowWait) {
		case windowConfirmed:
		case windowFailed:
			printHumanInfo("The confirmation window couldn't open here, so HA NOVA asks in your app's form or with a one-time code in chat anyway. Nothing changed.")
			return 0
		default:
			printHumanErr("Nothing changed: the window was cancelled or got no answer. If no window appeared at all, set \"%s\": \"off\" in %s yourself.", windowSetting, paths.ConfigFile)
			return 1
		}
	}
	top := make(map[string]json.RawMessage, len(doc.top))
	for key, value := range doc.top {
		top[key] = value
	}
	delete(top, windowSetting)
	if args[0] == "off" {
		top[windowSetting] = json.RawMessage(`"off"`)
	}
	if err := writeJSONFileIfUnchanged(paths.ConfigFile, top, 0o600, doc.source); err != nil {
		printHumanErr("cannot save the setting: %s", err)
		return 1
	}
	printHumanInfo("Confirmation windows are %s.", args[0])
	return 0
}

// confirmByWindow runs before apply for a window plan. It re-plans like
// apply, shows that fresh text in the window, and on Confirm hands apply the
// plan's code in process (humanAnswer); apply then claims, re-plans, and
// checks as always, so what runs is what the window showed. asked=false:
// apply runs and reports whatever kept the window closed (drift, expiry, a
// refusal); asked=true: env is the result and nothing was sent.
func confirmByWindow(paths runtimePaths, opts verbOptions, id string, noSnapshot bool) (env verbEnvelope, exit int, asked bool) {
	if humanAnswer != "" || !planIDPattern.MatchString(id) { // a form already answered
		return env, 0, false
	}
	plan, err := readPlanFile(filepath.Join(planDir(paths), id+".json"))
	expires, timeErr := time.Parse(time.RFC3339, plan.ExpiresAt)
	if err != nil || timeErr != nil || plan.ID != id || plan.Tier != tierTyped.String() || plan.Channel != channelWindow ||
		plan.SchemaVersion != planSchemaVersion || !verbNow().Before(expires) || expires.After(verbNow().Add(planTTL+time.Minute)) ||
		opts.serverSet && opts.server != plan.Server ||
		noSnapshot && !plan.SnapshotFailed {
		return env, 0, false
	}
	opts.server, opts.serverSet = plan.Server, true
	s, err := newRelaySession(paths, opts)
	if err != nil {
		return env, 0, false
	}
	defer s.close()
	fresh, ok := replanStored(paths, s, &verbEnvelope{}, plan, channelWindow)
	idHex := planIdentity(strings.TrimPrefix(plan.Hash, "sha256:"), plan.Nonce)
	if !ok || fresh.Hash != plan.Hash || fresh.Tier != plan.Tier || plan.ID != "p_"+idHex[:12] || fresh.Prompt == "" {
		return env, 0, false
	}
	wait := min(windowWait, expires.Sub(verbNow()))
	if wait < windowMinWait {
		return windowStopped(s, "refused", "PLAN_EXPIRED", "plan "+id+" expires before a confirmation window could get an answer", replan)
	}
	again := fmt.Sprintf("Tell the user nothing changed. Run ha-nova apply --plan %s again (the window opens again) only after they say they still want it; never ask them for a code. If they saw no window, they can make this change in Home Assistant itself, or turn the windows off in a terminal: ha-nova setup confirm-window off.", id)
	switch askWindow(confirmText(fresh, noSnapshot)+"\n\n"+windowAsked, wait) {
	case windowConfirmed:
		humanAnswer = confirmCode(s.transport.Credential, idHex, channelWindow, plan.ExpiresAt)
		return env, 0, false
	case windowCancelled:
		return windowStopped(s, "declined", "CONFIRM_DECLINED", "the confirmation window was cancelled",
			"Tell the user nothing changed. Apply this plan again only if they ask for it.")
	case windowFailed:
		return windowFallback(paths, s, plan, noSnapshot)
	}
	return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", "the confirmation window got no answer in time", again)
}

// askWindow shows the window and waits for its answer; our own deadline
// backs the window's. Once it has stayed up a second, stderr says so (a
// host that shows a running command's output shows it; one that failed at
// once never claims it opened).
func askWindow(message string, wait time.Duration) windowAnswer {
	ctx, cancel := context.WithTimeout(context.Background(), wait+5*time.Second)
	defer cancel()
	done := make(chan windowAnswer, 1)
	go func() { done <- windowRunner(ctx, message, wait) }()
	select {
	case answer := <-done:
		return answer
	case <-time.After(time.Second):
		fmt.Fprintln(os.Stderr, windowOpened)
	}
	return <-done
}

// windowFallback: the window could not open (a sandbox, no desktop), so
// the plan becomes a form plan where the MCP client shows forms, else a
// chat plan (fallback E) — said, never silent. The new
// plan replaces the window plan; its `say` carries the code and its limit.
func windowFallback(paths runtimePaths, s *relaySession, plan storedPlan, noSnapshot bool) (verbEnvelope, int, bool) {
	env := verbEnvelope{SchemaVersion: 1, Verb: "apply", Status: "ok", Server: s.server}
	channel := channelChat
	if formsShown { // inside ha-nova mcp for a client with forms: the form is next
		channel = channelForm
	}
	fresh, ok := replanStored(paths, s, &env, plan, channel)
	if !ok || fresh.Hash != plan.Hash || fresh.Tier != plan.Tier || env.Plan == nil {
		return verbEnvelope{}, 0, false // apply reports the drift or the error
	}
	// Claim the window plan first: one window plan yields at most one chat plan.
	open := filepath.Join(planDir(paths), plan.ID+".json")
	claimed := filepath.Join(planDir(paths), fmt.Sprintf("%s.claimed-%d", plan.ID, os.Getpid()))
	if os.Rename(open, claimed) != nil {
		return windowStopped(s, "refused", "PLAN_CLAIMED", "plan "+plan.ID+" was already claimed or used", mayHaveRun)
	}
	fresh.SnapshotFailed = plan.SnapshotFailed // a user's no-copy choice stays offered
	if err := savePlan(paths, fresh, s.transport.Credential); err != nil {
		_ = os.Rename(claimed, open)
		return windowStopped(s, "error", "CONFIRM_UNAVAILABLE", "the confirmation window could not open, and no code could be prepared instead ("+err.Error()+")",
			"Tell the user nothing changed; they can try again in a moment, or make this change in Home Assistant itself.")
	}
	_ = os.Remove(claimed)
	flag := ""
	if noSnapshot { // the user already chose to go on without a copy
		env.Say, flag = strings.Replace(env.Say, sayCopyNote, "No copy could be saved, so once deleted it can't be restored.", 1), " --no-snapshot"
	}
	env.Say = "The confirmation window couldn't open on this computer. " + env.Say
	env.NextStep = fmt.Sprintf("The window couldn't open, so this is a new plan with a one-time code: show `say`; only after a reply that is only that code, run ha-nova apply --plan %s --confirm <the user's text>%s.", env.Plan.ID, flag)
	return env, env.exit, true
}

// replanStored re-plans a stored plan's request on channel, without saving:
// the window's text, or the chat plan that replaces it.
func replanStored(paths runtimePaths, s *relaySession, env *verbEnvelope, plan storedPlan, channel string) (storedPlan, bool) {
	defer func(saved string) { planChannel = saved }(planChannel)
	planChannel = channel
	env.Server = s.server
	var fresh storedPlan
	var err error
	switch config := plan.ConfigRequest; {
	case plan.Kind == "call" && config == nil && plan.Request.validate() == nil && staticRefusal(plan.Request) == nil:
		_, fresh, err = computeCallPlan(s, env, plan.Request)
	case config != nil && config.Op == plan.Kind && config.ConfigID != "" && config.validate() == nil:
		_, fresh, _, err = computeConfigPlan(paths, s, env, *config)
	default:
		return fresh, false
	}
	return fresh, err == nil
}

// windowStopped: nothing was sent and the plan is untouched (no claim
// precedes the answer).
func windowStopped(s *relaySession, status, code, message, next string) (verbEnvelope, int, bool) {
	env := verbEnvelope{SchemaVersion: 1, Verb: "apply", Server: s.server}
	exit := setVerbError(&env, &verbFailure{exit: verbExitRefused, status: status, code: code, message: message + "; nothing was sent", next: next})
	return env, exit, true
}
