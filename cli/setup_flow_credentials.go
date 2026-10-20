package main

import "strings"

// prepareCredentials settles where this run's credentials live (desktop
// keyring, service file, file-backed device pairing), applies endpoint
// overrides, short-circuits an already complete setup, and picks the first
// step.
func (f *setupFlow) prepareCredentials() setupStepResult {
	restoreTokenFileOverride := func() {}
	f.cleanups = append(f.cleanups, func() { restoreTokenFileOverride() })
	if !f.serviceMode {
		var liftTokenFileSuppression func()
		f.cfg, f.formerServiceTokenFile, f.formerServiceToken, liftTokenFileSuppression = disableServiceRelayTokenFile(f.paths, f.cfg)
		f.cleanups = append(f.cleanups, liftTokenFileSuppression)
	}
	if f.serviceMode {
		if f.target == "all" {
			return f.fail(setupText("setup.error.service_needs_client"))
		}
		if err := requireSelectedClientServiceCredentials(f.paths, f.selectedClients); err != nil {
			return f.failErr(err)
		}
		// Only after the service target and client checks passed: a re-setup
		// with a healthy keyring pairing short-circuits to verify and never
		// reaches the pairing step, so a readable keyring device credential
		// migrates to the private-file backend now (the service contract).
		migrated := false
		migrateErr := withSetupLifecycleLock(f.paths, f.lifecycleMarker, func() error {
			var err error
			migrated, err = migrateKeyringDeviceCredentialToFile()
			return err
		})
		if migrateErr != nil {
			return f.fail(setupText("setup.error.service_move_credential", "error", migrateErr))
		}
		if migrated {
			f.setupChanged = true
			f.ui.info(setupText("setup.service.credential_moved"))
		}
		// Read any already-stored token BEFORE the file override redirects
		// token reads, so an existing desktop-keyring token can be offered
		// for migration into the service token file.
		if existing, err := readRelayAuthToken(); err == nil {
			f.formerServiceToken = strings.TrimSpace(existing)
		}
		f.cfg = enableServiceRelayTokenFile(f.paths, f.cfg)
		restoreTokenFileOverride = withRelayAuthTokenFileOverride(f.cfg.RelayTokenFile)
	}

	f.tokenStoragePreflightErr = relayAuthTokenSetupPreflightForSetup()
	// Service credentials stay a client-scoped deployment decision: only
	// offer the mid-flow switch for a specific target, mirroring the
	// explicit `--service all` rejection above.
	if !f.serviceMode && f.target != "all" {
		_, serviceClientID, hasServiceCredentials, serviceErr := selectedClientsServiceCredentialHint(f.paths, f.selectedClients)
		if serviceErr != nil {
			return f.failErr(serviceErr)
		}
		if hasServiceCredentials && offerServiceCredentialsQuestion(f.tokenStoragePreflightErr) {
			f.textOnly()
			useServiceCredentials, err := promptSetupServiceCredentialsInteractive(f.ui.termIn(), f.ui.termOut(), serviceClientID)
			if err == errSetupExit {
				return f.cancelled()
			}
			if err != nil {
				return f.failErr(err)
			}
			if useServiceCredentials {
				f.serviceMode = true
				f.formerServiceTokenFile = ""
				f.formerServiceToken = ""
				restoreTokenFileOverride()
				f.cfg = enableServiceRelayTokenFile(f.paths, f.cfg)
				restoreTokenFileOverride = withRelayAuthTokenFileOverride(f.cfg.RelayTokenFile)
				f.tokenStoragePreflightErr = relayAuthTokenSetupPreflightForSetup()
			}
		}
	}
	if result := f.checkTokenStorage(); result.done {
		return result
	}
	if f.existingToken == "" {
		f.existingToken = f.savedTokenBeforeSetup
	}
	if f.existingToken == "" && !f.hadSavedTokenBeforeSetup && f.formerServiceToken != "" {
		// Returning from service to desktop mode: prefill the token from the
		// former service token file so the user does not have to re-paste it,
		// but deliberately do NOT mark it as a previously stored token —
		// persistence must treat it as new and write it into the OS keyring
		// before the saved config stops referencing the token file.
		f.existingToken = f.formerServiceToken
	}

	// Activation recovery above intentionally ran before applying unconfirmed
	// local endpoint overrides.
	f.overrideApplied = strings.TrimSpace(f.hostFlag) != "" || strings.TrimSpace(f.haURLFlag) != "" || strings.TrimSpace(f.relayURLFlag) != ""
	if f.overrideApplied {
		var err error
		f.cfg, err = applySetupFlagOverrides(f.cfg, f.hostFlag, f.haURLFlag, f.relayURLFlag)
		if err != nil {
			return f.failErr(err)
		}
	}

	f.current = detectSetupStateForAssessment(f.paths, f.cfg, f.state, f.target, f.savedTokenBeforeSetup, f.hadSavedTokenBeforeSetup)
	currentLifecycleMarker := f.lifecycleMarker
	if !f.setupChanged && !f.overrideApplied &&
		!(len(f.lifecycleMarker) > 1 && len(f.lifecycleMarker[1]) > 0) {
		currentLifecycleMarker = nil
	}
	onPageRerun := f.page != nil && !f.page.inTerminal() && strings.TrimSpace(f.cfg.HAHost) != "" && strings.TrimSpace(f.cfg.HAURL) != ""
	if f.tokenStoragePreflightErr == nil && !onPageRerun {
		if handled, code := maybeHandleInteractiveSetupCurrentState(f.ui.termIn(), f.ui.termOut(), f.paths, f.cfg, f.current, f.overrideApplied, f.serviceMode, currentLifecycleMarker...); handled {
			return setupDone(code)
		}
	}

	f.token = f.existingToken
	_, _, _, f.deviceAlreadyPaired, _ = relayFunctionalTransportForDoctor(f.cfg)
	if f.tokenStoragePreflightErr != nil {
		return setupGoTo(setupStepSecureStorageRecovery)
	}
	if result, handled := f.pageRerun(currentLifecycleMarker); handled {
		return result
	}
	return setupGoTo(f.routeKnownAddress(setupStepHost))
}

// checkTokenStorage reads a token saved before this run, or decides how to
// go on without a usable keyring.
func (f *setupFlow) checkTokenStorage() setupStepResult {
	if f.tokenStoragePreflightErr == nil && !hybridCloudSetupPending(f.cfg) {
		if savedToken, err := readRelayAuthTokenForSetup(); err == nil && strings.TrimSpace(savedToken) != "" {
			f.savedTokenBeforeSetup = strings.TrimSpace(savedToken)
			f.hadSavedTokenBeforeSetup = true
		} else if err != nil && setupSecureStorageRecoveryAvailableNow(err) {
			f.tokenStoragePreflightErr = err
			f.tokenStorageRecoveryReadBlocked = true
		} else if err != nil && !isMissingRelayAuthTokenError(err) {
			printRelayTokenStorageSetupWarning(err)
		}
		return setupGoOn
	}
	if setupSecureStorageRecoveryAvailableNow(f.tokenStoragePreflightErr) {
		return setupGoOn
	}
	// A missing OS keyring is only fatal when a relay TOKEN must actually be
	// stored. The default onboarding path is secure device pairing, which
	// brings its own file-backed credential storage (device_credential_storage.go),
	// so a headless box (container/SSH with no Secret Service) must still reach
	// the pairing step instead of exiting here on the legacy-token preflight.
	// Only an explicit --relay-token makes the token path mandatory here:
	// service installs default to secure pairing as well (file-backed device
	// credential), so a missing keyring must not abort their onboarding.
	tokenPathRequired := strings.TrimSpace(f.relayTokenFlag) != ""
	noKeyringBackend := isDesktopKeyringSessionUnavailableError(f.tokenStoragePreflightErr) ||
		isDesktopKeyringUnavailableError(f.tokenStoragePreflightErr)
	// The legacy /pair fallback (which DOES need a keyring token store) is
	// guarded at the pairing step, failing before it consumes a one-time
	// code — see runSetupPairingFlow. Deciding it here is impossible for the
	// plain interactive flow, which discovers the relay URL later.
	deviceStorageViable := false
	if !tokenPathRequired && noKeyringBackend {
		_ = withSetupLifecycleLock(f.paths, f.lifecycleMarker, func() error {
			deviceStorageViable = deviceCredentialStorageViable()
			return nil
		})
	}
	printRelayTokenStorageSetupWarning(f.tokenStoragePreflightErr)
	if !tokenPathRequired && noKeyringBackend && deviceStorageViable {
		f.ui.info(setupText("setup.storage.file_backed"))
		f.tokenStoragePreflightErr = nil
		// The legacy /pair token store is unavailable, so the pairing step
		// must not fall back to it.
		f.legacyTokenStoreUnavailable = true
		return setupGoOn
	}
	return f.failErr(relayAuthTokenSetupSaveError(f.tokenStoragePreflightErr))
}
