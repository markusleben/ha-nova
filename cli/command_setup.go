package main

import (
	"flag"
	"io"
	"os"
	"strings"
)

func runSetup(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", "", "Home Assistant host")
	haURL := fs.String("ha-url", "", "Home Assistant base URL")
	relayURL := fs.String("relay-url", "", "Relay base URL")
	relayToken := fs.String("relay-token", "", "Relay auth token")
	nonInteractive := fs.Bool("non-interactive", false, "Disable prompts")
	if err := fs.Parse(normalizeSetupArgs(args)); err != nil {
		printHumanErr("%s", err)
		return 1
	}

	target := ""
	if remaining := fs.Args(); len(remaining) > 0 {
		target = remaining[0]
	}

	cfg, _ := loadConfig(paths)
	state := loadStateOrDefault(paths)

	if !*nonInteractive {
		return interactiveSetup(paths, cfg, state, target, *host, *haURL, *relayURL, *relayToken)
	}

	if target == "" {
		target = "all"
	}
	selectedClients, skippedClients, err := resolveSetupClients(paths, target)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if target == "all" {
		if len(selectedClients) == 0 {
			printHumanErr("no supported AI clients detected on this machine yet")
			return 1
		}
		printHumanInfo("Will install for available clients: %s", strings.Join(selectedClients, ", "))
		if len(skippedClients) > 0 {
			printHumanWarn("Skipping until installed: %s", strings.Join(skippedClients, ", "))
		}
	}

	cfg, err = applySetupFlagOverrides(cfg, *host, *haURL, *relayURL)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if cfg.HAHost == "" && cfg.HAURL != "" {
		cfg.HAHost = strings.TrimPrefix(strings.TrimPrefix(cfg.HAURL, "http://"), "https://")
		cfg.HAHost = strings.TrimSuffix(cfg.HAHost, ":8123")
	}
	if cfg.HAHost == "" && !*nonInteractive {
		answer, err := promptLine("Home Assistant host", cfg.HAHost)
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
		cfg.HAHost = strings.TrimSpace(answer)
	}
	if cfg.HAHost == "" {
		printHumanErr("missing Home Assistant host; use --host or run interactively")
		return 1
	}
	if cfg.HAURL == "" {
		cfg.HAURL = "http://" + cfg.HAHost + ":8123"
	}
	if cfg.RelayBaseURL == "" {
		cfg.RelayBaseURL = "http://" + cfg.HAHost + ":8791"
	}

	tokenStoragePreflightErr := relayAuthTokenSetupPreflightForSetup()
	token := strings.TrimSpace(*relayToken)
	if token == "" {
		if tokenStoragePreflightErr != nil {
			printHumanErr("%s", relayAuthTokenProblemMessage(tokenStoragePreflightErr))
			if hint := setupSecureStorageRecoveryHint(tokenStoragePreflightErr); hint != "" {
				printHumanWarn("%s", hint)
			}
			return 1
		}
		if existing, err := readRelayAuthToken(); err == nil {
			token = existing
		} else if !isMissingRelayAuthTokenError(err) {
			printHumanErr("%s", relayAuthTokenProblemMessage(err))
			if hint := setupSecureStorageRecoveryHint(err); hint != "" {
				printHumanWarn("%s", hint)
			}
			return 1
		}
	}
	if token == "" && !*nonInteractive {
		answer, err := promptLine("Relay auth token", "")
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
		token = strings.TrimSpace(answer)
	}
	if token == "" {
		printHumanErr("missing relay auth token; use --relay-token or run interactively")
		return 1
	}

	if tokenStoragePreflightErr != nil {
		printHumanErr("%s", relayAuthTokenSetupSaveError(tokenStoragePreflightErr))
		if hint := setupSecureStorageRecoveryHint(tokenStoragePreflightErr); hint != "" {
			printHumanWarn("%s", hint)
		}
		return 1
	}
	previousToken, tokenErr := readRelayAuthToken()
	hadPreviousToken := tokenErr == nil && strings.TrimSpace(previousToken) != ""
	tokenChanged := !hadPreviousToken || previousToken != token

	configSnapshot, hadConfigSnapshot, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		printHumanErr("cannot snapshot config: %s", err)
		return 1
	}
	stateSnapshot, hadStateSnapshot, err := readOptionalFile(paths.StateFile)
	if err != nil {
		printHumanErr("cannot snapshot state: %s", err)
		return 1
	}
	if tokenChanged {
		if err := relayAuthTokenSetupPreflightForSetup(); err != nil {
			printHumanErr("%s", relayAuthTokenSetupSaveError(err))
			if hint := setupSecureStorageRecoveryHint(err); hint != "" {
				printHumanWarn("%s", hint)
			}
			return 1
		}
		if err := writeRelayAuthTokenForSetup(token); err != nil {
			printHumanErr("%s", relayAuthTokenSetupSaveError(err))
			if hint := setupSecureStorageRecoveryHint(err); hint != "" {
				printHumanWarn("%s", hint)
			}
			return 1
		}
	}

	if err := saveConfigForSetup(paths, cfg); err != nil {
		restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged)
		printHumanErr("cannot save config: %s", err)
		return 1
	}
	state.Version = localVersion(paths)
	state.InstallSource = detectInstallSource(paths, state)
	if err := saveStateForSetup(paths, state); err != nil {
		rollbackSetupPersistence(
			paths,
			previousToken,
			hadPreviousToken,
			tokenChanged,
			configSnapshot,
			hadConfigSnapshot,
			stateSnapshot,
			hadStateSnapshot,
		)
		printHumanErr("cannot save state: %s", err)
		return 1
	}

	printHumanInfo("Saved HA NOVA configuration")
	if !*nonInteractive {
		if err := copyToClipboardForSetup(token); err == nil {
			printHumanInfo("Copied relay token to clipboard")
		}
		if err := openBrowserForSetup(cfg.HAURL + "/hassio/addon/2368fcfa_ha_nova_relay/config"); err != nil {
			printHumanWarn("Browser launch skipped; open this URL manually if needed: %s/hassio/addon/2368fcfa_ha_nova_relay/config", cfg.HAURL)
		}
	}

	_, issue, ok := verifySetupConnectionOnce(os.Stdout, cfg, token)
	if !ok {
		rollbackSetupPersistence(
			paths,
			previousToken,
			hadPreviousToken,
			tokenChanged,
			configSnapshot,
			hadConfigSnapshot,
			stateSnapshot,
			hadStateSnapshot,
		)
		renderSetupIncompleteBanner(os.Stdout, issue)
		return 1
	}

	if err := installClients(paths, &state, selectedClients); err != nil {
		printHumanErr("client installation failed: %s", err)
		return 1
	}
	if err := saveStateForSetup(paths, state); err != nil {
		printHumanErr("cannot save state: %s", err)
		return 1
	}

	return runDoctor(paths, nil)
}

func rollbackSetupPersistence(
	paths runtimePaths,
	previousToken string,
	hadPreviousToken, tokenChanged bool,
	configSnapshot []byte,
	hadConfigSnapshot bool,
	stateSnapshot []byte,
	hadStateSnapshot bool,
) {
	if tokenChanged {
		restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged)
	}
	restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot)
	restoreOptionalFile(paths.StateFile, stateSnapshot, hadStateSnapshot)
}

func restoreRelayAuthToken(previousToken string, hadPreviousToken, tokenChanged bool) {
	if !tokenChanged {
		return
	}
	if hadPreviousToken {
		_ = writeRelayAuthToken(previousToken)
	} else {
		_ = deleteRelayAuthToken()
	}
}

func normalizeSetupArgs(args []string) []string {
	if len(args) < 2 {
		return args
	}
	if strings.HasPrefix(args[0], "-") || !isSetupTarget(args[0]) {
		return args
	}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			return append(append([]string{}, args[1:]...), args[0])
		}
	}
	return args
}

func isSetupTarget(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "all", "claude", "codex", "opencode", "gemini", "hermes":
		return true
	default:
		return false
	}
}
