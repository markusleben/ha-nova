package main

import (
	"slices"
	"strings"
)

func (f *setupFlow) stepVerify() setupStepResult {
	if f.cfg.RelayBaseURL == "" {
		f.cfg.RelayBaseURL = deriveRelayURLFromHA(f.cfg.HAURL, f.cfg.HAHost)
	}
	steps := f.wizardSteps()
	f.ui.show(setupView{Progress: &setupProgress{Step: steps.Verify, Total: steps.Total, Title: setupText("setup.step.verify")}})
	if f.devicePaired {
		return f.verifyDevice()
	}

	if f.verifyFirstReuseFlow {
		f.ui.show(setupView{Blocks: []setupBlock{setupTight(setupText("setup.verify.using_address", "url", f.cfg.HAURL))}})
	}
	credentialRepair := setupCredentialRepairNone
	if strings.TrimSpace(f.relayTokenFlag) == "" {
		credentialRepair = setupCredentialRepairToken
		if !f.serviceMode && !f.manualCredentialFlow {
			credentialRepair = setupCredentialRepairPairing
		}
	}
	issue, ok, err := verifySetupConnection(f.ui.termIn(), f.ui.termOut(), f.cfg, f.token, f.verifyFirstReuseFlow, credentialRepair, f.pairingCredentialReceived)
	switch {
	case err == errSetupBack:
		if f.pairingFlow {
			f.pairingBackStep = setupStepVerify
			return setupGoTo(setupStepPairing)
		}
		if f.relayTokenFlag != "" {
			return setupGoTo(setupStepHost)
		}
		return setupGoTo(setupStepToken)
	case err == errSetupRelayTokenStep:
		f.pairingFlow = false
		return setupGoTo(setupStepToken)
	case err == errSetupPairingStep:
		f.pairingFlow = true
		f.pairingBackStep = setupStepVerify
		return setupGoTo(setupStepPairing)
	case err == errSetupHostStep:
		f.hostChangeRetry = true
		return setupGoTo(setupStepHost)
	case err == errSetupInstallStep:
		// The user asked for full guidance from the repair menu:
		// repository/app install and client credential setup
		// for the current address.
		f.verifyFirstReuseFlow = false
		return setupGoTo(setupStepRelayInstall)
	case err != nil:
		return f.inputFailed(err)
	}
	if !ok {
		if err := persistInteractiveSetupStateWithRecovery(f.ui.termIn(), f.ui.termOut(), f.paths, f.cfg, &f.state, f.savedTokenBeforeSetup, f.hadSavedTokenBeforeSetup, f.token, &f.secureStorageRecovery, f.lifecycleMarker...); err != nil {
			return f.failErr(err)
		}
		f.ui.outcome(setupOutcome{Kind: setupOutcomeIncomplete, Issue: issue})
		return setupDone(1)
	}
	// The token path just verified successfully. A leftover device
	// pairing (this branch is legacy-only; the device branch continues
	// to the skills above) would win transport resolution on the next run
	// and wedge the install on a dead pairing — retire it for good.
	// Link paused: no retirement (it changes credentials, and Link data
	// refuses it).
	if err := persistInteractiveSetupStateWithRecoveryMode(f.ui.termIn(), f.ui.termOut(), f.paths, f.cfg, &f.state, f.savedTokenBeforeSetup, f.hadSavedTokenBeforeSetup, f.token, &f.secureStorageRecovery, !f.cloudPausedAtStart, f.lifecycleMarker...); err != nil {
		return f.failErr(err)
	}
	return setupGoTo(setupStepSkills)
}

// verifyDevice checks a paired device's secure endpoint, saves the setup,
// and adds Home Assistant Link for a hybrid setup.
func (f *setupFlow) verifyDevice() setupStepResult {
	// Honest about what is verified: the secure device endpoint.
	f.ui.show(setupView{Blocks: []setupBlock{setupTight(setupText("setup.verify.device_checking"))}})
	revoked := func() setupStepResult { // removed on the NOVA page: no App to wait for (lane E3)
		f.ui.show(setupView{Blocks: []setupBlock{setupErrorBlock(setupText("setup.verify.device_revoked"))}})
		if f.page != nil && !f.page.inTerminal() { // the page's step 1 signs in again and says why
			f.devicePaired, f.deviceAlreadyPaired, f.pageRemoved, f.pageRerunOK = false, false, true, false
			return setupGoTo(setupStepHost)
		}
		f.pairingBackStep = setupStepHost // back: a different address, not this check again
		return setupGoTo(setupStepPairing)
	}
	health := verifyDeviceHealth(f.cfg)
	if health == deviceRevoked {
		return revoked()
	}
	if health != deviceOK {
		f.ui.show(setupView{Blocks: []setupBlock{setupErrorBlock(setupText("setup.verify.device_not_answering"))}})
		f.textOnly("")
		if retryErr := promptSetupDeviceVerifyRetry(f.ui.termIn(), f.ui.termOut()); retryErr != nil {
			if retryErr == errSetupBack {
				// Do exactly what the prompt advertises.
				f.pairingBackStep = setupStepVerify
				return setupGoTo(setupStepPairing)
			}
			return f.inputFailed(retryErr)
		}
		switch verifyDeviceHealth(f.cfg) {
		case deviceOK:
		case deviceRevoked:
			return revoked()
		default:
			f.pairingBackStep = setupStepVerify
			return setupGoTo(setupStepPairing)
		}
	}
	f.ui.show(setupView{Blocks: []setupBlock{setupSuccess(setupText("setup.verify.device_ok"))}})
	forgetUnprovenRelayIdentity(&f.cfg)
	if err := persistDeviceSetupState(f.paths, f.cfg, &f.state, f.lifecycleMarker...); err != nil {
		return f.failErr(err)
	}
	if f.connectionMode == setupConnectionHybrid && !f.cfg.Cloud.ready() && !f.cloudPausedAtStart {
		f.ui.show(setupView{Blocks: []setupBlock{setupParagraph(setupText("setup.verify.adding_link"))}})
		cfg, err := addHybridCloudAfterLocal(
			f.paths,
			f.cfg,
			f.lifecycleMarker...,
		)
		f.cfg = cfg
		if err != nil {
			if handlePausedCloudOwnerPairing(
				f.ui.termOut(),
				f.paths,
				err,
			) {
				f.cloudSetupPaused = true
				return setupGoTo(setupStepSkills)
			}
			renderCloudFailure(f.ui.termOut(), f.paths, err)
			f.cloudSetupIncomplete = true
			return setupGoTo(setupStepSkills)
		}
		f.ui.show(setupView{Blocks: []setupBlock{setupSuccess(setupText("setup.verify.link_ok"))}})
	}
	return setupGoTo(setupStepSkills)
}

// stepSkills installs the skills, then finishes: the Home Assistant Link
// offer, the phone and Claude Code rule offers, the first look, and the
// outcome.
func (f *setupFlow) stepSkills() setupStepResult {
	if f.noClient { // nothing to teach yet; finish says what to install
		finalizeServiceTokenFileMigration(f.formerServiceTokenFile, f.token)
		return f.afterSkills()
	}
	steps := f.wizardSteps()
	view := setupView{Progress: &setupProgress{Step: steps.Skills, Total: steps.Total, Title: setupText("setup.step.skills")}}
	if f.target == "all" && len(f.selectedClients) > 0 {
		view.Blocks = append(view.Blocks, setupTight(setupText("setup.skills.will_install", "clients", strings.Join(f.selectedClients, ", "))))
		if len(f.skippedClients) > 0 {
			view.Blocks = append(view.Blocks,
				setupTight(setupText("setup.skills.skipping", "clients", strings.Join(f.skippedClients, ", "))),
				setupBreak,
			)
		}
	}
	f.ui.show(view)
	if err := f.ui.working(setupText("setup.skills.installing", "client", setupClientLabel(f.target)), func() error {
		return withClientMutationLock(f.paths, func() error {
			if err := installClientsAndSaveStateUnlocked(f.paths, &f.state, f.selectedClients, saveState, f.lifecycleMarker...); err != nil {
				return err
			}
			finalizeServiceTokenFileMigration(f.formerServiceTokenFile, f.token)
			return nil
		})
	}); err != nil {
		f.ui.fail(setupText("setup.error.install_clients", "error", err))
		f.ui.outcome(setupOutcome{Kind: setupOutcomeIncomplete, Issue: setupIssueSkillsInstall})
		return setupDone(1)
	}
	return f.afterSkills()
}

// afterSkills: the Link offer, the lifecycle close, and finish.
func (f *setupFlow) afterSkills() setupStepResult {
	exit := 0
	onPage := f.page != nil && !f.page.inTerminal()
	if f.cloudSetupPaused && !onPage { // the page never runs with Link unfinished (setupPageCloudOK)
		f.showLinkPausedOutcome()
	} else if f.cloudSetupIncomplete {
		f.ui.outcome(setupOutcome{Kind: setupOutcomeIncomplete, Issue: setupIssueCloudAccess})
		exit = 1
	}
	if exit == 0 && onPage {
		// The page asks step 3 itself (phone, Claude Code rule, Link) once
		// setup is complete, like the terminal's offers in finish.
		f.pageControlled = true
	} else if exit == 0 && !f.connectionModeAsked && !f.serviceMode && !f.cloudSetupPaused {
		armSetupNextPromptSkipsStaleBlankInput()
		var cloudAttempted bool
		var cloudCode int
		f.cfg, cloudAttempted, cloudCode = maybeOfferCloudForCompletedSetup(
			f.ui.termIn(),
			f.ui.termOut(),
			f.paths,
			f.cfg,
			false,
			f.lifecycleMarker...,
		)
		clearSetupNextPromptSkipsStaleBlankInput()
		if cloudAttempted && cloudCode != 0 {
			exit = cloudCode
		}
		if exit == 0 && cloudAttempted &&
			f.cfg.Cloud != nil && !f.cfg.Cloud.ready() {
			exit = cloudCode
		}
	}
	if err := completeSetupLifecycle(f.paths, f.lifecycleMarker...); err != nil {
		return f.fail(setupText("setup.error.finalize_lifecycle", "error", err))
	}
	if f.pageControlled && len(f.lifecycleMarker) > 0 {
		// The generation setup completed with: an uninstall during step 3
		// rotates it, and the page's Link add then fails closed.
		generation, err := readInstallLifecycleGeneration(f.paths)
		if err != nil {
			return f.failErr(err)
		}
		f.pageGeneration = generation
	}
	if exit != 0 {
		return setupDone(exit)
	}
	return f.finish()
}

// finish asks the optional offers (never failing setup) and shows the
// outcome. The offers come first: the outcome clears the screen, so the
// Claude rule's note prints after it.
func (f *setupFlow) finish() setupStepResult {
	if f.pageControlled {
		return f.pageFinish()
	}
	interactive := !f.serviceMode && f.ui.interactive()
	if f.pairedThisRun && !f.cloudSetupPaused && interactive {
		offerPhoneLink(f.ui.termIn(), f.ui.termOut(), f.paths)
	}
	if f.noClient { // connected, but no AI app to try it in yet (lane D1)
		labels, err := setupInstallableClientLabels(f.paths)
		if err != nil {
			return f.failErr(err)
		}
		f.ui.show(setupView{Blocks: []setupBlock{
			setupSuccess(setupText("setup.done.connected_no_client")),
			setupParagraph(setupText("setup.clients.none_after_pairing", "clients", strings.Join(labels, ", "))),
		}})
		if interactive && f.pairedThisRun && !f.cloudSetupPaused && setupOwnerIngress != nil {
			f.printChatApps(f.offerChatApps()) // Claude and ChatGPT on the web need no AI app here
		}
		if !f.serviceMode && f.current.ConfigOK {
			if err := printCompletedSetupOverview(f.ui.termOut(), f.paths, f.cfg); err != nil {
				return f.failErr(err)
			}
		}
		return setupDone(0)
	}
	ruleNote := ""
	if interactive && slices.Contains(f.selectedClients, "claude") {
		ruleNote = offerClaudeRules(f.ui.termIn(), f.ui.termOut(), f.paths)
	}
	chatURL, chatNote := "", ""
	if interactive && f.pairedThisRun && !f.cloudSetupPaused && setupOwnerIngress != nil {
		chatURL, chatNote = f.offerChatApps()
	}
	// The read-only first look after a pairing in this run
	// (docs/work/2026-10-16-ha-sign-in-pairing-spec.md, "First look").
	var look *setupFirstLook
	if f.pairedThisRun && !f.cloudSetupPaused {
		look = loadSetupFirstLook(f.ui.termOut(), f.paths)
	}
	if f.cfg.Cloud.ready() && f.cfg.RoutePolicy == routePolicyAutomatic {
		f.ui.outcome(setupOutcome{Kind: setupOutcomeLinkReady, Look: look})
	} else if !f.cloudSetupPaused {
		f.ui.outcome(setupOutcome{Kind: setupOutcomeComplete, Clients: f.selectedClients, Look: look})
	}
	if ruleNote != "" {
		f.ui.note(ruleNote)
	}
	f.printChatApps(chatURL, chatNote) // after the outcome, which clears the screen
	if !f.serviceMode && f.current.ConfigOK {
		if err := printCompletedSetupOverview(f.ui.termOut(), f.paths, f.cfg); err != nil {
			return f.failErr(err)
		}
	}
	return setupDone(0)
}

func (f *setupFlow) printChatApps(url, note string) {
	if note != "" {
		f.ui.note(note)
	}
	if url != "" {
		printChatAppsHowTo(f.ui.termOut(), url)
	}
}

// offerChatApps is step 3's chat-app row in the terminal: one question, only
// when the row would show, default "Not now". Returns the connector address
// once they are on, and a line about how it went; both print after the
// outcome, which clears the screen.
func (f *setupFlow) offerChatApps() (url, note string) {
	offer, ok := readChatAppsOffer(setupOwnerIngress, f.paths, func() string { return setupLinkRemoteOrigin(f.cfg) })
	switch {
	case !ok:
		return "", ""
	case offer.url == "" && offer.state.Enabled:
		return "", setupT("setup.chat.on_noaddress")
	case offer.url == "":
		return "", setupT("setup.chat.need_address")
	case offer.state.Enabled:
		note := ""
		if !offer.loaded { // on, but a restart never came
			note = setupT("setup.chat.problem_slow")
		}
		if err := handOverOwnerPhone(setupOwnerIngress, f.paths, setupSignedInUser, &offer.state); err != nil {
			note = setupT("setup.chat.problem_phone") // the phone linked just before
		}
		return offer.url, note
	case !askChatAppsConsent(f.ui.termIn(), f.ui.termOut(), true, offer.restart, chatAppsPhoneName(f.paths, setupSignedInUser, offer.state)):
		return "", setupT("setup.chat.skipped")
	}
	note, on := turnOnChatAppsInTerminal(f.ui.termOut(), f.paths, setupOwnerIngress, offer, setupSignedInUser)
	if !on {
		return "", note
	}
	return offer.url, note
}
