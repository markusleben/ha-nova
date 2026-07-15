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
var detectDefaultHAHostForSetup = detectDefaultHAHost
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

func maybeHandleInteractiveSetupCurrentState(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, current setupState, overrideApplied bool) (bool, int) {
	if !(current.ConfigOK || current.TokenOK || current.RelayOK || current.WSOK || current.SkillsOK) {
		return false, 0
	}

	renderSetupHeader(out)
	renderSetupStatusSummary(out, current)
	if current.IsComplete() {
		if overrideApplied {
			if err := saveConfig(paths, cfg); err != nil {
				printHumanErr("cannot save config: %s", err)
				return true, 1
			}
		}
		renderSetupAlreadyDoneBanner(out)
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

func interactiveSetup(paths runtimePaths, cfg runtimeConfig, state installState, target string, hostFlag, haURLFlag, relayURLFlag, relayTokenFlag string, serviceMode bool) int {
	const (
		setupStageClient = iota
		setupStageSecureStorageRecovery
		setupStageHost
		setupStageRelayInstall
		setupStageToken
		setupStageLLAT
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
	if target == "" {
		for {
			answer, err := promptSetupClientInteractive(reader, os.Stdout, choices, "claude")
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
			break
		}
		promptedClient = true
	} else {
		var err error
		selectedClients, skippedClients, err = resolveSetupClientsWithChoices(choices, target)
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
	}

	restoreTokenFileOverride := func() {}
	defer func() {
		restoreTokenFileOverride()
	}()
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
	if tokenStoragePreflightErr == nil {
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
		printRelayTokenStorageSetupWarning(tokenStoragePreflightErr)
		printHumanErr("%s", relayAuthTokenSetupSaveError(tokenStoragePreflightErr))
		return 1
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

	overrideApplied := strings.TrimSpace(hostFlag) != "" || strings.TrimSpace(haURLFlag) != "" || strings.TrimSpace(relayURLFlag) != ""
	skipLLATWalkthrough := strings.TrimSpace(hostFlag) != "" && strings.TrimSpace(relayTokenFlag) != ""
	if overrideApplied {
		var err error
		cfg, err = applySetupFlagOverrides(cfg, hostFlag, haURLFlag, relayURLFlag)
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
	}

	current := detectSetupStateWithToken(paths, cfg, state, target, savedTokenBeforeSetup, hadSavedTokenBeforeSetup)
	if tokenStoragePreflightErr == nil {
		if handled, code := maybeHandleInteractiveSetupCurrentState(reader, os.Stdout, paths, cfg, current, overrideApplied); handled {
			return code
		}
	}

	pairingFlow := false
	pairingCredentialReceived := false
	pairingBackStage := setupStageLLAT
	usePairingByDefault := func() bool {
		return !serviceMode && strings.TrimSpace(relayTokenFlag) == "" && strings.TrimSpace(existingToken) == ""
	}
	stage := setupStageHost
	if tokenStoragePreflightErr != nil {
		stage = setupStageSecureStorageRecovery
	}
	verifyFirstReuseFlow := false
	if stage != setupStageSecureStorageRecovery && cfg.HAHost != "" && cfg.HAURL != "" {
		switch {
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
				stage = setupStageLLAT
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
			current = detectSetupStateWithToken(paths, cfg, state, target, savedTokenBeforeSetup, hadSavedTokenBeforeSetup)
			if current.IsComplete() {
				if overrideApplied {
					if err := saveConfig(paths, cfg); err != nil {
						printHumanErr("cannot save config: %s", err)
						return 1
					}
				}
				renderSetupAlreadyDoneBanner(os.Stdout)
				return 0
			}
			stage = setupStageHost
			verifyFirstReuseFlow = false
			if cfg.HAHost != "" && cfg.HAURL != "" {
				switch {
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
						stage = setupStageLLAT
					} else {
						stage = setupStageToken
					}
				}
			}
			continue

		case setupStageHost:
			defaultHost := ""
			if hostChangeRetry {
				// The user just rejected the saved address — don't spend the
				// discovery window re-confirming it or offer it back as the
				// press-Enter default.
				renderSetupParagraph(os.Stdout, fmt.Sprintf("The saved address %s could not be used. Enter a new one.", cfg.HAURL))
			} else {
				defaultHost, _ = detectDefaultHAHostWithFeedback(os.Stdout, cfg)
			}
			host, haURL, err := promptValidHAHostFromReader(reader, os.Stdout, defaultHost)
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
				if err := saveConfig(paths, cfg); err != nil {
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
					// install and the access-token walkthrough.
					skipLLATWalkthrough = false
					verifyFirstReuseFlow = false
				}
				hostChangeRetry = false
				stage = setupStageRelayInstall
			}

		case setupStageRelayInstall:
			steps := buildSetupWizardSteps(true)
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
				"   (don't start the app yet — setup continues here first)",
			)
			_, err = promptWizardLineFromReader(reader, os.Stdout, "Press Enter when the installation is complete", "")
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
			if usePairingByDefault() {
				pairingFlow = true
				stage = setupStageLLAT
			} else {
				stage = setupStageToken
			}

		case setupStageToken:
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
			steps := buildSetupWizardSteps(!skipLLATWalkthrough)
			credentialStep := steps.RelayToken
			if pairingFlow {
				steps = buildSetupPairingWizardSteps()
				credentialStep = steps.Pairing
			}
			if resumeWSRecovery {
				steps = buildSetupWizardSteps(false)
				credentialStep = steps.RelayToken
			}

			renderSetupStep(os.Stdout, credentialStep, steps.Total, "Set up Relay Auth Token")
			renderSetupIndentedBlock(os.Stdout, `NOVA needs two passwords ("tokens") to work securely:`, "    ",
				"a) Relay token — keeps the connection between this computer and Home Assistant private",
				"b) HA access token — allows the relay to control your devices and automations",
			)
			if skipLLATWalkthrough {
				renderSetupParagraph(os.Stdout, "This advanced path uses an explicit Relay Auth Token. The Home Assistant Access Token must already be configured in the App.")
			} else {
				renderSetupParagraph(os.Stdout, "This step is only for the Relay Auth Token. The Home Assistant Access Token comes next as its own step.")
			}
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
					_, err = promptWizardLineFromReader(reader, os.Stdout, "Press Enter after you saved the Relay Auth Token in NOVA Relay", "")
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

			if !skipLLATWalkthrough && !verifyFirstReuseFlow {
				stage = setupStageLLAT
				continue
			}
			stage = setupStageVerify

		case setupStageLLAT:
			steps := buildSetupWizardSteps(true)
			if pairingFlow {
				steps = buildSetupPairingWizardSteps()
			}
			if err := runSetupLLATWalkthrough(reader, os.Stdout, cfg, token, steps); err != nil {
				if err == errSetupBack {
					if pairingFlow {
						stage = setupStageRelayInstall
					} else if relayTokenFlag != "" {
						stage = setupStageHost
					} else {
						stage = setupStageToken
					}
					continue
				}
				if err == errSetupExit {
					renderSetupCancelledNote(os.Stdout)
					return 0
				}
				printHumanErr("%s", err)
				return 1
			}
			if pairingFlow {
				skipLLATWalkthrough = true
				pairingBackStage = setupStageLLAT
				stage = setupStagePairing
			} else {
				stage = setupStageVerify
			}

		case setupStagePairing:
			steps := buildSetupPairingWizardSteps()
			renderSetupStep(os.Stdout, steps.Pairing, steps.Total, "Pair this device")
			pairedToken, err := runSetupPairingFlow(reader, os.Stdout, cfg)
			if err == errSetupBack {
				stage = pairingBackStage
				continue
			}
			if err == errSetupRelayTokenStep {
				skipLLATWalkthrough = true
				stage = setupStageToken
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
			token = pairedToken
			pairingCredentialReceived = true
			verifyFirstReuseFlow = false
			stage = setupStageVerify

		case setupStageVerify:
			if cfg.RelayBaseURL == "" {
				cfg.RelayBaseURL = deriveRelayURLFromHA(cfg.HAURL, cfg.HAHost)
			}

			steps := buildSetupWizardSteps(!skipLLATWalkthrough && !verifyFirstReuseFlow)
			if pairingFlow {
				steps = buildSetupPairingWizardSteps()
			}
			renderSetupStep(os.Stdout, steps.Verify, steps.Total, "Verifying connection")
			if verifyFirstReuseFlow {
				renderSetupParagraphTight(os.Stdout, "Using Home Assistant address: "+cfg.HAURL)
			}
			credentialRepair := setupCredentialRepairNone
			if strings.TrimSpace(relayTokenFlag) == "" {
				credentialRepair = setupCredentialRepairToken
				if !serviceMode {
					credentialRepair = setupCredentialRepairPairing
				}
			}
			issue, ok, err := verifySetupConnection(reader, os.Stdout, cfg, token, verifyFirstReuseFlow, credentialRepair, pairingCredentialReceived)
			if err == errSetupBack {
				if pairingFlow {
					pairingBackStage = setupStageVerify
					stage = setupStagePairing
				} else if !skipLLATWalkthrough && !verifyFirstReuseFlow {
					stage = setupStageLLAT
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
				// repository/app install, token, and access-token walkthrough
				// for the current address.
				skipLLATWalkthrough = false
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
				if err := persistInteractiveSetupStateWithRecovery(reader, os.Stdout, paths, cfg, &state, savedTokenBeforeSetup, hadSavedTokenBeforeSetup, token, &secureStorageRecovery); err != nil {
					printHumanErr("%s", err)
					return 1
				}
				renderSetupIncompleteBanner(os.Stdout, issue)
				return 1
			}
			if err := persistInteractiveSetupStateWithRecovery(reader, os.Stdout, paths, cfg, &state, savedTokenBeforeSetup, hadSavedTokenBeforeSetup, token, &secureStorageRecovery); err != nil {
				printHumanErr("%s", err)
				return 1
			}
			stage = setupStageSkills

		case setupStageSkills:
			steps := buildSetupWizardSteps(!skipLLATWalkthrough && !verifyFirstReuseFlow)
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
				return installClients(paths, &state, selectedClients)
			}); err != nil {
				printHumanErr("client installation failed: %s", err)
				renderSetupIncompleteBanner(os.Stdout, setupIssueSkillsInstall)
				return 1
			}
			// Mark this version verified only if every tracked client was just
			// synced (see allTrackedClientsSynced); a subset sync must leave the
			// marker so the self-heal still repairs the untouched clients.
			if allTrackedClientsSynced(state.InstalledClients, selectedClients) {
				state.ClientsVerifiedVersion = localVersion(paths)
			}
			if err := saveState(paths, state); err != nil {
				printHumanErr("cannot save state: %s", err)
				return 1
			}
			finalizeServiceTokenFileMigration(formerServiceTokenFile, token)
			renderSetupCompleteBanner(os.Stdout, selectedClients)
			return 0
		}
	}
}

func persistInteractiveSetupStateWithRecovery(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, state *installState, previousToken string, hadPreviousToken bool, token string, recovery *setupSecureStorageRecoveryState) error {
	err := persistInteractiveSetupState(paths, cfg, state, previousToken, hadPreviousToken, token)
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

	return persistInteractiveSetupState(paths, cfg, state, previousToken, hadPreviousToken, token)
}
