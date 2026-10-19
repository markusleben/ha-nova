package main

import (
	"bufio"
	"os"
	"strings"
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
func setupDone(code int) setupStepResult         { return setupStepResult{done: true, code: code} }

type setupFlow struct {
	ui setupTextUI

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
	relaySkipNote        string
	cloudSetupIncomplete bool
	cloudSetupPaused     bool

	cleanups []func()
}

func interactiveSetup(paths runtimePaths, cfg runtimeConfig, state installState, target string, hostFlag, haURLFlag, relayURLFlag, relayTokenFlag string, serviceMode bool, lifecycleMarker ...[]byte) int {
	flow := &setupFlow{
		ui:              setupTextUI{reader: bufio.NewReader(os.Stdin), out: os.Stdout},
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
	return flow.run()
}

func (f *setupFlow) run() int {
	defer func() {
		for i := len(f.cleanups) - 1; i >= 0; i-- {
			f.cleanups[i]()
		}
	}()
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
	switch step {
	case setupStepClient:
		return f.stepClient()
	case setupStepSecureStorageRecovery:
		return f.stepSecureStorageRecovery()
	case setupStepHost:
		return f.stepHost()
	case setupStepRelayInstall:
		return f.stepRelayInstall()
	case setupStepToken:
		return f.stepToken()
	case setupStepPairing:
		return f.stepPairing()
	case setupStepVerify:
		return f.stepVerify()
	default:
		return f.stepSkills()
	}
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
