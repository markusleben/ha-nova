package main

import (
	"flag"
	"io"
	"os"
	"strings"
)

const setupReadinessNoClientExitCode = 2

func runInternalSetupReadiness(paths runtimePaths, args []string) int {
	if len(args) != 0 {
		printHumanErr("internal-setup-readiness does not accept arguments")
		return 1
	}
	choices, err := buildSetupClientChoices(paths, loadStateOrDefault(paths))
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if !hasAvailableSetupClientChoice(choices) {
		return setupReadinessNoClientExitCode
	}
	return 0
}

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
		if helpRequested(err, fs, "ha-nova setup [client] [--service] [--non-interactive] [--host <host>] [--ha-url <url>] [--relay-url <url>] [--relay-token <token>]") {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}

	target := ""
	if remaining := fs.Args(); len(remaining) > 0 {
		target = remaining[0]
	}

	cfg, cfgErr := loadConfig(paths)
	if cfgErr != nil {
		// Preserve credential routing from an incomplete config: the token
		// file setting decides where token reads/writes go, so the repair
		// path must see it even when relay_base_url is missing.
		if raw, rawErr := loadJSONConfig(paths.ConfigFile); rawErr == nil {
			cfg.RelayTokenFile = raw.RelayTokenFile
		}
	}
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
		// Only after the service target and client checks passed: fulfill the
		// service contract for installs that paired into the desktop keyring.
		// Re-setups with a healthy pairing never reach a pairing stage, so a
		// readable keyring credential migrates to the private-file backend now
		// — a rejected invocation above must not mutate credential storage.
		migrated, migrateErr := migrateKeyringDeviceCredentialToFile()
		if migrateErr != nil {
			printHumanErr("cannot move the device credential into service file storage: %s", migrateErr)
			return 1
		}
		if migrated {
			printHumanInfo("Moved this install's device credential into protected service file storage.")
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
	// Only an EXPLICIT --relay-token expresses the intent to run this install
	// on the token path; a silently reused stored token must never retire a
	// working device pairing below.
	explicitTokenIntent := token != ""
	// A passwordless-paired install has no legacy token; its device credential in
	// the separate slot is the usable credential.
	pairedDeviceAvailable := func() bool {
		if explicitTokenIntent || cfg.RelaySecureBaseURL == "" || cfg.RelaySpkiPin == "" {
			return false
		}
		_, ok, credErr := readDeviceCredential()
		return credErr == nil && ok
	}
	if token == "" {
		if tokenStoragePreflightErr != nil {
			// Headless (no Secret Service): honor an existing device pairing —
			// whose credential is file-backed, no keyring — BEFORE failing on the
			// legacy-token store, so a paired box still installs/syncs clients.
			if pairedDeviceAvailable() {
				return completeNonInteractivePairedSetup(paths, cfg, state, selectedClients)
			}
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
	// No legacy token (stored or flag) but a device pairing exists: honor it
	// instead of prompting/requiring a token.
	if token == "" && pairedDeviceAvailable() {
		return completeNonInteractivePairedSetup(paths, cfg, state, selectedClients)
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

	_, issue, ok := verifySetupConnectionOnce(os.Stdout, cfg, token, false)
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

	// An EXPLICIT --relay-token now serves this install — retire any leftover
	// device pairing, or the trailing doctor run (and every skill call) would
	// resolve the dead pairing first and fail. Mirrors the interactive token
	// path, which is equally intent-gated. A stored token reused implicitly
	// (routine client re-sync on a paired install) keeps the pairing: the
	// legacy relay path accepting the token says nothing about the pairing
	// being dead, and revoking it server-side is irreversible.
	if explicitTokenIntent && (cfg.RelaySecureBaseURL != "" || cfg.RelaySpkiPin != "") {
		printHumanInfo("Switching this install to the provided relay token; retiring its device pairing.")
		retireDeviceCredential(&cfg)
		if err := saveConfigForSetup(paths, cfg); err != nil {
			printHumanErr("cannot save config: %s", err)
			return 1
		}
	}

	if err := installClients(paths, &state, selectedClients); err != nil {
		printHumanErr("client installation failed: %s", err)
		return 1
	}
	// Mark this version verified only if every tracked client was just synced from
	// the canonical install root; a subset sync (e.g. single-client setup over a
	// multi-client install) must leave the marker so the self-heal still repairs
	// the untouched clients.
	if allTrackedClientsSynced(state.InstalledClients, selectedClients) {
		state.ClientsVerifiedVersion = localVersion(paths)
	}
	if err := saveStateForSetup(paths, state); err != nil {
		printHumanErr("cannot save state: %s", err)
		return 1
	}

	finalizeServiceTokenFileMigration(formerServiceTokenFile, token)
	return runDoctor(paths, nil)
}

// completeNonInteractivePairedSetup finishes setup for a passwordless-paired
// install, which has no legacy token: it verifies the device transport and
// persists config/state via the device path (stamping the version), then installs
// clients — instead of failing with "missing relay auth token".
func completeNonInteractivePairedSetup(paths runtimePaths, cfg runtimeConfig, state installState, selectedClients []string) int {
	if !verifyDeviceHealth(cfg) {
		printHumanErr("This device is paired, but the secure connection could not be verified. Run 'ha-nova doctor', or re-pair with 'ha-nova setup' interactively.")
		return 1
	}
	if err := persistDeviceSetupState(paths, cfg, &state); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	printHumanInfo("Saved HA NOVA configuration (secure device pairing)")
	if err := installClients(paths, &state, selectedClients); err != nil {
		printHumanErr("client installation failed: %s", err)
		return 1
	}
	if allTrackedClientsSynced(state.InstalledClients, selectedClients) {
		state.ClientsVerifiedVersion = localVersion(paths)
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
