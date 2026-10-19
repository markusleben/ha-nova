package main

import "strings"

// start detects where setup stands and asks for the client and the
// connection mode: interrupted retirements and activations, Cloud
// recovery, named profiles. A done result ends setup.
func (f *setupFlow) start() setupStepResult {
	f.ui.header()
	if f.target == "" && strings.TrimSpace(f.cfg.HAHost) == "" && strings.TrimSpace(f.cfg.HAURL) == "" &&
		strings.TrimSpace(f.hostFlag) == "" && strings.TrimSpace(f.haURLFlag) == "" &&
		strings.TrimSpace(f.relayURLFlag) == "" && strings.TrimSpace(f.relayTokenFlag) == "" {
		// Flag-driven runs are not fully interactive first runs even before
		// the overrides are applied to cfg below — skip the intro for them.
		renderSetupIntro(f.ui.out)
	}
	namedRetirementOnly := false
	retirementPending := false
	if activeServerProfile() != defaultServerProfileName {
		var retirementErr error
		retirementPending, retirementErr =
			deviceCredentialRetirementCheckpointExistsForProfile(
				f.paths,
				activeServerProfile(),
			)
		if retirementErr != nil {
			return f.fail(setupText("setup.error.inspect_retirement", "error", retirementErr))
		}
		namedRetirementOnly = namedSetupIsRetirementOnly(
			f.cfg,
			retirementPending,
		)
	}
	namedRequestAllowed := namedSetupRequestAllowed(
		f.cfg,
		retirementPending,
		f.target,
		f.serviceMode,
		f.hostFlag,
		f.haURLFlag,
		f.relayURLFlag,
		f.relayTokenFlag,
	)
	f.explicitLocalSetup = f.serviceMode ||
		strings.TrimSpace(f.hostFlag) != "" ||
		strings.TrimSpace(f.haURLFlag) != "" ||
		strings.TrimSpace(f.relayURLFlag) != "" ||
		strings.TrimSpace(f.relayTokenFlag) != ""
	var cloudRecoveryHandled bool
	var cloudRecoveryCode int
	f.cfg, cloudRecoveryHandled, cloudRecoveryCode =
		handleInteractiveCloudRecoveryBeforeClients(
			f.ui.reader,
			f.ui.out,
			f.paths,
			f.cfg,
			f.serviceMode,
			f.explicitLocalSetup,
			f.lifecycleMarker...,
		)
	if cloudRecoveryHandled {
		return setupDone(cloudRecoveryCode)
	}
	if !namedRequestAllowed {
		renderNamedSetupRequestError()
		return setupDone(1)
	}
	if err := resumeSetupDeviceCredentialRetirement(f.paths, f.cfg); err != nil {
		return f.fail(setupText("setup.error.finish_retirement", "error", err))
	}
	if namedRetirementOnly {
		return setupDone(0)
	}
	resumedActivation, resumeActivationErr :=
		resumeInteractiveSetupPendingActivation(
			f.paths,
			&f.cfg,
			f.lifecycleMarker...,
		)
	if resumeActivationErr != nil {
		printPendingActivationResumeError(resumeActivationErr)
		return setupDone(1)
	}
	if resumedActivation {
		f.ui.info(setupText("setup.resumed_pairing"))
	}
	f.setupChanged = resumedActivation
	choices, err := buildSetupClientChoices(f.paths, f.state)
	if err != nil {
		return f.failErr(err)
	}
	f.choices = choices
	return f.chooseClientAndMode()
}

// chooseClientAndMode asks for the AI client (unless a target was given)
// and, on an unconstrained fresh setup, how this computer connects.
func (f *setupFlow) chooseClientAndMode() setupStepResult {
	clientsResolved := false
	for {
		if f.target == "" {
			answer, selectErr := promptSetupClientForWizard(
				f.ui.reader,
				f.ui.out,
				f.choices,
			)
			if selectErr == errSetupClientPrerequisite {
				return setupDone(0)
			}
			if selectErr == errSetupExit {
				return f.cancelled()
			}
			if selectErr != nil {
				return f.failErr(selectErr)
			}
			f.target = answer
			f.promptedClient = true
			clientsResolved = false
		}
		if !clientsResolved {
			var err error
			f.selectedClients, f.skippedClients, err = resolveSetupClientsWithChoices(
				f.choices,
				f.target,
			)
			if err != nil {
				return f.failErr(err)
			}
			clientsResolved = true
		}

		if remoteOnlyCloudSetup(f.cfg) && !f.explicitLocalSetup {
			return setupDone(resumeInteractiveCloudOnlySetup(
				f.ui.reader,
				f.ui.out,
				f.paths,
				f.cfg,
				&f.state,
				f.target,
				f.selectedClients,
				f.skippedClients,
				f.lifecycleMarker...,
			))
		}

		f.connectionMode = setupConnectionLocal
		if hybridCloudSetupPending(f.cfg) {
			f.connectionMode = setupConnectionHybrid
		}
		if !shouldOfferSetupConnectionMode(
			f.cfg,
			f.hostFlag,
			f.haURLFlag,
			f.relayURLFlag,
			f.relayTokenFlag,
			f.serviceMode,
		) {
			return setupStepResult{}
		}
		f.connectionModeAsked = true
		mode, err := promptSetupConnectionMode(f.ui.reader, f.ui.out)
		f.connectionMode = mode
		if err == errSetupBack {
			f.target = ""
			f.selectedClients = nil
			f.skippedClients = nil
			clientsResolved = false
			continue
		}
		if err == errSetupExit {
			return f.cancelled()
		}
		if err != nil {
			return f.failErr(err)
		}
		if f.connectionMode != setupConnectionCloud {
			return setupStepResult{}
		}
		exit, back := runInteractiveCloudOnlySetupForWizard(
			f.ui.reader,
			f.ui.out,
			f.paths,
			f.cfg,
			&f.state,
			f.target,
			f.selectedClients,
			f.skippedClients,
			f.lifecycleMarker...,
		)
		if back {
			continue
		}
		return setupDone(exit)
	}
}
