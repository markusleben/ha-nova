package main

import "strings"

// stepToken is the legacy shared-token path: keep, paste, or generate the
// Relay Auth Token and put it into the NOVA App.
func (f *setupFlow) stepToken() setupStepResult {
	// Any route into the token step is the legacy path by definition:
	// verify must judge the token, never a leftover device pairing.
	f.devicePaired = false
	f.pairedThisRun = false
	f.current = detectSetupStateWithToken(f.paths, f.cfg, f.state, f.target, f.savedTokenBeforeSetup, f.hadSavedTokenBeforeSetup)
	f.existingToken = ""
	if f.relayTokenFlag == "" && f.hadSavedTokenBeforeSetup {
		f.existingToken = f.savedTokenBeforeSetup
	}
	if f.relayTokenFlag != "" {
		f.token = strings.TrimSpace(f.relayTokenFlag)
	}
	resumeWSRecovery := f.relayTokenFlag == "" && strings.TrimSpace(f.existingToken) != "" && f.current.RelayOK && !f.current.WSOK
	f.verifyFirstReuseFlow = false
	steps := buildSetupWizardSteps()
	credentialStep := steps.RelayToken
	if f.pairingFlow && !resumeWSRecovery {
		steps = buildSetupPairingWizardSteps()
		credentialStep = steps.Pairing
	}

	f.ui.show(setupView{
		Progress: &setupProgress{Step: credentialStep, Total: steps.Total, Title: setupText("setup.step.token")},
		Blocks: []setupBlock{
			setupList(setupText("setup.token.separate_access"),
				setupText("setup.token.separate_relay"),
				setupText("setup.token.separate_app"),
			),
			setupParagraph(setupText("setup.token.standalone")),
		},
	})
	switch {
	case f.relayTokenFlag != "":
		f.ui.show(setupView{Blocks: []setupBlock{setupTight(setupText("setup.token.using_flag"))}})
	case resumeWSRecovery:
		f.verifyFirstReuseFlow = true
		f.token = f.existingToken
		f.ui.show(setupView{Blocks: []setupBlock{setupTight(setupText("setup.token.using_saved", "hint", maskSecretHint(f.existingToken)))}})
	default:
		if result, again := f.askToken(); again || result.done {
			return result
		}
	}

	if f.verifyFirstReuseFlow {
		f.ui.show(setupView{Blocks: []setupBlock{setupParagraph(setupText("setup.token.reuse_note"))}})
	}
	return setupGoTo(setupStepVerify)
}

// askToken asks keep/paste/generate and walks through putting a new token
// into the App. again=true hands result back to the step loop.
func (f *setupFlow) askToken() (result setupStepResult, again bool) {
	if f.existingToken != "" {
		f.ui.show(setupView{Blocks: []setupBlock{setupTight(setupText("setup.token.found_saved", "hint", maskSecretHint(f.existingToken)))}})
	}

	choice, err := promptSetupTokenChoiceInteractive(f.ui.reader, f.ui.out, f.existingToken != "")
	if err == errSetupBack {
		if f.pairingFlow {
			return setupGoTo(setupStepPairing), true
		}
		return setupGoTo(setupStepRelayInstall), true
	}
	if err != nil {
		return f.inputFailed(err), true
	}

	f.tokenChanged = false
	switch choice {
	case "keep":
		f.token = f.existingToken
		f.verifyFirstReuseFlow = true
	case "paste":
		f.ui.show(setupView{Blocks: []setupBlock{setupBreak}})
		pasted, err := f.ui.input(setupText("setup.token.paste_prompt"))
		if err == errSetupBack {
			return setupGoTo(setupStepToken), true
		}
		if err != nil {
			return f.inputFailed(err), true
		}
		f.token = strings.TrimSpace(pasted)
		if f.token == "" {
			f.ui.show(setupView{Blocks: []setupBlock{setupErrorBlock(setupText("setup.token.none_entered"))}})
			return setupGoTo(setupStepToken), true
		}
		f.verifyFirstReuseFlow = true
	case "generate":
		generated, err := generateRelayToken()
		if err != nil {
			return f.fail(setupText("setup.error.generate_token", "error", err)), true
		}
		f.token = generated
		f.tokenChanged = true
		f.verifyFirstReuseFlow = false
		f.ui.show(setupView{Blocks: []setupBlock{setupList(setupText("setup.token.generated"), setupData(f.token))}})
	}

	if f.tokenChanged {
		if err := copyToClipboardForSetup(f.token); err == nil {
			f.ui.show(setupView{Blocks: []setupBlock{setupSuccess(setupText("setup.token.copied"))}})
		}
		appPage := haRelayAppPageURL(f.cfg.HAURL)
		f.ui.show(setupView{Blocks: []setupBlock{
			setupList(setupText("setup.token.next_on_app_page"),
				setupText("setup.token.app_step_tab"),
				setupText("setup.token.app_step_paste"),
				setupText("setup.token.app_step_save"),
				setupText("setup.token.app_step_restart"),
			),
			setupLink(setupText("setup.link.will_open"), appPage),
		}})
		if _, err := f.ui.input(setupText("setup.prompt.open_browser")); err == errSetupBack {
			return setupGoTo(setupStepToken), true
		} else if err != nil {
			return f.inputFailed(err), true
		}
		openAnnouncedBrowserURL(f.ui.out, appPage)
		if _, err := f.ui.input(setupText("setup.token.wait_saved")); err == errSetupBack {
			return setupGoTo(setupStepToken), true
		} else if err != nil {
			return f.inputFailed(err), true
		}
	}
	return setupStepResult{}, false
}

// inputFailed ends setup after a question failed: "exit" cancels, anything
// else is an error.
func (f *setupFlow) inputFailed(err error) setupStepResult {
	if err == errSetupExit {
		return f.cancelled()
	}
	return f.failErr(err)
}
