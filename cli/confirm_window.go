package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
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
	windowClosed                        // closed from outside (killed) before an answer: nothing is sent
)

const (
	// A late Confirm still re-plans, sends, and verifies within the host's limit.
	cliWindowWait    = 90 * time.Second // under Claude Code's 120 s default command timeout
	mcpWindowWait    = 40 * time.Second // under the common 60 s MCP tool-call timeout
	windowOpened     = "A confirmation window opened on this computer — confirm there."
	windowOpenedBoth = "A confirmation window opened on this computer — confirm there or on your phone."
	windowAsked      = "Requested by your AI assistant. Go ahead only if you want this."
	windowBoth       = "Your linked phone asks too. The first answer counts; a later one changes nothing."
	windowSetting    = "confirm_window" // config.json top level; "off" = codes in chat
)

// windowsBoxStyle (confirm_window_windows.go): MB_OKCANCEL, MB_ICONWARNING,
// MB_DEFBUTTON2 (Cancel is the default), MB_TOPMOST. No MB_SETFOREGROUND:
// raiseBox takes the foreground only once OK is off. Here so every
// platform's tests check it.
const windowsBoxStyle = 0x1 | 0x30 | 0x100 | 0x40000

var (
	windowWait    = cliWindowWait
	windowMinWait = 15 * time.Second // less left before expiry: no window or phone ask
	formsShown    bool               // set by the MCP server: its client shows confirmation forms
	// The OS window (confirm_window_<os>.go); tests replace both.
	windowAvailable = nativeWindowAvailable
	windowRunner    = nativeWindow
)

// detectChannel picks a typed plan's channel: the window when this computer
// can show one and the user has not turned it off, else the linked phone,
// else an MCP client's form, else chat (detectAfterWindow). The window and
// the phone come before the form because a model with a shell can start
// ha-nova mcp itself and answer its own form, but can't click an OS window
// or tap a phone.
func detectChannel(s *relaySession) string {
	if chatCall == nil && !windowOff(s.paths) && windowAvailable() { // ha-nova serve runs on a server: never a window
		return channelWindow
	}
	return detectAfterWindow(s)
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
		fmt.Println("Usage: " + confirmWindowUsage + "\n\nWhere a typed change (unlock, open, disarm, delete) asks you: \"on\" (default) opens a\nconfirmation window on this computer when one can open; \"off\" uses your linked phone, else your app's\nconfirmation form, or a four-digit code in chat that your AI assistant can see too. Turning it off is confirmed in that window.")
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
			printHumanInfo("No confirmation window can open here, so HA NOVA already asks on your linked phone, in your app's form, or with a four-digit code in chat. Nothing changed.")
			return 0
		}
		switch askWindow(context.Background(), "Turn off confirmation windows? HA NOVA then asks on your linked phone, else in your app's confirmation form, or shows a four-digit code in chat that your AI assistant can see too.", "en", cliWindowWait) {
		case windowConfirmed:
		case windowFailed:
			printHumanInfo("The confirmation window couldn't open here, so HA NOVA asks on your linked phone, in your app's form, or with a four-digit code in chat anyway. Nothing changed.")
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
	if chatCall != nil { // ha-nova serve never shows a window (it makes no window plans)
		return env, 0, false
	}
	plan, fresh, s, idHex, wait, goal, ok := humanPlan(paths, opts, id, noSnapshot, channelWindow)
	if !ok {
		return env, 0, false
	}
	setup, status, deadline, inTime := discoverInTime(s, wait)
	if !inTime {
		defer s.close()
		return windowStopped(s, "refused", "PLAN_EXPIRED", "plan "+id+" expires before a confirmation window could get an answer", replan)
	}
	again := fmt.Sprintf("Tell the user in plain words: the window closed without an answer, nothing changed, and they can ask again. Run ha-nova apply --plan %s again (the window opens again) only after they say they still want it; never ask them for a code. If they saw no window, they can make this change in Home Assistant itself.", id)
	text := confirmText(fresh, noSnapshot) + "\n\n"
	switch status {
	case phoneReady: // the linked phone asks too (confirm_both.go)
		a := &phoneAsking{id: id, plan: plan, s: s, idHex: idHex, goal: goal, setup: setup, ask: phonePlanAsk(goal, noSnapshot), deadline: deadline}
		return confirmBoth(a, text+windowBoth+"\n"+windowAsked)
	case phoneMissing, phoneNone, phoneLegacy: // the window still asks; a user waiting at the phone learns why it stays quiet
		if status == phoneMissing || phoneLinkedHere(paths) {
			fmt.Fprintln(os.Stderr, phoneQuietText)
			again += phoneQuietNext
		}
	}
	defer s.close()
	switch askWindow(context.Background(), text+windowAsked, plan.Lang, time.Until(deadline)) {
	case windowConfirmed:
		humanAnswer, humanWhere = confirmCode(s.transport.Credential, idHex, channelWindow, plan.ExpiresAt), channelWindow
		return env, 0, false
	case windowCancelled:
		return windowStopped(s, "declined", "CONFIRM_DECLINED", "the confirmation window was cancelled",
			"Tell the user nothing changed. Apply this plan again only if they ask for it.")
	case windowFailed:
		return windowFallback(paths, s, plan, noSnapshot)
	case windowClosed:
		return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", "the confirmation window was closed", again)
	}
	return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", "the confirmation window got no answer in time", again)
}

// askWindow shows the window and waits for its answer; our own deadline
// backs the window's. Once it has stayed up a second, stderr says so (a
// host that shows a running command's output shows it; one that failed at
// once never claims it opened).
// A cancelled parent withdraws the window (it closes; confirm_both.go).
// A signal that ends this process closes the window first, then ends the
// process as the signal would (lane E2): a window left behind takes a
// Confirm nothing reads.
func askWindow(parent context.Context, message, lang string, wait time.Duration) windowAnswer {
	answer, ended := askWindowUntilSignal(parent, message, lang, wait)
	endBySignal(ended)
	return answer
}

// askWindowUntilSignal is askWindow without the ending: on SIGTERM, SIGINT,
// or SIGHUP (one not ignored since start, as under nohup) it withdraws the
// window and returns the signal, so the caller can tidy up (confirm_both.go
// ends the phone's ask) before endBySignal. SIGKILL can't be caught: on Linux
// the window gets Pdeathsig (confirm_window_linux.go); Windows' box lives
// in this process. macOS: an osascript window outlives a SIGKILL until its
// own timeout.
func askWindowUntilSignal(parent context.Context, message, lang string, wait time.Duration) (windowAnswer, os.Signal) {
	ctx, cancel := context.WithTimeout(parent, wait+5*time.Second)
	defer cancel()
	signals := make(chan os.Signal, 1)
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		if runtime.GOOS != "windows" && !signal.Ignored(sig) {
			signal.Notify(signals, sig)
		}
	}
	done := make(chan windowAnswer, 1)
	go func() { done <- windowRunner(ctx, message, lang, wait) }()
	opened := time.After(time.Second)
	for {
		select {
		case answer := <-done:
			signal.Stop(signals) // from here a signal acts as usual; one queued before still wins
			select {
			case ended := <-signals:
				return windowNoAnswer, ended
			default:
				return answer, nil
			}
		case <-opened:
			if ctx.Err() == nil {
				fmt.Fprintln(os.Stderr, map[bool]string{false: windowOpened, true: windowOpenedBoth}[strings.Contains(message, windowBoth)])
			}
		case ended := <-signals:
			cancel() // the window closes; a stuck one doesn't hold the ending up
			select {
			case <-done:
			case <-time.After(3 * time.Second):
			}
			signal.Stop(signals)
			return windowNoAnswer, ended
		}
	}
}

// endBySignal ends the process by the signal that stopped a window, as it
// would have without the window (exit 128+n), after one plain line.
var endBySignal = func(ended os.Signal) {
	if ended == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "HA NOVA was stopped; the confirmation window closed and nothing was sent.")
	if self, err := os.FindProcess(os.Getpid()); err == nil {
		_ = self.Signal(ended)
		time.Sleep(time.Second) // the signal may land on another thread: let it end the process first
	}
}

// windowFallback: the window could not open (a sandbox, no desktop), so
// the plan becomes a form plan where the MCP client shows forms, else a
// chat plan (fallback E) — said, never silent. The new
// plan replaces the window plan; its `say` carries the code and its limit.
func windowFallback(paths runtimePaths, s *relaySession, plan storedPlan, noSnapshot bool) (verbEnvelope, int, bool) {
	env := verbEnvelope{SchemaVersion: 1, Verb: "apply", Status: "ok", Server: s.server}
	// A window that failed late: its wait is not read time.
	s.renewBudget()
	channel := detectAfterWindow(s) // the phone, else a form, else chat
	switch channel {
	case channelPhoneMissing:
		return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "the confirmation window couldn't open, and HA NOVA can't reach the linked phone either", phoneMissingStep())
	case channelPhoneUnlinked:
		return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "the confirmation window couldn't open, and "+phoneUnlinkedWhy, phoneUnlinkedNext)
	}
	fresh, ok := replanStored(paths, s, &env, plan, channel)
	if !ok || !stillConfirmed(plan, fresh) || fresh.Tier != plan.Tier || env.Plan == nil {
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
		env.Say, flag = batchNoCopies(plan.Lang, strings.Replace(env.Say, sayCopyNote, sayNoCopyNote, 1)), " --no-snapshot"
	}
	env.Say = sayNoWindowHere + env.Say
	if channel == channelPhone { // no code: sealPlan's next_step applies
		if noSnapshot {
			env.NextStep = strings.Replace(env.NextStep, "without --confirm", "with --no-snapshot and without --confirm", 1)
		}
		return env, env.exit, true
	}
	env.NextStep = fmt.Sprintf("The window couldn't open, so this is a new plan with a one-time code: show `say`; only after a reply that is only that code, run ha-nova apply --confirm <the user's text> --plan %s%s.", env.Plan.ID, flag)
	if plan.Kind == "batch" {
		env.NextStep = batchNextStep(env.NextStep, fresh)
	}
	return env, env.exit, true
}

// replanStored re-plans a stored plan's request on channel, without saving:
// the window's text, or the chat plan that replaces it.
func replanStored(paths runtimePaths, s *relaySession, env *verbEnvelope, plan storedPlan, channel string) (storedPlan, bool) {
	defer func(saved string) { planChannel = saved }(planChannel)
	planChannel = channel
	s.lang = plan.Lang
	env.Server = s.server
	var fresh storedPlan
	var err error
	switch config := plan.ConfigRequest; {
	case plan.Kind == "call" && config == nil && plan.Request.validate() == nil && staticRefusal(plan.Request) == nil:
		_, fresh, err = computeCallPlan(s, env, plan.Request)
	case config != nil && config.Op == plan.Kind && config.ConfigID != "" && config.validate() == nil:
		_, fresh, _, err = computeConfigPlan(paths, s, env, *config)
	case plan.Kind == "raw" && config == nil:
		_, fresh, err = computeRawPlan(s, env, plan.RawRequests)
	case plan.Kind == "batch" && config == nil && validBatchMembers(plan.BatchMembers) == nil:
		_, fresh, err = computeBatchPlan(paths, s, env, plan.BatchMembers)
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

// humanPlan reads a typed plan of channel and re-plans it like apply, for
// a window or phone ask (goal: the phone's verb and object). ok=false:
// apply runs and reports whatever is wrong (drift, expiry, a refusal); s is
// open only when ok.
func humanPlan(paths runtimePaths, opts verbOptions, id string, noSnapshot bool, channel string) (plan, fresh storedPlan, s *relaySession, idHex string, wait time.Duration, goal [3]string, ok bool) {
	if humanAnswer != "" || !planIDPattern.MatchString(id) { // a form already answered
		return
	}
	plan, err := readPlanFile(filepath.Join(planDir(paths), id+".json"))
	expires, timeErr := time.Parse(time.RFC3339, plan.ExpiresAt)
	if err != nil || timeErr != nil || plan.ID != id || plan.Tier != tierTyped.String() || plan.Channel != channel ||
		plan.SchemaVersion != planSchemaVersion || !verbNow().Before(expires) || expires.After(verbNow().Add(planTTL+time.Minute)) ||
		opts.serverSet && opts.server != plan.Server ||
		noSnapshot && !plan.SnapshotFailed {
		return
	}
	opts.server, opts.serverSet = plan.Server, true
	if s, err = newRelaySession(paths, opts); err != nil {
		return plan, fresh, nil, "", 0, goal, false
	}
	if !planSealed(plan, s.transport.Credential) { // an edited plan file: no ask, and apply refuses it
		s.close()
		return plan, fresh, nil, "", 0, goal, false
	}
	env := verbEnvelope{}
	fresh, ok = replanStored(paths, s, &env, plan, channel)
	idHex = planIdentity(strings.TrimPrefix(plan.Hash, "sha256:"), plan.Nonce)
	if !ok || !stillConfirmed(plan, fresh) || fresh.Tier != plan.Tier || plan.ID != "p_"+idHex[:12] || fresh.Prompt == "" {
		s.close()
		return plan, fresh, nil, "", 0, goal, false
	}
	if fresh.Hash != plan.Hash { // still the confirmed plan: ask with its sealed preview, every warning kept
		fresh.Prompt = plan.Prompt
	}
	return plan, fresh, s, idHex, min(windowWait, expires.Sub(verbNow())), env.ask, true
}
