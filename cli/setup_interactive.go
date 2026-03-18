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

func interactiveSetup(paths runtimePaths, cfg runtimeConfig, state installState, target string, hostFlag, haURLFlag, relayURLFlag, relayTokenFlag string) int {
	const (
		setupStageClient = iota
		setupStageHost
		setupStageRelayInstall
		setupStageToken
		setupStageLLAT
		setupStageVerify
		setupStageSkills
	)

	reader := bufio.NewReader(os.Stdin)
	renderSetupHeader(os.Stdout)
	choices, err := buildSetupClientChoices(paths, state)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	selectedClients := []string{}
	skippedClients := []string{}

	savedTokenBeforeSetup := ""
	hadSavedTokenBeforeSetup := false
	if savedToken, err := readRelayAuthToken(); err == nil && strings.TrimSpace(savedToken) != "" {
		savedTokenBeforeSetup = strings.TrimSpace(savedToken)
		hadSavedTokenBeforeSetup = true
	} else if err != nil && !isMissingRelayAuthTokenError(err) {
		printHumanWarn("secure storage unavailable: %s", err)
	}

	existingToken := strings.TrimSpace(relayTokenFlag)
	if existingToken == "" {
		existingToken = savedTokenBeforeSetup
	}

	promptedClient := false
	if target == "" {
		for {
			answer, err := promptSetupClientInteractive(reader, os.Stdout, choices, "claude")
			if err == errSetupBack {
				continue
			}
			if err == errSetupExit {
				printHumanInfo("Setup cancelled")
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

	current := detectSetupState(paths, cfg, state, target)
	if current.ConfigOK || current.TokenOK || current.RelayOK || current.WSOK || current.SkillsOK {
		renderSetupHeader(os.Stdout)
		renderSetupStatusSummary(os.Stdout, current)
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
		if summary := current.SkipSummary(); summary != "" {
			fmt.Fprintf(os.Stdout, "  Already done: %s\n\n", summary)
		}
		if writerSupportsTTYForSetup(os.Stdout) {
			_, err := promptWizardLineFromReader(reader, os.Stdout, "Press Enter to continue setup", "")
			if err == errSetupExit {
				printHumanInfo("Setup cancelled")
				return 0
			}
			if err != nil && err != errSetupBack {
				printHumanErr("%s", err)
				return 1
			}
		}
	}

	stage := setupStageHost
	verifyFirstReuseFlow := false
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
			stage = setupStageToken
		}
	}

	token := existingToken
	tokenChanged := false

	for {
		renderSetupHeader(os.Stdout)

		switch stage {
		case setupStageClient:
			answer, err := promptSetupClientInteractive(reader, os.Stdout, choices, target)
			if err == errSetupBack {
				continue
			}
			if err == errSetupExit {
				printHumanInfo("Setup cancelled")
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
			stage = setupStageHost

		case setupStageHost:
			defaultHost, _ := detectDefaultHAHostWithFeedback(os.Stdout, cfg)
			host, haURL, err := promptValidHAHostFromReader(reader, os.Stdout, defaultHost)
			if err == errSetupBack {
				if promptedClient {
					stage = setupStageClient
				}
				continue
			}
			if err == errSetupExit {
				printHumanInfo("Setup cancelled")
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			cfg = applySelectedSetupHost(cfg, host, haURL, relayURLFlag)
			if strings.TrimSpace(relayTokenFlag) != "" {
				stage = setupStageToken
			} else {
				stage = setupStageRelayInstall
			}

		case setupStageRelayInstall:
			steps := buildSetupWizardSteps(true)
			renderSetupStep(os.Stdout, steps.RelayInstall, steps.Total, "Install NOVA Relay in Home Assistant")
			renderSetupParagraph(os.Stdout,
				"I'll open your browser to add the HA NOVA repository.",
				`Just click "Open link" when prompted.`,
			)
			_, err := promptWizardLineFromReader(reader, os.Stdout, "Press Enter to open your browser", "")
			if err == errSetupBack {
				stage = setupStageHost
				continue
			}
			if err == errSetupExit {
				printHumanInfo("Setup cancelled")
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			if err := openBrowserForSetup("https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Fmarkusleben%2Fha-nova"); err != nil {
				printHumanWarn("Browser launch skipped; open this URL manually if needed: %s", "https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Fmarkusleben%2Fha-nova")
			}
			renderSetupIndentedBlock(os.Stdout, "Once the repository is added:", "    ",
				"1. Go to Settings > Apps > App Store",
				`2. Search for "NOVA Relay"`,
				"3. Click Install and wait for it to finish",
				"(don't start the app yet — we'll set up the tokens first)",
			)
			_, err = promptWizardLineFromReader(reader, os.Stdout, "Press Enter when the installation is complete", "")
			if err == errSetupBack {
				stage = setupStageHost
				continue
			}
			if err == errSetupExit {
				printHumanInfo("Setup cancelled")
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			stage = setupStageToken

		case setupStageToken:
			current = detectSetupState(paths, cfg, state, target)
			existingToken = ""
			if relayTokenFlag == "" {
				if existing, err := readRelayAuthToken(); err == nil {
					existingToken = strings.TrimSpace(existing)
				}
			}
			if relayTokenFlag != "" {
				token = strings.TrimSpace(relayTokenFlag)
			}
			resumeWSRecovery := relayTokenFlag == "" && strings.TrimSpace(existingToken) != "" && current.RelayOK && !current.WSOK
			verifyFirstReuseFlow = false
			steps := buildSetupWizardSteps(!skipLLATWalkthrough)
			if resumeWSRecovery {
				steps = buildSetupWizardSteps(false)
			}

			renderSetupStep(os.Stdout, steps.RelayToken, steps.Total, "Set up Relay Auth Token")
			renderSetupIndentedBlock(os.Stdout, `NOVA needs two passwords ("tokens") to work securely:`, "    ",
				"a) Relay token — keeps the connection between this computer and Home Assistant private",
				"b) HA access token — allows the relay to control your devices and automations",
			)
			renderSetupParagraphTight(os.Stdout, "This step is only for the Relay Auth Token. The Home Assistant Access Token comes next as its own step.")
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
					printHumanInfo("Setup cancelled")
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
						printHumanInfo("Setup cancelled")
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
					if err := openBrowserForSetup(cfg.HAURL + "/hassio/addon/2368fcfa_ha_nova_relay/config"); err != nil {
						printHumanWarn("Browser launch skipped; open this URL manually if needed: %s/hassio/addon/2368fcfa_ha_nova_relay/config", cfg.HAURL)
					}
					_, err := promptWizardLineFromReader(reader, os.Stdout, "Press Enter after you saved the Relay Auth Token in NOVA Relay", "")
					if err == errSetupBack {
						continue
					}
					if err == errSetupExit {
						printHumanInfo("Setup cancelled")
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
			if err := runSetupLLATWalkthrough(reader, os.Stdout, cfg, token, steps); err != nil {
				if err == errSetupBack {
					if relayTokenFlag != "" {
						stage = setupStageHost
					} else {
						stage = setupStageToken
					}
					continue
				}
				if err == errSetupExit {
					printHumanInfo("Setup cancelled")
					return 0
				}
				printHumanErr("%s", err)
				return 1
			}
			stage = setupStageVerify

		case setupStageVerify:
			if cfg.RelayBaseURL == "" {
				cfg.RelayBaseURL = deriveRelayURLFromHA(cfg.HAURL, cfg.HAHost)
			}

			steps := buildSetupWizardSteps(!skipLLATWalkthrough && !verifyFirstReuseFlow)
			renderSetupStep(os.Stdout, steps.Verify, steps.Total, "Verifying connection")
			issue, ok, err := verifySetupConnection(reader, os.Stdout, cfg, token, verifyFirstReuseFlow, relayTokenFlag == "")
			if err == errSetupBack {
				if !skipLLATWalkthrough && !verifyFirstReuseFlow {
					stage = setupStageLLAT
				} else if relayTokenFlag != "" {
					stage = setupStageHost
				} else {
					stage = setupStageToken
				}
				continue
			}
			if err == errSetupRelayTokenStep {
				stage = setupStageToken
				continue
			}
			if err == errSetupExit {
				printHumanInfo("Setup cancelled")
				return 0
			}
			if err != nil {
				printHumanErr("%s", err)
				return 1
			}
			if !ok {
				if err := persistInteractiveSetupState(paths, cfg, &state, savedTokenBeforeSetup, hadSavedTokenBeforeSetup, token); err != nil {
					printHumanErr("%s", err)
					return 1
				}
				renderSetupIncompleteBanner(os.Stdout, issue)
				return 1
			}
			if err := persistInteractiveSetupState(paths, cfg, &state, savedTokenBeforeSetup, hadSavedTokenBeforeSetup, token); err != nil {
				printHumanErr("%s", err)
				return 1
			}
			stage = setupStageSkills

		case setupStageSkills:
			steps := buildSetupWizardSteps(!skipLLATWalkthrough && !verifyFirstReuseFlow)
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
			if err := saveState(paths, state); err != nil {
				printHumanErr("cannot save state: %s", err)
				return 1
			}
			renderSetupCompleteBanner(os.Stdout, selectedClients)
			return 0
		}
	}
}
