package main

import (
	"errors"
	"strings"
	"time"
)

func (f *setupFlow) stepClient() setupStepResult {
	answer, err := chooseSetupClient(f.ui, f.choices, f.target)
	if err == errSetupBack {
		return setupGoTo(setupStepClient)
	}
	if err == errSetupExit {
		return f.cancelled()
	}
	if err != nil {
		return f.failErr(err)
	}
	f.target = answer
	f.selectedClients, f.skippedClients, err = resolveSetupClientsWithChoices(f.choices, f.target)
	if err != nil {
		return f.failErr(err)
	}
	if f.tokenStoragePreflightErr != nil {
		return setupGoTo(setupStepSecureStorageRecovery)
	}
	return setupGoTo(setupStepHost)
}

func (f *setupFlow) stepSecureStorageRecovery() setupStepResult {
	f.textOnly(secureStorageHandover(f.tokenStoragePreflightErr))
	result, err := runSetupSecureStorageRecoveryFlow(f.ui.termIn(), f.ui.termOut(), f.tokenStoragePreflightErr, &f.secureStorageRecovery, setupSecureStorageRecoveryInitialAttempt)
	if err == errSetupBack {
		if f.promptedClient {
			return setupGoTo(setupStepClient)
		}
		return f.cancelled()
	}
	if err == errSetupExit {
		return f.cancelled()
	}
	if err != nil {
		return f.failErr(err)
	}
	if result != setupSecureStorageRecoveryRecovered {
		if isDesktopKeyringLockedError(f.tokenStoragePreflightErr) { // lane D7: say what happened, not a save error
			message := setupT("setup.storage.still_locked")
			if _, _, ok, err := selectedClientsServiceCredentialHint(f.paths, f.selectedClients); err == nil && ok && len(f.selectedClients) == 1 {
				message += " " + setupT("setup.storage.still_locked_service", "client", f.selectedClients[0])
			}
			return f.failErr(errors.New(message))
		}
		if f.tokenStorageRecoveryReadBlocked {
			return f.failErr(relayAuthTokenSetupReadError(f.tokenStoragePreflightErr))
		}
		return f.failErr(relayAuthTokenSetupSaveError(f.tokenStoragePreflightErr))
	}

	f.tokenStoragePreflightErr = relayAuthTokenSetupPreflightForSetup()
	if f.tokenStoragePreflightErr != nil {
		printRelayTokenStorageSetupWarning(f.tokenStoragePreflightErr)
		return f.failErr(relayAuthTokenSetupSaveError(f.tokenStoragePreflightErr))
	}
	f.tokenStorageRecoveryReadBlocked = false
	f.savedTokenBeforeSetup = ""
	f.hadSavedTokenBeforeSetup = false
	if savedToken, err := readRelayAuthTokenForSetup(); err == nil && strings.TrimSpace(savedToken) != "" {
		f.savedTokenBeforeSetup = strings.TrimSpace(savedToken)
		f.hadSavedTokenBeforeSetup = true
	} else if err != nil && !isMissingRelayAuthTokenError(err) {
		return f.failErr(relayAuthTokenSetupReadError(err))
	}
	if strings.TrimSpace(f.relayTokenFlag) == "" {
		f.existingToken = f.savedTokenBeforeSetup
	}
	f.current = detectSetupStateForAssessment(f.paths, f.cfg, f.state, f.target, f.savedTokenBeforeSetup, f.hadSavedTokenBeforeSetup)
	if f.current.IsComplete() {
		if f.overrideApplied {
			if err := saveSetupConfigWithLifecycle(f.paths, f.cfg, f.lifecycleMarker...); err != nil {
				return f.fail(setupText("setup.error.save_config", "error", err))
			}
		}
		f.ui.outcome(setupOutcome{Kind: setupOutcomeAlreadyDone, LegacyToken: f.cfg.RelaySecureBaseURL == "" && f.cfg.RelayBaseURL != ""})
		return setupDone(0)
	}
	f.verifyFirstReuseFlow = false
	// Secure storage just became readable: the pre-recovery transport
	// resolution is stale. A paired device found NOW routes to verify —
	// demanding a fresh code here would undo the point of recovery.
	_, _, _, f.deviceAlreadyPaired, _ = relayFunctionalTransportForDoctor(f.cfg)
	return setupGoTo(f.routeKnownAddress(setupStepHost))
}

func (f *setupFlow) stepHost() setupStepResult {
	var host string
	var haURL string
	var err error
	if f.hostChangeRetry {
		// The user just rejected the saved address — don't spend the
		// discovery window re-confirming it or offer it back as the
		// press-Enter default.
		f.ui.show(setupView{Blocks: []setupBlock{setupParagraph(setupText("setup.host.saved_unusable", "url", f.cfg.HAURL))}})
		host, haURL, err = promptValidHAHostFromReader(f.ui.termIn(), f.ui.termOut(), "")
	} else {
		candidate, selected, discoveryErr := selectDefaultHAHostWithFeedback(f.ui.termIn(), f.ui.termOut(), f.cfg)
		switch {
		case discoveryErr != nil:
			err = discoveryErr
		case selected:
			host = candidate.Host
			haURL = candidate.HAURL
		default:
			host, haURL, err = promptValidHAHostFromReader(f.ui.termIn(), f.ui.termOut(), candidate.Host)
		}
	}
	if err == errSetupBack {
		if f.hostChangeRetry {
			f.hostChangeRetry = false
			return setupGoTo(setupStepVerify)
		}
		if f.promptedClient {
			return setupGoTo(setupStepClient)
		}
		return setupGoTo(setupStepHost)
	}
	if err == errSetupExit {
		return f.cancelled()
	}
	if err != nil {
		return f.failErr(err)
	}
	previousRelayURL := strings.TrimSpace(f.cfg.RelayBaseURL)
	previousDerivedRelayURL := deriveRelayURLFromHA(f.cfg.HAURL, f.cfg.HAHost)
	f.cfg = applySelectedSetupHost(f.cfg, host, haURL, f.relayURLFlag)
	if strings.TrimSpace(f.relayURLFlag) == "" && previousRelayURL != "" &&
		previousRelayURL != previousDerivedRelayURL && f.cfg.RelayBaseURL != previousRelayURL {
		// A saved relay address that was NOT derived from the old host
		// is a deliberate custom setting (proxy, other machine); a
		// host change must not silently clobber it.
		f.cfg.RelayBaseURL = previousRelayURL
		f.ui.show(setupView{Blocks: []setupBlock{setupTight(setupText("setup.host.keep_relay", "url", previousRelayURL))}})
	}
	switch {
	case f.hostChangeRetry && f.verifyFirstReuseFlow && f.hadSavedTokenBeforeSetup && strings.TrimSpace(f.token) != "":
		// Resume with saved state: the relay app and tokens are known
		// to be in place; only the address was wrong, so return
		// straight to verification. Fresh-flow host changes — including
		// pasted-token fresh runs — fall through instead: the new
		// address may be a different instance that never got the
		// repository/app/token steps. Save the corrected address
		// now so it survives an exit before verification succeeds.
		f.hostChangeRetry = false
		if err := saveSetupConfigWithLifecycle(f.paths, f.cfg, f.lifecycleMarker...); err != nil {
			return f.fail(setupText("setup.error.save_config", "error", err))
		}
		return setupGoTo(setupStepVerify)
	case strings.TrimSpace(f.relayTokenFlag) != "" && !f.hostChangeRetry:
		return setupGoTo(setupStepToken)
	default:
		if f.hostChangeRetry {
			// A fresh-flow host change abandons the flag-driven or
			// pasted-token shortcut: the corrected address may be a
			// different instance that still needs the repository/app
			// install and client credential setup.
			f.verifyFirstReuseFlow = false
		}
		f.hostChangeRetry = false
		f.detectRelayOnInstall = true
		return setupGoTo(setupStepRelayInstall)
	}
}

func (f *setupFlow) stepRelayInstall() setupStepResult {
	pairAfterInstall := f.usePairingByDefault()
	steps := buildSetupWizardSteps()
	if pairAfterInstall {
		steps = buildSetupPairingWizardSteps()
	}
	skipInstall := false
	if f.detectRelayOnInstall && pairAfterInstall {
		skipInstall, f.relaySkipNote = detectInstalledRelay(f.ui.termOut(), f.paths, f.cfg, f.relayURLFlag)
	}
	f.detectRelayOnInstall = false
	if !skipInstall {
		f.relaySkipNote = setupMsg{}
		f.ui.show(setupView{Progress: &setupProgress{Step: steps.RelayInstall, Total: steps.Total, Title: setupText("setup.step.install_app")}})
		note, err := runRelayInstallSteps(f.ui.termIn(), f.ui.termOut(), f.paths, f.cfg, pairAfterInstall)
		f.relaySkipNote = note
		if err == errSetupBack {
			return setupGoTo(setupStepHost)
		}
		if err == errSetupExit {
			return f.cancelled()
		}
		if err != nil {
			return f.failErr(err)
		}
	}
	if !pairAfterInstall {
		setupInstalledSignIn = nil // no pairing follows that could use it
		return setupGoTo(setupStepToken)
	}
	if f.deviceAlreadyPaired {
		// An earlier pairing already gave this device its own
		// credential: verify it against the (re)installed relay
		// first — a failed verify still routes back to pairing.
		f.relaySkipNote = setupMsg{}
		f.devicePaired = true
		return setupGoTo(setupStepVerify)
	}
	f.pairingFlow = true
	f.pairingBackStep = setupStepRelayInstall
	if skipInstall {
		// No install screen was shown; back returns to the address.
		f.pairingBackStep = setupStepHost
	}
	return setupGoTo(setupStepPairing)
}

func (f *setupFlow) stepPairing() setupStepResult {
	// Only a sign-in pairing in this attempt sets them again: a code pairing
	// after a sign-in to another home must not reuse that home's session.
	setupSignedInUser, setupOwnerIngress = "", nil
	steps := buildSetupPairingWizardSteps()
	view := setupView{Progress: &setupProgress{Step: steps.Pairing, Total: steps.Total, Title: setupText("setup.step.connect")}}
	if !f.relaySkipNote.empty() {
		view.Blocks = append(view.Blocks, setupSuccess(f.relaySkipNote))
		f.relaySkipNote = setupMsg{}
	}
	f.ui.show(view)
	if f.serviceMode {
		// `setup --service` documents that the device credential lands in a
		// protected file — service installs must not depend on a desktop
		// keyring nobody unlocks. Forced here at the pairing step, not at
		// setup entry, so earlier reads of an existing keyring credential
		// stay untouched.
		forceDeviceCredentialFileMode()
	}
	pairedToken, err := runSetupPairingFlow(f.ui.termIn(), f.ui.termOut(), f.paths, &f.cfg, f.legacyTokenStoreUnavailable, f.lifecycleMarker...)
	var onPage *setupPagePairingError
	switch {
	case err == errSetupBack:
		return setupGoTo(f.pairingBackStep)
	case err == errSetupRelayTokenStep:
		// The user explicitly left the device path: verify must judge
		// the token they are about to provide, not the old pairing.
		f.devicePaired = false
		f.manualCredentialFlow = true
		return setupGoTo(setupStepToken)
	case err == errSetupExit:
		return f.cancelled()
	case errors.As(err, &onPage):
		f.page.failRunningRows()
		f.pageProblem, f.pageRetry = pagePairingProblem(onPage)
		f.pageRetryUntil = time.Now().Add(setupPageRetryFor)
		return setupGoTo(setupStepHost)
	case err == errSetupDevicePaired:
		// Secure v1 pairing already stored the device credential and the
		// secure endpoint in the config; there is no relay token to keep,
		// and activation already proved the connection.
		f.devicePaired = true
		f.pairedThisRun = true
		f.token = ""
		f.pairingCredentialReceived = false
		f.manualCredentialFlow = false
		f.verifyFirstReuseFlow = false
		return setupGoTo(setupStepVerify)
	case err != nil:
		if !isSetupFailureReported(err) {
			f.ui.failErr(err)
		}
		return setupDone(1)
	}
	f.token = pairedToken
	f.pairingCredentialReceived = true
	f.pairedThisRun = true
	f.devicePaired = false
	f.manualCredentialFlow = false
	f.verifyFirstReuseFlow = false
	return setupGoTo(setupStepVerify)
}

// secureStorageHandover: why the setup page hands over for the keyring (see
// setupPageState.Handover); "" for any other storage problem.
func secureStorageHandover(err error) string {
	switch inferBasicSetupSecureStorageRecoveryAction(err) {
	case platformSecureStorageRecoveryUnlock:
		return "keyring_locked"
	case platformSecureStorageRecoveryInitialize:
		return "keyring_setup"
	}
	return ""
}
