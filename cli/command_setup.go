package main

import (
	"errors"
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
	server := fs.String("server", "", "server profile name")
	nonInteractive := fs.Bool("non-interactive", false, "Disable prompts")
	serviceMode := fs.Bool("service", false, "Use a service-safe relay token file instead of desktop secure storage")
	if err := fs.Parse(normalizeSetupArgs(args)); err != nil {
		if helpRequested(err, fs, "ha-nova setup [client] [--server <name>] [--service] [--non-interactive] [--host <host>] [--ha-url <url>] [--relay-url <url>] [--relay-token <token>]") {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}
	serverSet := false
	fs.Visit(func(field *flag.Flag) {
		if field.Name == "server" {
			serverSet = true
		}
	})
	if serverSet {
		if strings.TrimSpace(*server) != *server {
			printHumanErr("invalid server profile name %q: whitespace is not allowed", *server)
			return 1
		}
		if err := validateServerProfileName(*server); err != nil {
			printHumanErr("%s", err)
			return 1
		}
		setServerSelectionOverride(*server)
	}
	setupGeneration, err := readInstallLifecycleGeneration(paths)
	if err != nil {
		renderSetupCloudRecoveryBeforePrerequisiteFailure(paths)
		printHumanErr("cannot inspect install lifecycle: %s", err)
		return 1
	}
	setupCensusMarker, err := readCensusLifecycleMarker(paths)
	if err != nil {
		renderSetupCloudRecoveryBeforePrerequisiteFailure(paths)
		printHumanErr("cannot inspect uninstall lifecycle: %s", err)
		return 1
	}
	setupConfigSnapshot, err := readSetupConfigSnapshot(paths)
	if err != nil {
		renderSetupCloudRecoveryBeforePrerequisiteFailure(paths)
		printHumanErr("cannot inspect server configuration: %s", err)
		return 1
	}
	setupLifecycle := [][]byte{setupGeneration, setupCensusMarker, setupConfigSnapshot}

	target := ""
	if remaining := fs.Args(); len(remaining) > 0 {
		target = remaining[0]
	}

	retirementProfile, retirementProfileErr :=
		resolveSetupRetirementProfile(paths)
	if retirementProfileErr != nil {
		renderSetupCloudRecoveryBeforePrerequisiteFailure(paths)
		retirementPending, pendingErr :=
			setupRetirementCheckpointExistsWithUnresolvedProfile(paths)
		if pendingErr != nil {
			printHumanErr(
				"cannot resolve the server profile (%s) or safely inspect interrupted device credential retirement (%s); credentials were not changed",
				retirementProfileErr,
				pendingErr,
			)
			return 1
		}
		if retirementPending {
			printHumanErr(
				"an interrupted device credential retirement is pending, but the server profile cannot be resolved because config.json is unreadable: %s. Restore config.json, then rerun setup; credentials were not changed.",
				retirementProfileErr,
			)
			return 1
		}
		printHumanErr(
			"cannot resolve the server profile before retirement recovery: %s",
			retirementProfileErr,
		)
		return 1
	}
	// Profile selection must be fixed before any setup guard or credential
	// operation. Config validation can fail before loadConfig reaches its normal
	// selection seam; leaving the seam at "default" would let a named local or
	// service setup evade namedSetupRequestAllowed.
	setActiveServerProfile(retirementProfile)
	retirementPending, retirementErr :=
		deviceCredentialRetirementCheckpointExistsForProfile(
			paths,
			retirementProfile,
		)
	if retirementErr != nil {
		renderSetupCloudRecoveryBeforePrerequisiteFailure(paths)
		printHumanErr(
			"cannot inspect interrupted device credential retirement: %s",
			retirementErr,
		)
		return 1
	}
	cfg, cfgErr := loadConfig(paths)
	if cfgErr != nil && retirementPending {
		printHumanErr(
			"an interrupted device credential retirement is pending, but the server configuration is unreadable: %s. Restore config.json, then rerun `%s`; credentials were not changed.",
			cfgErr,
			deviceRetirementSetupCommand(retirementProfile),
		)
		return 1
	}
	if cfgErr != nil {
		if errors.Is(cfgErr, errInvalidClientInstallID) {
			repaired, repairErr :=
				repairInvalidClientInstallIdentityForSetup(
					paths,
					cfgErr,
					setupLifecycle,
				)
			if repairErr != nil {
				printHumanErr(
					"cannot safely repair the local installation identity: %s",
					repairErr,
				)
				return 1
			}
			if repaired {
				printHumanInfo(
					"Repaired the local installation identity after verified Cloud cleanup.",
				)
				cfg, cfgErr = loadConfig(paths)
			}
		}
		if cfgErr != nil {
			if errors.Is(cfgErr, errInvalidServerProfileSelection) {
				printHumanErr("%s", cfgErr)
				return 1
			}
			// A mistyped --server/HA_NOVA_SERVER selection must fail loud
			// instead of silently running setup against a fresh config for the
			// wrong house. An explicit missing default is the one onboarding
			// exception.
			if errors.Is(cfgErr, errUnknownServerProfile) {
				if name, _ := requestedServerSelection(); name !=
					defaultServerProfileName {
					printHumanErr("%s", cfgErr)
					return 1
				}
			}
			var recoveryErr error
			cfg, recoveryErr = recoverSetupConfigAfterLoadError(
				paths,
				cfgErr,
			)
			if recoveryErr != nil {
				printHumanErr(
					"cannot safely continue setup with the saved server configuration: %s",
					recoveryErr,
				)
				return 1
			}
		}
	}
	if err := validateRuntimeConfigSave(paths, cfg); err != nil {
		renderSetupCloudRecoveryForValidatedConfig(cfg)
		printHumanErr(
			"cannot safely continue setup with the saved server configuration: %s",
			err,
		)
		return 1
	}
	unconstrainedCloudReuse := !*serviceMode &&
		strings.TrimSpace(*host) == "" &&
		strings.TrimSpace(*haURL) == "" &&
		strings.TrimSpace(*relayURL) == "" &&
		strings.TrimSpace(*relayToken) == ""
	if *nonInteractive &&
		handleNonInteractiveCloudSetupRecovery(paths, cfg) {
		return 1
	}
	// A named Cloud-only profile may use setup solely to resume Cloud onboarding
	// and install/sync client skills. An incomplete hybrid Cloud lifecycle may
	// also enter setup solely to expose its exact recovery actions. Recovery
	// always renders first so an invalid local request cannot hide a checkpoint.
	if *nonInteractive &&
		!namedSetupRequestAllowed(
			cfg,
			retirementPending,
			*serviceMode,
			*host,
			*haURL,
			*relayURL,
			*relayToken,
		) {
		renderNamedSetupRequestError()
		return 1
	}
	if *nonInteractive {
		namedRetirementOnly := namedSetupIsRetirementOnly(
			cfg,
			retirementPending,
		)
		if err := resumeSetupDeviceCredentialRetirement(
			paths,
			cfg,
		); err != nil {
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
			resumeSetupPendingActivation(
				paths,
				&cfg,
				setupLifecycle...,
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
	}
	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		renderSetupCloudRecoveryBeforePrerequisiteFailure(paths)
		printHumanErr("%s", err)
		return 1
	}

	if !*nonInteractive {
		return interactiveSetup(paths, cfg, state, target, *host, *haURL, *relayURL, *relayToken, *serviceMode, setupLifecycle...)
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
	if unconstrainedCloudReuse && remoteOnlyCloudSetup(cfg) {
		return completeNonInteractivePairedSetup(
			paths,
			cfg,
			state,
			selectedClients,
			setupLifecycle...,
		)
	}
	if unconstrainedCloudReuse &&
		effectiveRoutePolicy(cfg.RoutePolicy) == routePolicyAutomatic &&
		cfg.Cloud.ready() {
		return completeNonInteractivePairedSetup(
			paths,
			cfg,
			state,
			selectedClients,
			setupLifecycle...,
		)
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
		migrated := false
		migrateErr := withSetupLifecycleLock(paths, setupLifecycle, func() error {
			var err error
			migrated, err = migrateKeyringDeviceCredentialToFile()
			return err
		})
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
				return completeNonInteractivePairedSetup(paths, cfg, state, selectedClients, setupLifecycle...)
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
		return completeNonInteractivePairedSetup(paths, cfg, state, selectedClients, setupLifecycle...)
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
	releaseMutation, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		printHumanErr("another HA NOVA client update is already in progress")
		return 1
	}
	if err := ensureSetupLifecycleCurrent(paths, setupLifecycle...); err != nil {
		releaseMutation()
		printHumanErr("%s", err)
		return 1
	}
	previousToken, tokenErr := readRelayAuthToken()
	hadPreviousToken := tokenErr == nil && strings.TrimSpace(previousToken) != ""
	tokenChanged := !hadPreviousToken || previousToken != token
	if tokenChanged {
		if err := relayAuthTokenSetupPreflightForSetup(); err != nil {
			releaseMutation()
			printHumanErr("%s", relayAuthTokenSetupSaveError(err))
			if hint := setupSecureStorageRecoveryHint(err); hint != "" {
				printHumanWarn("%s", hint)
			}
			return 1
		}
	}
	configSnapshot, hadConfigSnapshot, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		releaseMutation()
		printHumanErr("cannot snapshot config: %s", err)
		return 1
	}
	stateSnapshot, hadStateSnapshot, err := readOptionalFile(paths.StateFile)
	if err != nil {
		releaseMutation()
		printHumanErr("cannot snapshot state: %s", err)
		return 1
	}
	if tokenChanged {
		if err := writeRelayAuthTokenForSetup(token); err != nil {
			releaseMutation()
			printHumanErr("%s", relayAuthTokenSetupSaveError(err))
			if hint := setupSecureStorageRecoveryHint(err); hint != "" {
				printHumanWarn("%s", hint)
			}
			return 1
		}
	}
	if err := saveConfigForSetup(paths, cfg); err != nil {
		rollbackErr := errors.Join(
			restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged),
			restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot),
		)
		releaseMutation()
		printHumanErr("cannot save config: %s", err)
		printSetupRollbackFailure(rollbackErr)
		return 1
	}
	if err := refreshSetupConfigSnapshot(paths, setupLifecycle); err != nil {
		rollbackErr := errors.Join(
			restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged),
			restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot),
		)
		releaseMutation()
		printHumanErr("cannot verify saved config: %s", err)
		printSetupRollbackFailure(rollbackErr)
		return 1
	}
	state.Version = localVersion(paths)
	state.InstallSource = detectInstallSource(paths, state)
	if err := mergeLatestSetupState(paths, &state); err != nil {
		rollbackErr := errors.Join(
			restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged),
			restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot),
		)
		releaseMutation()
		printHumanErr("cannot merge state: %s", err)
		printSetupRollbackFailure(rollbackErr)
		return 1
	}
	if err := saveStateForSetup(paths, state); err != nil {
		rollbackErr := errors.Join(
			restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged),
			restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot),
			restoreOptionalFile(paths.StateFile, stateSnapshot, hadStateSnapshot),
		)
		releaseMutation()
		printHumanErr("cannot save state: %s", err)
		printSetupRollbackFailure(rollbackErr)
		return 1
	}
	printHumanInfo("Saved HA NOVA configuration")

	_, issue, ok := verifySetupConnectionOnce(os.Stdout, cfg, token, false)
	if !ok {
		rollbackErr := errors.Join(
			restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged),
			restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot),
			restoreOptionalFile(paths.StateFile, stateSnapshot, hadStateSnapshot),
		)
		releaseMutation()
		renderSetupIncompleteBanner(os.Stdout, issue)
		printSetupRollbackFailure(rollbackErr)
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
		var retireErr error
		cfg, retireErr = saveConfigBeforeDeviceRetirement(paths, cfg, saveConfigForSetup)
		if retireErr != nil {
			rollbackErr := errors.Join(
				restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged),
				restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot),
				restoreOptionalFile(paths.StateFile, stateSnapshot, hadStateSnapshot),
			)
			releaseMutation()
			printHumanErr("cannot save config: %s", retireErr)
			printSetupRollbackFailure(rollbackErr)
			return 1
		}
		if err := refreshSetupConfigSnapshot(paths, setupLifecycle); err != nil {
			releaseMutation()
			printHumanErr("device pairing was retired safely, but the saved configuration could not be re-read: %s; rerun setup", err)
			return 1
		}
	}
	if err := installClientsAndSaveStateUnlocked(paths, &state, selectedClients, saveStateForSetup, setupLifecycle...); err != nil {
		releaseMutation()
		printHumanErr("client installation or state save failed: %s", err)
		return 1
	}
	finalizeServiceTokenFileMigration(formerServiceTokenFile, token)
	if err := completeSetupLifecycleUnlocked(paths, setupLifecycle...); err != nil {
		releaseMutation()
		printHumanWarn("Setup succeeded, but finalizing its lifecycle failed: %s", err)
		return 1
	}
	releaseMutation()

	return runDoctorWithCensusAsk(paths, nil, false)
}

// completeNonInteractivePairedSetup finishes setup for a passwordless-paired
// install, which has no legacy token: it verifies the device transport and
// persists config/state via the device path (stamping the version), then installs
// clients — instead of failing with "missing relay auth token".
func completeNonInteractivePairedSetup(paths runtimePaths, cfg runtimeConfig, state installState, selectedClients []string, lifecycleMarker ...[]byte) int {
	if !verifyDeviceHealth(cfg) {
		printHumanErr("This device is paired, but the secure connection could not be verified. Run 'ha-nova doctor', or re-pair with 'ha-nova setup' interactively.")
		return 1
	}
	if err := persistDeviceSetupState(paths, cfg, &state, lifecycleMarker...); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	printHumanInfo("Saved HA NOVA configuration (secure device pairing)")
	if err := withSetupLifecycleLock(paths, lifecycleMarker, func() error {
		if err := installClientsAndSaveStateUnlocked(paths, &state, selectedClients, saveStateForSetup, lifecycleMarker...); err != nil {
			return err
		}
		return completeSetupLifecycleUnlocked(paths, lifecycleMarker...)
	}); err != nil {
		printHumanErr("client installation or state save failed: %s", err)
		return 1
	}
	return runDoctorWithCensusAsk(paths, nil, false)
}

func restoreRelayAuthToken(previousToken string, hadPreviousToken, tokenChanged bool) error {
	if !tokenChanged {
		return nil
	}
	if hadPreviousToken {
		return writeRelayAuthToken(previousToken)
	}
	return deleteRelayAuthToken()
}

func printSetupRollbackFailure(err error) {
	if err != nil {
		printHumanErr("setup rollback incomplete: %s", err)
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
	case "host", "ha-url", "relay-url", "relay-token", "server":
		return true
	default:
		return false
	}
}
