package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

// setupFlow is the interactive setup engine: ordered steps that decide what
// setup does (detection, decisions, actions, outcomes) and hand typed views
// and questions to a renderer (setup_flow_text.go). Steps call the pairing,
// Cloud, keychain, and client-install internals unchanged
// (docs/work/2026-10-18-onboarding-0.27-spec.md §3).
type setupStepID int

const (
	setupStepClient setupStepID = iota
	setupStepSecureStorageRecovery
	setupStepHost
	setupStepRelayInstall
	setupStepToken
	setupStepPairing
	setupStepVerify
	setupStepSkills
)

// setupStepResult: the next step, or the end of setup with its exit code.
type setupStepResult struct {
	next setupStepID
	done bool
	code int
}

func setupGoTo(next setupStepID) setupStepResult { return setupStepResult{next: next} }

// setupGoOn: a setup phase finished without ending setup.
var setupGoOn = setupStepResult{}

func setupDone(code int) setupStepResult { return setupStepResult{done: true, code: code} }

type setupFlow struct {
	ui   setupUI
	page *setupWebUI // the setup page renders; nil in the terminal
	// pageProblem: why the page's sign-in didn't connect this computer;
	// step 2 shows it.
	pageProblem *setupPageMsg
	// pageRetry: the owner's sign-in a pairing that didn't finish leaves
	// for step 2's Try again; any other click drops it.
	pageRetry      *haSignInSession
	pageRetryUntil time.Time
	pageTyped      bool // the user typed Home Assistant's address on the page
	// pageControlled: step 3 runs on the page; pageQuit: setup ended there
	// (Ctrl+C or idle); pageNotes: what step 3 did, for step 4, and
	// pageChatLink its Add to Claude link (pageOrgLink: for a Team or
	// Enterprise owner); pageGeneration: the install
	// generation setup completed with.
	pageControlled bool
	pageQuit       bool
	pageNotes      []*setupPageMsg
	pageChatLink   string
	pageOrgLink    string
	pageChatPhoned bool // a phone confirms for chat apps: step 4 says to allow every tool in Claude
	pageChatOff    bool // step 3 ended with chat apps off: step 4 tells a browser user what's next
	pageGeneration []byte
	// pageRerunning: setup ran on this computer before (setup_page_rerun.go);
	// pageRerunOK: it works, so the re-run screen with Done ends setup;
	// pageRemoved: this computer was removed on the NOVA page, step 1 says so;
	// pageNewClients: AI assistants found since the last setup.
	pageRerunning  bool
	pageRerunOK    bool
	pageRemoved    bool
	pageNewClients []string

	// inputs
	paths           runtimePaths
	cfg             runtimeConfig
	state           installState
	target          string
	hostFlag        string
	haURLFlag       string
	relayURLFlag    string
	relayTokenFlag  string
	serviceMode     bool
	lifecycleMarker [][]byte

	// detection
	explicitLocalSetup  bool
	choices             []setupClientChoice
	selectedClients     []string
	skippedClients      []string
	promptedClient      bool
	noClient            bool // no AI app here yet: pair anyway, finish says what to install (lane D1)
	connectionMode      setupConnectionMode
	connectionModeAsked bool
	overrideApplied     bool
	current             setupState
	deviceAlreadyPaired bool

	// credentials
	secureStorageRecovery           setupSecureStorageRecoveryState
	savedTokenBeforeSetup           string
	hadSavedTokenBeforeSetup        bool
	tokenStoragePreflightErr        error
	tokenStorageRecoveryReadBlocked bool
	legacyTokenStoreUnavailable     bool
	existingToken                   string
	token                           string
	tokenChanged                    bool
	formerServiceTokenFile          string
	formerServiceToken              string
	setupChanged                    bool

	// progress through the steps
	pairingFlow               bool
	pairingCredentialReceived bool
	devicePaired              bool
	pairedThisRun             bool // a pairing (sign-in or code) succeeded in this run
	manualCredentialFlow      bool
	pairingBackStep           setupStepID
	verifyFirstReuseFlow      bool
	hostChangeRetry           bool
	// Set only on the host -> install transition: a second computer whose
	// Relay already runs skips the install steps. The repair menu's "full
	// guidance" always shows them.
	detectRelayOnInstall bool
	// Shown under the pairing step header: the step header clears the
	// screen, so a note printed in the install step would vanish.
	relaySkipNote        setupMsg
	cloudSetupIncomplete bool
	cloudSetupPaused     bool
	// cloudPausedAtStart: Link was unfinished when setup started; this run
	// changes no credentials (setup_flow_link_paused.go).
	cloudPausedAtStart bool
	linkPausedShown    bool // its commands were shown

	cleanups []func()
}

func interactiveSetup(paths runtimePaths, cfg runtimeConfig, state installState, target string, hostFlag, haURLFlag, relayURLFlag, relayTokenFlag string, serviceMode bool, lifecycleMarker ...[]byte) int {
	flow := &setupFlow{
		paths:           paths,
		cfg:             cfg,
		state:           state,
		target:          target,
		hostFlag:        hostFlag,
		haURLFlag:       haURLFlag,
		relayURLFlag:    relayURLFlag,
		relayTokenFlag:  relayTokenFlag,
		serviceMode:     serviceMode,
		lifecycleMarker: lifecycleMarker,
		existingToken:   strings.TrimSpace(relayTokenFlag),
		connectionMode:  setupConnectionLocal,
		pairingBackStep: setupStepRelayInstall,
	}
	if flow.usesSetupPage() {
		return flow.runOnPage()
	}
	flow.ui = setupTextUI{reader: bufio.NewReader(os.Stdin), out: os.Stdout}
	return flow.run()
}

func (f *setupFlow) run() int {
	defer func() {
		for i := len(f.cleanups) - 1; i >= 0; i-- {
			f.cleanups[i]()
		}
	}()
	code := f.runSteps()
	if f.cloudPausedAtStart && !f.linkPausedShown { // Exit or a failure: still the Link commands
		fmt.Fprint(f.ui.termOut(), f.linkPausedOutcome().LinkSteps)
	}
	return code
}

func (f *setupFlow) runSteps() int {
	if result := f.start(); result.done {
		return result.code
	}
	first := f.prepareCredentials()
	if first.done {
		return first.code
	}
	step := first.next
	for {
		f.ui.header()
		result := f.runStep(step)
		if result.done {
			return result.code
		}
		step = result.next
	}
}

func (f *setupFlow) runStep(step setupStepID) setupStepResult {
	if f.cloudPausedAtStart && linkPausedStopsBefore(step) {
		return f.stopWhileLinkPaused()
	}
	switch step {
	case setupStepClient:
		return f.stepClient()
	case setupStepSecureStorageRecovery:
		return f.stepSecureStorageRecovery()
	case setupStepHost:
		if f.page != nil && !f.page.inTerminal() {
			return f.stepPage()
		}
		return f.stepHost()
	case setupStepRelayInstall:
		return f.stepRelayInstall()
	case setupStepToken:
		return f.stepToken()
	case setupStepPairing:
		return f.stepPairing()
	case setupStepVerify:
		return f.stepVerify()
	case setupStepSkills:
		return f.stepSkills()
	}
	panic(fmt.Sprintf("setup flow: unknown step %d", step))
}

// Service installs pair by default too: secure pairing no longer depends on
// a desktop keyring (the service path forces the file backend at the pairing
// step), and the legacy token flow costs manual HA UI steps. Only an
// explicit --relay-token or an already-stored token keeps the token path.
func (f *setupFlow) usePairingByDefault() bool {
	return f.connectionMode == setupConnectionHybrid ||
		(strings.TrimSpace(f.relayTokenFlag) == "" &&
			strings.TrimSpace(f.existingToken) == "")
}

// routeKnownAddress picks the first step for a saved Home Assistant address.
// A device credential from an earlier pairing makes this a paired install:
// re-runs (adding a client, finishing an interrupted setup) verify the
// existing pairing instead of demanding a fresh code every time.
func (f *setupFlow) routeKnownAddress(fallback setupStepID) setupStepID {
	if f.cfg.HAHost == "" || f.cfg.HAURL == "" {
		return fallback
	}
	switch {
	case f.deviceAlreadyPaired:
		f.devicePaired = true
		return setupStepVerify
	case f.existingToken != "" && f.current.RelayOK && !f.current.WSOK:
		f.verifyFirstReuseFlow = true
		return setupStepVerify
	case f.existingToken != "":
		if f.relayTokenFlag != "" {
			return setupStepToken
		}
		f.verifyFirstReuseFlow = true
		return setupStepVerify
	default:
		if f.usePairingByDefault() {
			f.pairingFlow = true
			return setupStepPairing
		}
		return setupStepToken
	}
}

func (f *setupFlow) wizardSteps() setupWizardSteps {
	if f.pairingFlow {
		return buildSetupPairingWizardSteps()
	}
	return buildSetupWizardSteps()
}

// textOnly: the next step exists only in the terminal (recovery states,
// spec §2), so the setup page hands over first, saying why (reason: see
// setupPageState.Handover). A no-op in the terminal.
func (f *setupFlow) textOnly(reason string) {
	if f.page != nil {
		f.page.handoverFor(reason)
	}
}

// cancelled ends setup the way Ctrl+C or "exit" does.
func (f *setupFlow) cancelled() setupStepResult {
	f.ui.cancelled()
	return setupDone(0)
}

// fail reports a failure and ends setup with exit 1.
func (f *setupFlow) fail(msg setupMsg) setupStepResult {
	f.ui.fail(msg)
	return setupDone(1)
}

func (f *setupFlow) failErr(err error) setupStepResult {
	f.ui.failErr(err)
	return setupDone(1)
}
