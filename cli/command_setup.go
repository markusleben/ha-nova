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
	serviceMode := fs.Bool("service", false, "Use a service-safe relay token file instead of desktop secure storage")
	if err := fs.Parse(normalizeSetupArgs(args)); err != nil {
		printHumanErr("%s", err)
		return 1
	}

	target := ""
	if remaining := fs.Args(); len(remaining) > 0 {
		target = remaining[0]
	}

	cfg, _ := loadConfig(paths)
	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}

	if !*nonInteractive {
		return interactiveSetup(paths, cfg, state, target, *host, *haURL, *relayURL, *relayToken, *serviceMode)
	}

	if target == "" {
		target = "all"
	}
	if *serviceMode && target == "all" {
		printHumanErr("service credentials require a specific client; use: ha-nova setup --service <client>")
		return 1
	}
	selectedClients, skippedClients, err := resolveSetupClientsWithState(paths, target, state)
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

	if *serviceMode {
		if err := requireSelectedClientServiceCredentials(paths, selectedClients); err != nil {
			printHumanErr("%s", err)
			return 1
		}
	}
	cfg, err = applySetupFlagOverrides(cfg, *host, *haURL, *relayURL)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	formerServiceTokenFile := ""
	migrationToken := ""
	if *serviceMode {
		// Read any already-stored token BEFORE the file override redirects
		// token reads, so `--service` without --relay-token migrates an
		// existing desktop-keyring token into the service token file.
		if existing, err := readRelayAuthToken(); err == nil {
			migrationToken = strings.TrimSpace(existing)
		}
		cfg = enableServiceRelayTokenFile(paths, cfg)
		restoreTokenFileOverride := withRelayAuthTokenFileOverride(cfg.RelayTokenFile)
		defer restoreTokenFileOverride()
	} else {
		var liftTokenFileSuppression func()
		cfg, formerServiceTokenFile, migrationToken, liftTokenFileSuppression = disableServiceRelayTokenFile(paths, cfg)
		defer liftTokenFileSuppression()
	}
	if cfg.HAHost == "" && cfg.HAURL != "" {
		cfg.HAHost = strings.TrimPrefix(strings.TrimPrefix(cfg.HAURL, "http://"), "https://")
		cfg.HAHost = strings.TrimSuffix(cfg.HAHost, ":8123")
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
		} else if isMissingRelayAuthTokenError(err) && migrationToken != "" {
			// Mode switch in either direction: migrate the token that was
			// stored in the previous backend (keyring or service file).
			token = migrationToken
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

	finalizeServiceTokenFileMigration(formerServiceTokenFile, token)
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
	targetIndex := -1
	skipNext := false
	for index, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if setupFlagRequiresValue(arg) {
				skipNext = true
			}
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			targetIndex = index
		}
	}
	if targetIndex < 0 || targetIndex == len(args)-1 {
		return args
	}
	for _, arg := range args[targetIndex+1:] {
		if strings.HasPrefix(arg, "-") {
			normalized := append([]string{}, args[:targetIndex]...)
			normalized = append(normalized, args[targetIndex+1:]...)
			return append(normalized, args[targetIndex])
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

func setupFlagRequiresValue(arg string) bool {
	name := strings.TrimLeft(arg, "-")
	if strings.Contains(name, "=") {
		return false
	}
	switch name {
	case "host", "ha-url", "relay-url", "relay-token":
		return true
	default:
		return false
	}
}
