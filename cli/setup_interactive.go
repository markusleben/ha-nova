package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

var resolveHAURLBaseForSetup = resolveHomeAssistantURLBase
var probeHTTPForSetup = probeHTTP
var fetchRelayHealthForSetup = fetchRelayHealth
var probeRelayWSPingForSetup = probeRelayWSPing
var readRelayAuthTokenForSetup = readRelayAuthToken

func printRelayTokenStorageSetupWarning(err error) {
	switch {
	case isDesktopKeyringSessionUnavailableError(err):
		printHumanWarn("secure storage unavailable in this Linux session; run HA NOVA from a terminal inside the Linux desktop session on this machine so local secure storage is available")
	case isDesktopKeyringUnavailableError(err):
		printHumanWarn("secure storage unavailable on this Linux machine; start a Secret Service provider (for example GNOME Keyring or KWallet Secrets) before finishing setup")
	case isDesktopKeyringInitializationRequiredError(err):
		printHumanWarn("secure storage is present but not initialized on this Linux machine; finish local secure storage setup before finishing setup")
	case isDesktopKeyringLockedError(err):
		printHumanWarn("secure storage is present but locked on this Linux machine; unlock the default keyring before finishing setup")
	case isDesktopKeyringSetupRequiredError(err):
		printHumanWarn("secure storage is present but not ready on this Linux machine; finish local secure storage setup before finishing setup")
	case err != nil:
		printHumanWarn("%s", relayAuthTokenProblemMessage(err))
	}
}

func maybeHandleInteractiveSetupCurrentState(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, current setupState, overrideApplied, serviceMode bool, lifecycleMarker ...[]byte) (bool, int) {
	if !(current.ConfigOK || current.TokenOK || current.RelayOK || current.WSOK || current.SkillsOK) {
		return false, 0
	}

	renderSetupHeader(out)
	renderSetupStatusSummary(out, current)
	if cfg.Cloud != nil && !cloudRemoteFeatureAvailable() {
		fmt.Fprintln(
			out,
			"  Home Assistant Cloud access is unavailable because this build or platform has Cloud setup disabled.",
		)
		renderCloudCheckpointActions(out, paths, cfg, false)
		return true, 1
	}
	if current.IsComplete() {
		if overrideApplied {
			if err := ensureProfileIdentityForSetup(paths, &cfg); err != nil {
				printHumanErr("cannot prepare the server profile identity: %s", err)
				return true, 1
			}
			if err := saveSetupConfigWithLifecycle(paths, cfg, lifecycleMarker...); err != nil {
				printHumanErr("cannot save config: %s", err)
				return true, 1
			}
		}
		var cloudAttempted bool
		var cloudCode int
		cfg, cloudAttempted, cloudCode = maybeOfferCloudForCompletedSetup(reader, out, paths, cfg, serviceMode, lifecycleMarker...)
		if cloudAttempted && cloudCode != 0 {
			return true, cloudCode
		}
		if cloudAttempted && cfg.Cloud != nil && !cfg.Cloud.ready() {
			return true, cloudCode
		}
		renderSetupAlreadyDoneBanner(out, cfg.RelaySecureBaseURL == "" && cfg.RelayBaseURL != "")
		return true, 0
	}
	if summary := current.SkipSummary(); summary != "" {
		fmt.Fprintf(out, "  Already done: %s\n", summary)
	}
	if writerSupportsTTYForSetup(out) {
		_, err := promptWizardLineFromReader(reader, out, "Press Enter to continue setup", "")
		if err == errSetupExit {
			renderSetupCancelledNote(os.Stdout)
			return true, 0
		}
		if err != nil && err != errSetupBack {
			printHumanErr("%s", err)
			return true, 1
		}
	}
	return false, 0
}

func saveSetupConfigWithLifecycle(paths runtimePaths, cfg runtimeConfig, lifecycleMarker ...[]byte) error {
	return withSetupLifecycleLock(paths, lifecycleMarker, func() error {
		return saveConfig(paths, cfg)
	})
}

func saveSetupConfigWithLifecycleUnlocked(
	paths runtimePaths,
	cfg runtimeConfig,
	lifecycleMarker ...[]byte,
) error {
	if err := ensureSetupLifecycleCurrent(paths, lifecycleMarker...); err != nil {
		return err
	}
	if err := saveConfig(paths, cfg); err != nil {
		return err
	}
	return refreshSetupConfigSnapshot(paths, lifecycleMarker)
}

func promptYesNoFromReader(reader *bufio.Reader, out io.Writer, label string, defaultYes bool) (bool, error) {
	return promptYesNoWithOptions(reader, out, label, defaultYes, false)
}

func promptLineFromReader(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	return promptLineWithOptions(reader, out, label, defaultValue, false)
}

func generateRelayToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func maskSecretHint(value string) string {
	if len(value) <= 8 {
		return "***"
	}
	return "***" + value[len(value)-4:]
}

func interactiveSetup(paths runtimePaths, cfg runtimeConfig, state installState, target string, hostFlag, haURLFlag, relayURLFlag, relayTokenFlag string, serviceMode bool, lifecycleMarker ...[]byte) int {
	const (
		setupStageClient = iota
		setupStageSecureStorageRecovery
		setupStageHost
		setupStageRelayInstall
		setupStageToken
		setupStagePairing
		setupStageVerify
		setupStageSkills
	)

	reader := bufio.NewReader(os.Stdin)
	renderSetupHeader(os.Stdout)
	if target == "" && strings.TrimSpace(cfg.HAHost) == "" && strings.TrimSpace(cfg.HAURL) == "" &&
		strings.TrimSpace(hostFlag) == "" && strings.TrimSpace(haURLFlag) == "" &&
		strings.TrimSpace(relayURLFlag) == "" && strings.TrimSpace(relayTokenFlag) == "" {
		// Flag-driven runs are not fully interactive first runs even before
		// the overrides are applied to cfg below — skip the intro for them.
		renderSetupIntro(os.Stdout)
	}
	namedRetirementOnly := false
	retirementPending := false
	if activeServerProfile() != defaultServerProfileName {
		var retirementErr error
		retirementPending, retirementErr =
			deviceCredentialRetirementCheckpointExistsForProfile(
				paths,
				activeServerProfile(),
			)
		if retirementErr != nil {
			printHumanErr(
				"cannot inspect interrupted device credential retirement: %s",
				retirementErr,
			)
			return 1
		}
		namedRetirementOnly = namedSetupIsRetirementOnly(
			cfg,
			retirementPending,
		)
	}
	namedRequestAllowed := namedSetupRequestAllowed(
		cfg,
		retirementPending,
		target,
		serviceMode,
		hostFlag,
		haURLFlag,
		relayURLFlag,
		relayTokenFlag,
	)
	explicitLocalSetup := serviceMode ||
		strings.TrimSpace(hostFlag) != "" ||
		strings.TrimSpace(haURLFlag) != "" ||
		strings.TrimSpace(relayURLFlag) != "" ||
		strings.TrimSpace(relayTokenFlag) != ""
	var cloudRecoveryHandled bool
	var cloudRecoveryCode int
	cfg, cloudRecoveryHandled, cloudRecoveryCode =
		handleInteractiveCloudRecoveryBeforeClients(
			reader,
			os.Stdout,
			paths,
			cfg,
			serviceMode,
			explicitLocalSetup,
			lifecycleMarker...,
		)
	if cloudRecoveryHandled {
		return cloudRecoveryCode
	}
	if !namedRequestAllowed {
		renderNamedSetupRequestError()
		return 1
	}
	if err := resumeSetupDeviceCredentialRetirement(paths, cfg); err != nil {
		printHumanErr(
			"cannot finish the interrupted device credential retirement: %s",
			err,
		)
		return 1
	}
	if namedRetirementOnly {
		return 0
	}
	resumedActivation, resumeActivationErr :=
		resumeInteractiveSetupPendingActivation(
			paths,
			&cfg,
			lifecycleMarker...,
		)
	if resumeActivationErr != nil {
		printPendingActivationResumeError(resumeActivationErr)
		return 1
	}
	if resumedActivation {
		printHumanInfo(
			"Resumed the interrupted pairing — this device is connected.",
		)
	}
	choices, err := buildSetupClientChoices(paths, state)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	selectedClients := []string{}
	skippedClients := []string{}
	secureStorageRecovery := setupSecureStorageRecoveryState{}

	savedTokenBeforeSetup := ""
	hadSavedTokenBeforeSetup := false
	tokenStoragePreflightErr := error(nil)
	tokenStorageRecoveryReadBlocked := false
	existingToken := strings.TrimSpace(relayTokenFlag)

	promptedClient := false
	clientsResolved := false
	connectionMode := setupConnectionLocal
	for {
		if target == "" {
			answer, selectErr := promptSetupClientForWizard(
				reader,
				os.Stdout,
				choices,
			)
			if selectErr == errSetupClientPrerequisite {
				return 0
			}
			if selectErr == errSetupExit {
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if selectErr != nil {
				printHumanErr("%s", selectErr)
				return 1
			}
			target = answer
			promptedClient = true
			clientsResolved = false
		}
		if !clientsResolved {
			selectedClients, skippedClients, err = resolveSetupClientsWithChoices(
				choices,
				target,
			)
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			clientsResolved = true
		}

		if remoteOnlyCloudSetup(cfg) && !explicitLocalSetup {
			return resumeInteractiveCloudOnlySetup(
				reader,
				os.Stdout,
				paths,
				cfg,
				&state,
				target,
				selectedClients,
				skippedClients,
				lifecycleMarker...,
			)
		}

		connectionMode = setupConnectionLocal
		if hybridCloudSetupPending(cfg) {
			connectionMode = setupConnectionHybrid
		}
		if !shouldOfferSetupConnectionMode(
			cfg,
			hostFlag,
			haURLFlag,
			relayURLFlag,
			relayTokenFlag,
			serviceMode,
		) {
			break
		}
		connectionMode, err = promptSetupConnectionMode(reader, os.Stdout)
		if err == errSetupBack {
			target = ""
			selectedClients = nil
			skippedClients = nil
			clientsResolved = false
			continue
		}
		if err == errSetupExit {
			renderSetupCancelledNote(os.Stdout)
			return 0
		}
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
		if connectionMode != setupConnectionCloud {
			break
		}
		exit, back := runInteractiveCloudOnlySetupForWizard(
			reader,
			os.Stdout,
			paths,
			cfg,
			&state,
			target,
			selectedClients,
			skippedClients,
			lifecycleMarker...,
		)
		if back {
			continue
		}
		return exit
	}

	restoreTokenFileOverride := func() {}
	defer func() {
		restoreTokenFileOverride()
	}()
	setupChanged := resumedActivation
	formerServiceTokenFile := ""
	formerServiceToken := ""
	if !serviceMode {
		var liftTokenFileSuppression func()
		cfg, formerServiceTokenFile, formerServiceToken, liftTokenFileSuppression = disableServiceRelayTokenFile(paths, cfg)
		defer liftTokenFileSuppression()
	}
	if serviceMode {
		if target == "all" {
			printHumanErr("service credentials require a specific client; use: ha-nova setup --service <client>")
			return 1
		}
		if err := requireSelectedClientServiceCredentials(paths, selectedClients); err != nil {
			printHumanErr("%s", err)
			return 1
		}
		// Only after the service target and client checks passed: a re-setup
		// with a healthy keyring pairing short-circuits to verify and never
		// reaches the pairing stage, so a readable keyring device credential
		// migrates to the private-file backend now (the service contract).
		migrated := false
		migrateErr := withSetupLifecycleLock(paths, lifecycleMarker, func() error {
			var err error
			migrated, err = migrateKeyringDeviceCredentialToFile()
			return err
		})
		if migrateErr != nil {
			printHumanErr("cannot move the device credential into service file storage: %s", migrateErr)
			return 1
		}
		if migrated {
			setupChanged = true
			printHumanInfo("Moved this install's device credential into protected service file storage.")
		}
		// Read any already-stored token BEFORE the file override redirects
		// token reads, so an existing desktop-keyring token can be offered
		// for migration into the service token file.
		if existing, err := readRelayAuthToken(); err == nil {
			formerServiceToken = strings.TrimSpace(existing)
		}
		cfg = enableServiceRelayTokenFile(paths, cfg)
		restoreTokenFileOverride = withRelayAuthTokenFileOverride(cfg.RelayTokenFile)
	}

	tokenStoragePreflightErr = relayAuthTokenSetupPreflightForSetup()
	// Service credentials stay a client-scoped deployment decision: only
	// offer the mid-flow switch for a specific target, mirroring the
	// explicit `--service all` rejection above.
	if !serviceMode && target != "all" {
		serviceCredentials, serviceClientID, hasServiceCredentials, serviceErr := selectedClientsServiceCredentialHint(paths, selectedClients)
		if serviceErr != nil {
			printHumanErr("%s", serviceErr)
			return 1
		}
		if hasServiceCredentials && shouldOfferServiceCredentials(tokenStoragePreflightErr) {
			useServiceCredentials, err := promptSetupServiceCredentialsInteractive(reader, os.Stdout, serviceCredentials, serviceClientID)
			if err == errSetupExit {
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			if useServiceCredentials {
				serviceMode = true
				formerServiceTokenFile = ""
				formerServiceToken = ""
				restoreTokenFileOverride()
				cfg = enableServiceRelayTokenFile(paths, cfg)
				restoreTokenFileOverride = withRelayAuthTokenFileOverride(cfg.RelayTokenFile)
				tokenStoragePreflightErr = relayAuthTokenSetupPreflightForSetup()
			}
		}
	}
	// Set when the missing-keyring error is downgraded below: the legacy /pair
	// token store is unavailable, so the pairing stage must not fall back to it.
	legacyTokenStoreUnavailable := false
	if tokenStoragePreflightErr == nil && !hybridCloudSetupPending(cfg) {
		if savedToken, err := readRelayAuthTokenForSetup(); err == nil && strings.TrimSpace(savedToken) != "" {
			savedTokenBeforeSetup = strings.TrimSpace(savedToken)
			hadSavedTokenBeforeSetup = true
		} else if err != nil && setupSecureStorageRecoveryAvailableNow(err) {
			tokenStoragePreflightErr = err
			tokenStorageRecoveryReadBlocked = true
		} else if err != nil && !isMissingRelayAuthTokenError(err) {
			printRelayTokenStorageSetupWarning(err)
		}
	} else if !setupSecureStorageRecoveryAvailableNow(tokenStoragePreflightErr) {
		// A missing OS keyring is only fatal when a relay TOKEN must actually be
		// stored. The default onboarding path is secure device pairing, which
		// brings its own file-backed credential storage (device_credential_storage.go),
		// so a headless box (container/SSH with no Secret Service) must still reach
		// the pairing stage instead of exiting here on the legacy-token preflight.
		// Only an explicit --relay-token makes the token path mandatory here:
		// service installs default to secure pairing as well (file-backed device
		// credential), so a missing keyring must not abort their onboarding.
		tokenPathRequired := strings.TrimSpace(relayTokenFlag) != ""
		noKeyringBackend := isDesktopKeyringSessionUnavailableError(tokenStoragePreflightErr) ||
			isDesktopKeyringUnavailableError(tokenStoragePreflightErr)
		// The default onboarding path is secure device pairing, which brings its
		// own file-backed credential storage — no keyring needed. So a missing
		// keyring must not abort here; let the wizard reach the pairing stage,
		// where the relay URL is known. The legacy /pair fallback (which DOES need
		// a keyring token store) is guarded there, failing before it consumes a
		// one-time code — see runSetupPairingFlow. Deciding it here is impossible
		// for the plain interactive flow, which discovers the relay URL later.
		deviceStorageViable := false
		if !tokenPathRequired && noKeyringBackend {
			_ = withSetupLifecycleLock(paths, lifecycleMarker, func() error {
				deviceStorageViable = deviceCredentialStorageViable()
				return nil
			})
		}
		if !tokenPathRequired && noKeyringBackend && deviceStorageViable {
			printRelayTokenStorageSetupWarning(tokenStoragePreflightErr)
			printHumanInfo("No OS keyring is reachable here — this device will pair with secure file-backed storage instead of a shared token.")
			tokenStoragePreflightErr = nil
			legacyTokenStoreUnavailable = true
		} else {
			printRelayTokenStorageSetupWarning(tokenStoragePreflightErr)
			printHumanErr("%s", relayAuthTokenSetupSaveError(tokenStoragePreflightErr))
			return 1
		}
	}
	if existingToken == "" {
		existingToken = savedTokenBeforeSetup
	}
	if existingToken == "" && !hadSavedTokenBeforeSetup && formerServiceToken != "" {
		// Returning from service to desktop mode: prefill the token from the
		// former service token file so the user does not have to re-paste it,
		// but deliberately do NOT mark it as a previously stored token —
		// persistence must treat it as new and write it into the OS keyring
		// before the saved config stops referencing the token file.
		existingToken = formerServiceToken
	}

	// Activation recovery above intentionally ran before applying unconfirmed
	// local endpoint overrides.
	overrideApplied := strings.TrimSpace(hostFlag) != "" || strings.TrimSpace(haURLFlag) != "" || strings.TrimSpace(relayURLFlag) != ""
	if overrideApplied {
		var err error
		cfg, err = applySetupFlagOverrides(cfg, hostFlag, haURLFlag, relayURLFlag)
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
	}

	current := detectSetupStateForAssessment(paths, cfg, state, target, savedTokenBeforeSetup, hadSavedTokenBeforeSetup)
	if tokenStoragePreflightErr == nil {
		if handled, code := maybeHandleInteractiveSetupCurrentState(reader, os.Stdout, paths, cfg, current, overrideApplied, serviceMode, lifecycleMarker...); handled {
			needsLifecycleFinalization := setupChanged || overrideApplied ||
				(len(lifecycleMarker) > 1 && len(lifecycleMarker[1]) > 0)
			if code == 0 && current.IsComplete() && needsLifecycleFinalization {
				if err := completeSetupLifecycle(paths, lifecycleMarker...); err != nil {
					printHumanErr("cannot finalize setup lifecycle: %s", err)
					return 1
				}
			}
			if code == 0 && current.IsComplete() {
				askCensusIfEligible(paths, "setup", reader, os.Stdout)
			}
			return code
		}
	}

	pairingFlow := false
	pairingCredentialReceived := false
	devicePaired := false
	manualCredentialFlow := false
	pairingBackStage := setupStageRelayInstall
	cloudSetupIncomplete := false
	cloudSetupPaused := false
	// Service installs pair by default too: secure pairing no longer depends on
	// a desktop keyring (the service path forces the file backend at the pairing
	// stage), and the legacy token flow costs manual HA UI steps. Only an
	// explicit --relay-token or an already-stored token keeps the token path.
	usePairingByDefault := func() bool {
		return connectionMode == setupConnectionHybrid ||
			(strings.TrimSpace(relayTokenFlag) == "" &&
				strings.TrimSpace(existingToken) == "")
	}
	stage := setupStageHost
	if tokenStoragePreflightErr != nil {
		stage = setupStageSecureStorageRecovery
	}
	// A device credential from an earlier pairing makes this a paired install:
	// re-runs (adding a client, finishing an interrupted setup) verify the
	// existing pairing instead of demanding a fresh code every time.
	_, _, _, deviceAlreadyPaired, _ := relayFunctionalTransportForDoctor(cfg)
	verifyFirstReuseFlow := false
	if stage != setupStageSecureStorageRecovery && cfg.HAHost != "" && cfg.HAURL != "" {
		switch {
		case deviceAlreadyPaired:
			devicePaired = true
			stage = setupStageVerify
		case existingToken != "" && current.RelayOK && !current.WSOK:
			stage = setupStageVerify
			verifyFirstReuseFlow = true
		case existingToken != "":
			if relayTokenFlag != "" {
				stage = setupStageToken
			} else {
				stage = setupStageVerify
				verifyFirstReuseFlow = true
			}
		default:
			if usePairingByDefault() {
				pairingFlow = true
				stage = setupStagePairing
			} else {
				stage = setupStageToken
			}
		}
	}

	token := existingToken
	tokenChanged := false
	hostChangeRetry := false

	for {
		renderSetupHeader(os.Stdout)

		switch stage {
		case setupStageClient:
			answer, err := promptSetupClientInteractive(reader, os.Stdout, choices, target)
			if err == errSetupBack {
				continue
			}
			if err == errSetupExit {
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			target = answer
			selectedClients, skippedClients, err = resolveSetupClientsWithChoices(choices, target)
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			if tokenStoragePreflightErr != nil {
				stage = setupStageSecureStorageRecovery
			} else {
				stage = setupStageHost
			}

		case setupStageSecureStorageRecovery:
			result, err := runSetupSecureStorageRecoveryFlow(reader, os.Stdout, tokenStoragePreflightErr, &secureStorageRecovery, setupSecureStorageRecoveryInitialAttempt)
			if err == errSetupBack {
				if promptedClient {
					stage = setupStageClient
					continue
				}
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if err == errSetupExit {
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			if result != setupSecureStorageRecoveryRecovered {
				if tokenStorageRecoveryReadBlocked {
					printHumanErr("%s", relayAuthTokenSetupReadError(tokenStoragePreflightErr))
				} else {
					printHumanErr("%s", relayAuthTokenSetupSaveError(tokenStoragePreflightErr))
				}
				return 1
			}

			tokenStoragePreflightErr = relayAuthTokenSetupPreflightForSetup()
			if tokenStoragePreflightErr != nil {
				printRelayTokenStorageSetupWarning(tokenStoragePreflightErr)
				printHumanErr("%s", relayAuthTokenSetupSaveError(tokenStoragePreflightErr))
				return 1
			}
			tokenStorageRecoveryReadBlocked = false
			savedTokenBeforeSetup = ""
			hadSavedTokenBeforeSetup = false
			if savedToken, err := readRelayAuthTokenForSetup(); err == nil && strings.TrimSpace(savedToken) != "" {
				savedTokenBeforeSetup = strings.TrimSpace(savedToken)
				hadSavedTokenBeforeSetup = true
			} else if err != nil && !isMissingRelayAuthTokenError(err) {
				printHumanErr("%s", relayAuthTokenSetupReadError(err))
				return 1
			}
			if strings.TrimSpace(relayTokenFlag) == "" {
				existingToken = savedTokenBeforeSetup
			}
			current = detectSetupStateForAssessment(paths, cfg, state, target, savedTokenBeforeSetup, hadSavedTokenBeforeSetup)
			if current.IsComplete() {
				if overrideApplied {
					if err := saveSetupConfigWithLifecycle(paths, cfg, lifecycleMarker...); err != nil {
						printHumanErr("cannot save config: %s", err)
						return 1
					}
				}
				renderSetupAlreadyDoneBanner(os.Stdout, cfg.RelaySecureBaseURL == "" && cfg.RelayBaseURL != "")
				return 0
			}
			stage = setupStageHost
			verifyFirstReuseFlow = false
			// Secure storage just became readable: the pre-recovery transport
			// resolution is stale. A paired device found NOW routes to verify —
			// demanding a fresh code here would undo the point of recovery.
			_, _, _, deviceAlreadyPaired, _ = relayFunctionalTransportForDoctor(cfg)
			if cfg.HAHost != "" && cfg.HAURL != "" {
				switch {
				case deviceAlreadyPaired:
					devicePaired = true
					stage = setupStageVerify
				case existingToken != "" && current.RelayOK && !current.WSOK:
					stage = setupStageVerify
					verifyFirstReuseFlow = true
				case existingToken != "":
					if relayTokenFlag != "" {
						stage = setupStageToken
					} else {
						stage = setupStageVerify
						verifyFirstReuseFlow = true
					}
				default:
					if usePairingByDefault() {
						pairingFlow = true
						stage = setupStagePairing
					} else {
						stage = setupStageToken
					}
				}
			}
			continue

		case setupStageHost:
			var host string
			var haURL string
			var err error
			if hostChangeRetry {
				// The user just rejected the saved address — don't spend the
				// discovery window re-confirming it or offer it back as the
				// press-Enter default.
				renderSetupParagraph(os.Stdout, fmt.Sprintf("The saved address %s could not be used. Enter a new one.", cfg.HAURL))
				host, haURL, err = promptValidHAHostFromReader(reader, os.Stdout, "")
			} else {
				candidate, selected, discoveryErr := selectDefaultHAHostWithFeedback(reader, os.Stdout, cfg)
				switch {
				case discoveryErr != nil:
					err = discoveryErr
				case selected:
					host = candidate.Host
					haURL = candidate.HAURL
				default:
					host, haURL, err = promptValidHAHostFromReader(reader, os.Stdout, candidate.Host)
				}
			}
			if err == errSetupBack {
				if hostChangeRetry {
					hostChangeRetry = false
					stage = setupStageVerify
				} else if promptedClient {
					stage = setupStageClient
				}
				continue
			}
			if err == errSetupExit {
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			previousRelayURL := strings.TrimSpace(cfg.RelayBaseURL)
			previousDerivedRelayURL := deriveRelayURLFromHA(cfg.HAURL, cfg.HAHost)
			cfg = applySelectedSetupHost(cfg, host, haURL, relayURLFlag)
			if strings.TrimSpace(relayURLFlag) == "" && previousRelayURL != "" &&
				previousRelayURL != previousDerivedRelayURL && cfg.RelayBaseURL != previousRelayURL {
				// A saved relay address that was NOT derived from the old host
				// is a deliberate custom setting (proxy, other machine); a
				// host change must not silently clobber it.
				cfg.RelayBaseURL = previousRelayURL
				renderSetupParagraphTight(os.Stdout, "Keeping your saved relay address: "+previousRelayURL)
			}
			switch {
			case hostChangeRetry && verifyFirstReuseFlow && hadSavedTokenBeforeSetup && strings.TrimSpace(token) != "":
				// Resume with saved state: the relay app and tokens are known
				// to be in place; only the address was wrong, so return
				// straight to verification. Fresh-flow host changes — including
				// pasted-token fresh runs — fall through instead: the new
				// address may be a different instance that never got the
				// repository/app/token steps. Save the corrected address
				// now so it survives an exit before verification succeeds.
				hostChangeRetry = false
				if err := saveSetupConfigWithLifecycle(paths, cfg, lifecycleMarker...); err != nil {
					printHumanErr("cannot save config: %s", err)
					return 1
				}
				stage = setupStageVerify
			case strings.TrimSpace(relayTokenFlag) != "" && !hostChangeRetry:
				stage = setupStageToken
			default:
				if hostChangeRetry {
					// A fresh-flow host change abandons the flag-driven or
					// pasted-token shortcut: the corrected address may be a
					// different instance that still needs the repository/app
					// install and client credential setup.
					verifyFirstReuseFlow = false
				}
				hostChangeRetry = false
				stage = setupStageRelayInstall
			}

		case setupStageRelayInstall:
			pairAfterInstall := usePairingByDefault()
			steps := buildSetupWizardSteps()
			if pairAfterInstall {
				steps = buildSetupPairingWizardSteps()
			}
			renderSetupStep(os.Stdout, steps.RelayInstall, steps.Total, "Install NOVA Relay in Home Assistant")
			repositoryURL := haAddRepositoryURL(cfg.HAURL)
			renderSetupParagraph(os.Stdout,
				"Next, add the HA NOVA app repository to your Home Assistant.",
			)
			renderSetupLink(os.Stdout, "This will open:", repositoryURL)
			_, err := promptWizardLineFromReader(reader, os.Stdout, "Press Enter to open your browser", "")
			if err == errSetupBack {
				stage = setupStageHost
				continue
			}
			if err == errSetupExit {
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			openAnnouncedBrowserURL(os.Stdout, repositoryURL)
			renderSetupParagraphTight(os.Stdout, `In the browser: log in to Home Assistant if asked, then click "Add" to confirm the repository.`)
			renderSetupIndentedBlock(os.Stdout, "Once the repository is added:", "    ",
				"1. Go to Settings > Apps > App Store (on older Home Assistant: Settings > Add-ons)",
				`2. Search for "NOVA Relay"`,
				"3. Click Install and wait for it to finish",
				"4. Click Start",
			)
			_, err = promptWizardLineFromReader(reader, os.Stdout, "Press Enter when the app is running", "")
			if err == errSetupBack {
				stage = setupStageHost
				continue
			}
			if err == errSetupExit {
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			if pairAfterInstall {
				if deviceAlreadyPaired {
					// An earlier pairing already gave this device its own
					// credential: verify it against the (re)installed relay
					// first — a failed verify still routes back to pairing.
					devicePaired = true
					stage = setupStageVerify
					continue
				}
				pairingFlow = true
				pairingBackStage = setupStageRelayInstall
				stage = setupStagePairing
			} else {
				stage = setupStageToken
			}

		case setupStageToken:
			// Any route into the token stage is the legacy path by definition:
			// verify must judge the token, never a leftover device pairing.
			devicePaired = false
			current = detectSetupStateWithToken(paths, cfg, state, target, savedTokenBeforeSetup, hadSavedTokenBeforeSetup)
			existingToken = ""
			if relayTokenFlag == "" && hadSavedTokenBeforeSetup {
				existingToken = savedTokenBeforeSetup
			}
			if relayTokenFlag != "" {
				token = strings.TrimSpace(relayTokenFlag)
			}
			resumeWSRecovery := relayTokenFlag == "" && strings.TrimSpace(existingToken) != "" && current.RelayOK && !current.WSOK
			verifyFirstReuseFlow = false
			steps := buildSetupWizardSteps()
			credentialStep := steps.RelayToken
			if pairingFlow {
				steps = buildSetupPairingWizardSteps()
				credentialStep = steps.Pairing
			}
			if resumeWSRecovery {
				steps = buildSetupWizardSteps()
				credentialStep = steps.RelayToken
			}

			renderSetupStep(os.Stdout, credentialStep, steps.Total, "Set up Relay Auth Token")
			renderSetupIndentedBlock(os.Stdout, "NOVA keeps client and Home Assistant access separate:", "    ",
				"a) This Relay token protects the connection from this computer",
				"b) The Home Assistant App receives its upstream access automatically",
			)
			renderSetupParagraph(os.Stdout, "Standalone Container/Core relays keep HA_LLAT in the server environment; the CLI never asks for it.")
			if relayTokenFlag != "" {
				renderSetupParagraphTight(os.Stdout, "Using the Relay Auth Token you already provided.")
			} else if resumeWSRecovery {
				verifyFirstReuseFlow = true
				token = existingToken
				renderSetupParagraphTight(os.Stdout, fmt.Sprintf("Using saved relay token: %s", maskSecretHint(existingToken)))
			} else {
				if existingToken != "" {
					renderSetupParagraphTight(os.Stdout, fmt.Sprintf("Existing relay token found: %s", maskSecretHint(existingToken)))
				}

				choice, err := promptSetupTokenChoiceInteractive(reader, os.Stdout, existingToken != "")
				if err == errSetupBack {
					if pairingFlow {
						stage = setupStagePairing
					} else {
						stage = setupStageRelayInstall
					}
					continue
				}
				if err == errSetupExit {
					renderSetupCancelledNote(os.Stdout)
					return 0
				}
				if err != nil {
					printHumanErr("%s", err)
					return 1
				}

				tokenChanged = false
				switch choice {
				case "keep":
					token = existingToken
					verifyFirstReuseFlow = true
				case "paste":
					fmt.Fprintln(os.Stdout)
					pasted, err := promptWizardLineFromReader(reader, os.Stdout, "Paste Relay Auth Token", "")
					if err == errSetupBack {
						continue
					}
					if err == errSetupExit {
						renderSetupCancelledNote(os.Stdout)
						return 0
					}
					if err != nil {
						printHumanErr("%s", err)
						return 1
					}
					token = strings.TrimSpace(pasted)
					if token == "" {
						renderSetupErrorLine(os.Stdout, "No token entered.")
						continue
					}
					verifyFirstReuseFlow = true
				case "generate":
					generated, err := generateRelayToken()
					if err != nil {
						printHumanErr("cannot generate relay token: %s", err)
						return 1
					}
					token = generated
					tokenChanged = true
					verifyFirstReuseFlow = false
					renderSetupIndentedBlock(os.Stdout, "Here is your relay token (generated automatically):", "    ", token)
				}

				if tokenChanged {
					if err := copyToClipboardForSetup(token); err == nil {
						renderSetupSuccessLine(os.Stdout, "Copied to clipboard.")
					}
					renderSetupIndentedBlock(os.Stdout, "Next, on the NOVA Relay page:", "    ",
						`1. Open the "Configuration" tab`,
						`2. Paste the token into the "Relay Auth Token" field ("relay_auth_token")`,
						"3. Click Save",
						"4. Restart the App so it picks up the new token",
					)
					renderSetupLink(os.Stdout, "This will open:", haRelayAppPageURL(cfg.HAURL))
					_, err := promptWizardLineFromReader(reader, os.Stdout, "Press Enter to open your browser", "")
					if err == errSetupBack {
						continue
					}
					if err == errSetupExit {
						renderSetupCancelledNote(os.Stdout)
						return 0
					}
					if err != nil {
						printHumanErr("%s", err)
						return 1
					}
					openAnnouncedBrowserURL(os.Stdout, haRelayAppPageURL(cfg.HAURL))
					_, err = promptWizardLineFromReader(reader, os.Stdout, "Press Enter after you saved the Relay Auth Token and restarted NOVA Relay", "")
					if err == errSetupBack {
						continue
					}
					if err == errSetupExit {
						renderSetupCancelledNote(os.Stdout)
						return 0
					}
					if err != nil {
						printHumanErr("%s", err)
						return 1
					}
				}
			}

			if verifyFirstReuseFlow {
				renderSetupParagraph(os.Stdout, "If this token already works on another device, the next verification step should succeed without any new Home Assistant changes.")
			}

			stage = setupStageVerify

		case setupStagePairing:
			steps := buildSetupPairingWizardSteps()
			renderSetupStep(os.Stdout, steps.Pairing, steps.Total, "Pair this device")
			if serviceMode {
				// `setup --service` documents that the device credential lands in a
				// protected file — service installs must not depend on a desktop
				// keyring nobody unlocks. Forced here at the pairing stage, not at
				// setup entry, so earlier reads of an existing keyring credential
				// stay untouched.
				forceDeviceCredentialFileMode()
			}
			pairedToken, err := runSetupPairingFlow(reader, os.Stdout, paths, &cfg, legacyTokenStoreUnavailable, lifecycleMarker...)
			if err == errSetupBack {
				stage = pairingBackStage
				continue
			}
			if err == errSetupRelayTokenStep {
				// The user explicitly left the device path: verify must judge
				// the token they are about to provide, not the old pairing.
				devicePaired = false
				manualCredentialFlow = true
				stage = setupStageToken
				continue
			}
			if err == errSetupExit {
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if err == errSetupDevicePaired {
				// Secure v1 pairing already stored the device credential and the
				// secure endpoint in the config; there is no relay token to keep,
				// and activation already proved the connection.
				devicePaired = true
				token = ""
				pairingCredentialReceived = false
				manualCredentialFlow = false
				verifyFirstReuseFlow = false
				stage = setupStageVerify
				continue
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			token = pairedToken
			pairingCredentialReceived = true
			devicePaired = false
			manualCredentialFlow = false
			verifyFirstReuseFlow = false
			stage = setupStageVerify

		case setupStageVerify:
			if cfg.RelayBaseURL == "" {
				cfg.RelayBaseURL = deriveRelayURLFromHA(cfg.HAURL, cfg.HAHost)
			}

			steps := buildSetupWizardSteps()
			if pairingFlow {
				steps = buildSetupPairingWizardSteps()
			}
			renderSetupStep(os.Stdout, steps.Verify, steps.Total, "Verifying connection")

			if devicePaired {
				renderDeviceVerifyIntro(os.Stdout)
				if !verifyDeviceHealth(cfg) {
					renderSetupErrorLine(os.Stdout, "Paired, but the secure device endpoint did not answer yet. The App may still be starting.")
					if _, retryErr := promptWizardLineFromReader(reader, os.Stdout, "Press Enter to retry, or type 'back' to pair again", ""); retryErr != nil {
						if retryErr == errSetupBack {
							// Do exactly what the prompt advertises.
							pairingBackStage = setupStageVerify
							stage = setupStagePairing
							continue
						}
						if retryErr == errSetupExit {
							renderSetupCancelledNote(os.Stdout)
							return 0
						}
						printHumanErr("%s", retryErr)
						return 1
					}
					if !verifyDeviceHealth(cfg) {
						pairingBackStage = setupStageVerify
						stage = setupStagePairing
						continue
					}
				}
				renderSetupSuccessLine(os.Stdout, "Secure connection verified")
				if err := persistDeviceSetupState(paths, cfg, &state, lifecycleMarker...); err != nil {
					printHumanErr("%s", err)
					return 1
				}
				if connectionMode == setupConnectionHybrid && !cfg.Cloud.ready() {
					renderSetupParagraph(
						os.Stdout,
						"Local access is ready. Now connecting Home Assistant Cloud for away-from-home access.",
					)
					cfg, err = addHybridCloudAfterLocal(
						paths,
						cfg,
						lifecycleMarker...,
					)
					if err != nil {
						if handlePausedCloudOwnerPairing(
							os.Stdout,
							paths,
							err,
						) {
							cloudSetupPaused = true
							stage = setupStageSkills
							continue
						}
						renderCloudFailure(os.Stdout, paths, err)
						cloudSetupIncomplete = true
						stage = setupStageSkills
						continue
					}
					renderSetupSuccessLine(os.Stdout, "Home Assistant Cloud access verified")
				}
				stage = setupStageSkills
				continue
			}

			if verifyFirstReuseFlow {
				renderSetupParagraphTight(os.Stdout, "Using Home Assistant address: "+cfg.HAURL)
			}
			credentialRepair := setupCredentialRepairNone
			if strings.TrimSpace(relayTokenFlag) == "" {
				credentialRepair = setupCredentialRepairToken
				if !serviceMode && !manualCredentialFlow {
					credentialRepair = setupCredentialRepairPairing
				}
			}
			issue, ok, err := verifySetupConnection(reader, os.Stdout, cfg, token, verifyFirstReuseFlow, credentialRepair, pairingCredentialReceived)
			if err == errSetupBack {
				if pairingFlow {
					pairingBackStage = setupStageVerify
					stage = setupStagePairing
				} else if relayTokenFlag != "" {
					stage = setupStageHost
				} else {
					stage = setupStageToken
				}
				continue
			}
			if err == errSetupRelayTokenStep {
				pairingFlow = false
				stage = setupStageToken
				continue
			}
			if err == errSetupPairingStep {
				pairingFlow = true
				pairingBackStage = setupStageVerify
				stage = setupStagePairing
				continue
			}
			if err == errSetupHostStep {
				hostChangeRetry = true
				stage = setupStageHost
				continue
			}
			if err == errSetupInstallStep {
				// The user asked for full guidance from the repair menu:
				// repository/app install and client credential setup
				// for the current address.
				verifyFirstReuseFlow = false
				stage = setupStageRelayInstall
				continue
			}
			if err == errSetupExit {
				renderSetupCancelledNote(os.Stdout)
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			if !ok {
				if err := persistInteractiveSetupStateWithRecovery(reader, os.Stdout, paths, cfg, &state, savedTokenBeforeSetup, hadSavedTokenBeforeSetup, token, &secureStorageRecovery, lifecycleMarker...); err != nil {
					printHumanErr("%s", err)
					return 1
				}
				renderSetupIncompleteBanner(os.Stdout, issue)
				return 1
			}
			// The token path just verified successfully. A leftover device
			// pairing (this branch is legacy-only; the device branch continues
			// to Skills above) would win transport resolution on the next run
			// and wedge the install on a dead pairing — retire it for good.
			if err := persistInteractiveSetupStateWithRecoveryMode(reader, os.Stdout, paths, cfg, &state, savedTokenBeforeSetup, hadSavedTokenBeforeSetup, token, &secureStorageRecovery, true, lifecycleMarker...); err != nil {
				printHumanErr("%s", err)
				return 1
			}
			stage = setupStageSkills

		case setupStageSkills:
			steps := buildSetupWizardSteps()
			if pairingFlow {
				steps = buildSetupPairingWizardSteps()
			}
			renderSetupStep(os.Stdout, steps.Skills, steps.Total, "Installing HA NOVA skills")
			if target == "all" && len(selectedClients) > 0 {
				fmt.Fprintf(os.Stdout, "  Will install: %s\n", strings.Join(selectedClients, ", "))
				if len(skippedClients) > 0 {
					fmt.Fprintf(os.Stdout, "  Skipping until installed: %s\n\n", strings.Join(skippedClients, ", "))
				}
			}
			if err := runSetupStepWithFeedback(os.Stdout, fmt.Sprintf("Setting up HA NOVA for %s...", setupClientLabel(target)), func() error {
				return withClientMutationLock(paths, func() error {
					if err := installClientsAndSaveStateUnlocked(paths, &state, selectedClients, saveState, lifecycleMarker...); err != nil {
						return err
					}
					finalizeServiceTokenFileMigration(formerServiceTokenFile, token)
					return completeSetupLifecycleUnlocked(paths, lifecycleMarker...)
				})
			}); err != nil {
				printHumanErr("client installation failed: %s", err)
				renderSetupIncompleteBanner(os.Stdout, setupIssueSkillsInstall)
				return 1
			}
			if exit := renderSetupCompletionOutcomeWithCloudPause(
				os.Stdout,
				selectedClients,
				cloudSetupIncomplete,
				cloudSetupPaused,
			); exit != 0 {
				return exit
			}
			// One-time census ask AFTER the complete banner — clearly outside
			// the numbered wizard steps, never readable as a setup hurdle.
			// A queued Enter from the wizard's last step must not silently
			// answer "No": arm the stale-blank-input skip for the next prompt
			// and clear it afterwards (the ask may not prompt at all).
			armSetupNextPromptSkipsStaleBlankInput()
			askCensusIfEligible(paths, "setup", reader, os.Stdout)
			clearSetupNextPromptSkipsStaleBlankInput()
			return 0
		}
	}
}

func renderSetupCompletionOutcomeWithCloudPause(
	out io.Writer,
	selectedClients []string,
	cloudSetupIncomplete bool,
	cloudSetupPaused bool,
) int {
	if cloudSetupPaused {
		renderSetupCloudPausedOutcome(out)
		return 0
	}
	if cloudSetupIncomplete {
		renderSetupIncompleteBanner(out, setupIssueCloudAccess)
		return 1
	}
	renderSetupCompleteBanner(out, selectedClients)
	return 0
}

func persistInteractiveSetupStateWithRecovery(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, state *installState, previousToken string, hadPreviousToken bool, token string, recovery *setupSecureStorageRecoveryState, lifecycleMarker ...[]byte) error {
	return persistInteractiveSetupStateWithRecoveryMode(reader, out, paths, cfg, state, previousToken, hadPreviousToken, token, recovery, false, lifecycleMarker...)
}

func persistInteractiveSetupStateWithRecoveryMode(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, state *installState, previousToken string, hadPreviousToken bool, token string, recovery *setupSecureStorageRecoveryState, retireDevice bool, lifecycleMarker ...[]byte) error {
	err := persistInteractiveSetupStateWithMode(paths, cfg, state, previousToken, hadPreviousToken, token, retireDevice, lifecycleMarker...)
	if err == nil {
		return nil
	}
	if !isDesktopKeyringSetupRequiredError(err) || recovery == nil || recovery.saveRetryAttempted || !setupSecureStorageRecoveryAvailableNow(err) {
		return err
	}

	result, recoveryErr := runSetupSecureStorageRecoveryFlow(reader, out, err, recovery, setupSecureStorageRecoverySaveRetryAttempt)
	if recoveryErr != nil {
		if recoveryErr == errSetupExit || recoveryErr == errSetupBack {
			return relayAuthTokenSetupSaveError(err)
		}
		return recoveryErr
	}
	if result != setupSecureStorageRecoveryRecovered {
		return relayAuthTokenSetupSaveError(err)
	}

	return persistInteractiveSetupStateWithMode(paths, cfg, state, previousToken, hadPreviousToken, token, retireDevice, lifecycleMarker...)
}
