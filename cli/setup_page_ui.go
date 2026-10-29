package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sync"
	"time"
)

// setupWebUI renders setup on the local page (setup_page_server.go). It sends
// data only: a screen, progress rows, catalog keys with their values, and HA
// names; the page words and formats them (setupweb/app.js, spec §4.2). What
// the page does not cover hands over to the terminal: the first read from
// termIn switches the page to "continue in the terminal", and the rest of the
// run renders there. Until then the terminal gets a transcript.

// Ends setup when the page waits this long for a click.
var setupPageIdle = 30 * time.Minute

// The actions the page may post; the engine accepts a subset at a time.
var setupPageActions = []string{"signin", "address", "terminal", "recheck", "web", "local", "stop", "demo", "fix", "change", "link_remove", "switch", "server", "clients", "window",
	"phone", "chat", "chat_recheck", "chat_move", "finish", // step 3 (setup_page_control.go)
	"preview", "save", "undo", "claude", "done", // step 4 (setup_page_home.go)
}

type setupPageMsg struct {
	Key  string         `json:"key"`
	Args map[string]any `json:"args,omitempty"`
}

func setupPageText(key string, kv ...any) *setupPageMsg {
	msg := &setupPageMsg{Key: key}
	for i := 0; i+1 < len(kv); i += 2 {
		if msg.Args == nil {
			msg.Args = map[string]any{}
		}
		msg.Args[fmt.Sprint(kv[i])] = kv[i+1]
	}
	return msg
}

type setupPageRow struct {
	ID    string        `json:"id"`
	State string        `json:"state"` // todo, run, ok, fail
	Msg   *setupPageMsg `json:"msg"`
}

type setupPageState struct {
	Screen     string               `json:"screen"` // looking, found, connect, noclient, control, home, terminal, done, closed, incomplete, failed, stopped
	HA         string               `json:"ha,omitempty"`
	Candidates []setupPageCandidate `json:"candidates,omitempty"`
	Clients    []string             `json:"clients,omitempty"` // product names
	App        string               `json:"app,omitempty"`     // missing, old, running
	AppVersion string               `json:"app_version,omitempty"`
	Primary    string               `json:"primary,omitempty"` // install, update, signin; empty: no sign-in from the page
	Rows       []setupPageRow       `json:"rows,omitempty"`
	Problem    *setupPageMsg        `json:"problem,omitempty"`
	Fix        string               `json:"fix,omitempty"` // retry, owner, web (chat apps can't work here yet), web_never (nor after a change here); app_update (re-run: the App needs an update)
	Links      map[string]string    `json:"links,omitempty"`
	AskHA      bool                 `json:"ask_ha,omitempty"` // step 1 opens the address field
	Typed      bool                 `json:"typed,omitempty"`  // the user typed HA's address: step 1 says so
	SignInURL  string               `json:"signin_url,omitempty"`
	Control    *setupPageControl    `json:"control,omitempty"`     // step 3
	Home       *setupPageHome       `json:"home,omitempty"`        // step 4
	NewClients []string             `json:"new_clients,omitempty"` // a re-run: AI assistants found since, to set up with a click
	Overview   []setupOverviewItem  `json:"overview,omitempty"`
	Web        bool                 `json:"web,omitempty"`         // no AI app here: Claude or ChatGPT in the browser (chat apps)
	Desktop    bool                 `json:"desktop,omitempty"`     // Claude Desktop without a usable Code runtime
	CodexWhere string               `json:"codex_where,omitempty"` // catalog key: where "Codex" works on this OS
	Handover   string               `json:"handover,omitempty"`    // why the terminal: code (code_install, code_update: the App first), keyring_locked, keyring_setup; "": a question there
	Accepts    []string             `json:"accepts"`
}

// setupPageCandidate is one Home Assistant discovery found: a name to show
// and the exact address to use.
type setupPageCandidate struct {
	Host string `json:"host"`
	URL  string `json:"url"`
}

type setupPageAction struct {
	Action string `json:"action"`
	Value  string `json:"value,omitempty"`
}

type setupWebUI struct {
	server *setupPageServer
	stdin  *bufio.Reader // the terminal's one reader; reading it hands over
	text   setupTextUI   // the terminal, after the hand-over
	log    io.Writer     // the transcript before it

	mu         sync.Mutex
	view       setupPageState
	actions    chan setupPageAction
	cancel     context.CancelFunc // the running sign-in, for the page's Stop
	stopAsked  bool
	handedOver bool
	idled      bool // setup ended because nobody clicked for setupPageIdle
}

// setupTranscriptWriter: the terminal while the page runs. It looks like the
// rest of setup there (styled on a terminal, plain otherwise, lane E7), but
// never animates, clears the screen, or shows an arrow menu (ui_mode.go).
type setupTranscriptWriter struct{ io.Writer }

type setupHandoverReader struct {
	ui *setupWebUI
	r  io.Reader
}

func (h setupHandoverReader) Read(p []byte) (int, error) {
	h.ui.handover()
	return h.r.Read(p)
}

func newSetupWebUI(stdin io.Reader, stdout io.Writer) *setupWebUI {
	ui := &setupWebUI{
		log:     setupTranscriptWriter{stdout},
		actions: make(chan setupPageAction, 1),
		view:    setupPageState{Screen: "looking"},
	}
	ui.stdin = bufio.NewReader(setupHandoverReader{ui: ui, r: stdin})
	ui.text = setupTextUI{reader: ui.stdin, out: stdout}
	return ui
}

func (ui *setupWebUI) update(change func(*setupPageState)) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	change(&ui.view)
}

func (ui *setupWebUI) snapshot() setupPageState {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	view := ui.view
	view.Rows = slices.Clone(view.Rows)
	if view.Accepts == nil {
		view.Accepts = []string{}
	}
	return view
}

// handover: from now on setup continues in the terminal.
func (ui *setupWebUI) handover() { ui.handoverFor("") }

// handoverFor: the hand-over, and why (setupPageState.Handover), so the
// page says what waits in the terminal.
func (ui *setupWebUI) handoverFor(reason string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if ui.handedOver {
		return
	}
	ui.handedOver = true
	ui.view.Screen, ui.view.Accepts, ui.view.SignInURL, ui.view.Handover = "terminal", nil, "", reason
}

func (ui *setupWebUI) inTerminal() bool {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.handedOver
}

// act takes a posted action: "stop" cancels a running sign-in; any other one
// must be an action the engine waits for right now, and is taken once.
func (ui *setupWebUI) act(action setupPageAction) error {
	if !slices.Contains(setupPageActions, action.Action) || len(action.Value) > 256 {
		return errSetupPageBadAction
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if action.Action == "stop" { // only while the page waits for the Home Assistant sign-in
		if ui.cancel == nil || ui.view.SignInURL == "" {
			return errSetupPageNotNow
		}
		ui.stopAsked = true
		ui.cancel()
		return nil
	}
	if !slices.Contains(ui.view.Accepts, action.Action) {
		return errSetupPageNotNow
	}
	select {
	case ui.actions <- action:
		ui.view.Accepts = nil // taken once; the next wait sets it again
		return nil
	default:
		return errSetupPageNotNow
	}
}

// wait shows that the page accepts the given actions and returns the first
// one. Ctrl+C in the terminal or setupPageIdle without a click ends setup.
func (ui *setupWebUI) wait(accepts ...string) (setupPageAction, error) {
	ui.update(func(v *setupPageState) { v.Accepts = accepts })
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	idle := time.NewTimer(setupPageIdle)
	defer idle.Stop()
	idled := false
	select {
	case action := <-ui.actions:
		return action, nil
	case <-ctx.Done():
	case <-idle.C:
		idled = true
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.view.Accepts, ui.idled = nil, idled
	select { // an action that raced the end is dropped
	case <-ui.actions:
	default:
	}
	return setupPageAction{}, errSetupExit
}

// running lets the page's Stop cancel a sign-in; stopped reports whether it did.
func (ui *setupWebUI) running(cancel context.CancelFunc) (stopped func() bool) {
	ui.mu.Lock()
	ui.cancel, ui.stopAsked = cancel, false
	ui.mu.Unlock()
	return func() bool {
		ui.mu.Lock()
		defer ui.mu.Unlock()
		ui.cancel = nil
		return ui.stopAsked
	}
}

func (ui *setupWebUI) setRow(id, state string, msg *setupPageMsg) {
	ui.update(func(v *setupPageState) {
		for i := range v.Rows {
			if v.Rows[i].ID == id {
				v.Rows[i].State = state
				if msg != nil {
					v.Rows[i].Msg = msg
				}
			}
		}
	})
}

// failRunningRows marks the rows that were running when a step failed.
func (ui *setupWebUI) failRunningRows() {
	ui.update(func(v *setupPageState) {
		for i := range v.Rows {
			if v.Rows[i].State == "run" {
				v.Rows[i].State = "fail"
			}
		}
	})
}

// The page sign-in (haSignInTab): the page navigates to Home Assistant, and
// the redirect comes back to this server.
func (ui *setupWebUI) navigate(target string) {
	ui.update(func(v *setupPageState) { v.SignInURL = target })
}

func (ui *setupWebUI) callbackHost() string { return ui.server.callbackHost() }

func (ui *setupWebUI) routeCallback(callback http.Handler) func() {
	return ui.server.routeCallback(callback)
}

func (ui *setupWebUI) callbackDone(w http.ResponseWriter, r *http.Request) {
	ui.update(func(v *setupPageState) { v.SignInURL = "" })
	ui.server.callbackDone(w, r)
}

// setupUI: the engine's views. Before the hand-over they go to the page and
// the transcript; after it, to the terminal.

func (ui *setupWebUI) header() {
	if ui.inTerminal() {
		ui.text.header()
	}
}

func (ui *setupWebUI) show(view setupView) {
	if ui.inTerminal() {
		ui.text.show(view)
		return
	}
	setupTextUI{out: ui.log}.show(view)
	if view.Progress != nil && view.Progress.Title.Key == "setup.step.connect" {
		ui.setRow("connect", "run", nil)
	}
	for _, block := range view.Blocks {
		if block.Kind == setupBlockSuccess && block.Lines[0].Key == "setup.verify.device_ok" {
			ui.setRow("connect", "ok", nil)
		}
	}
}

func (ui *setupWebUI) choose(ask setupAsk) (string, error) {
	ui.handover()
	return ui.text.choose(ask)
}

func (ui *setupWebUI) listChoices(ask setupAsk) {
	ui.handover()
	ui.text.listChoices(ask)
}

func (ui *setupWebUI) openURL(url string) {
	ui.handover()
	ui.text.openURL(url)
}

func (ui *setupWebUI) input(label setupMsg) (string, error) {
	ui.handover()
	return ui.text.input(label)
}

func (ui *setupWebUI) interactive() bool { return ui.text.interactive() }

func (ui *setupWebUI) working(label setupMsg, task func() error) error {
	if ui.inTerminal() {
		return ui.text.working(label, task)
	}
	row := label.Key == "setup.skills.installing"
	if row {
		ui.setRow("teach", "run", nil)
	}
	err := runSetupStepWithFeedback(ui.log, label.plain(), task)
	if row {
		state := "ok"
		if err != nil {
			state = "fail"
		}
		ui.setRow("teach", state, nil)
	}
	return err
}

// outcome: the terminal shows the banner (with the first look); the page
// shows the end screen.
func (ui *setupWebUI) outcome(o setupOutcome) {
	ui.text.outcome(o)
	screen := "done"
	if o.Kind == setupOutcomeIncomplete {
		screen = "incomplete"
	}
	ui.end(screen)
}

func (ui *setupWebUI) end(screen string) {
	ui.update(func(v *setupPageState) {
		v.Screen, v.Accepts, v.SignInURL = screen, nil, ""
	})
}

func (ui *setupWebUI) note(line string) { ui.text.note(line) }

func (ui *setupWebUI) cancelled() {
	ui.text.cancelled()
	ui.mu.Lock()
	screen := "stopped"
	if ui.idled {
		screen = "idle"
	}
	ui.mu.Unlock()
	ui.end(screen)
}

func (ui *setupWebUI) fail(msg setupMsg) {
	ui.text.fail(msg)
	ui.end("failed")
}

func (ui *setupWebUI) failErr(err error) {
	ui.text.failErr(err)
	ui.end("failed")
}

func (ui *setupWebUI) info(msg setupMsg) { ui.text.info(msg) }

func (ui *setupWebUI) termIn() *bufio.Reader { return ui.stdin }

func (ui *setupWebUI) termOut() io.Writer {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if ui.handedOver {
		return ui.text.out
	}
	return ui.log
}
