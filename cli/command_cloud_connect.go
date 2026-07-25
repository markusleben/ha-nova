package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

var errCloudURLPromptCancelled = errors.New(
	"Cloud URL prompt cancelled before setup started",
)

func runCloudConnectCommand(
	paths runtimePaths,
	args []string,
	reconnect bool,
) int {
	command := "add"
	if reconnect {
		command = "reconnect"
	}
	options, err := parseCloudCommandFlags(command, args)
	if errors.Is(err, errHelpShown) {
		return 0
	}
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	loadedCfg, loadedCfgErr := loadCloudManagementConfig(paths)
	if loadedCfgErr == nil {
		if problem := cloudRecoveryHoldProblem(loadedCfg); problem != nil {
			renderCloudRecoveryGuidance(os.Stdout, loadedCfg, problem)
			return 1
		}
	}
	if err := requireCloudRemoteFeatureForSetup(); err != nil {
		printHumanErr("%s", err)
		renderDurableCloudRecoveryGuidance(
			os.Stdout,
			paths,
			cloudAdapterUnavailableProblem(),
		)
		return 1
	}
	if !cloudInteractivePromptSessionForSetup() {
		printHumanErr(
			"Home Assistant Cloud setup requires an interactive desktop session: use a local, non-elevated graphical desktop terminal (not SSH, sudo/root, or WSL); no authorization was changed.",
		)
		if loadedCfgErr == nil && loadedCfg.Cloud != nil {
			renderCloudCheckpointActions(
				os.Stdout,
				paths,
				loadedCfg,
				true,
			)
		}
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	ctx = withCloudSecretAccessHolder(ctx)

	var connected runtimeConfig
	var configPreflightErr error
	err = withClientMutationLock(paths, func() error {
		cfg, err := loadCloudConnectConfig(paths, options, reconnect)
		if err != nil {
			return err
		}
		if err := validateRuntimeConfigSave(paths, cfg); err != nil {
			configPreflightErr = fmt.Errorf(
				"cannot safely continue Home Assistant Cloud setup with the saved server configuration: %w",
				err,
			)
			return configPreflightErr
		}
		connected = cfg
		connected, err = connectCloudCommandLocked(
			ctx,
			paths,
			cfg,
			options,
			reconnect,
		)
		return err
	})
	if err != nil {
		if configPreflightErr != nil {
			printHumanErr("%s", configPreflightErr)
			return 1
		}
		if errors.Is(err, errCloudURLPromptCancelled) {
			printCloudURLPromptCancellation(paths)
			return 0
		}
		if handlePausedCloudOwnerPairing(os.Stdout, paths, err) {
			return 0
		}
		renderCloudFailure(os.Stdout, paths, err)
		return 1
	}
	if effectiveRoutePolicy(connected.RoutePolicy) == routePolicyCloud {
		printHumanInfo("Home Assistant Cloud access is ready. This profile uses Cloud routing.")
		return 0
	}
	printHumanInfo("Home Assistant Cloud access is ready. Local access stays preferred.")
	return 0
}

func printCloudURLPromptCancellation(paths runtimePaths) {
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err == nil && cfg.Cloud != nil {
		printHumanInfo(
			"Home Assistant Cloud setup remains paused at the saved checkpoint %q. No authorization was changed by this attempt.",
			cfg.Cloud.State,
		)
		if hold := cloudRecoveryHoldProblem(cfg); hold != nil {
			printHumanInfo("The recovery safety hold remains active: %s", hold.Detail)
			if cloudRecoveryHoldClearsAfterUnlock(cfg.Cloud.RecoveryHold) {
				printHumanInfo(
					"Verify native secure storage with: %s",
					cloudUnlockCommand(),
				)
			}
			printHumanInfo(
				"Verified cleanup remains available with: %s",
				cloudRemoveCommand(),
			)
			return
		}
		if resume := cloudResumeCommand(cfg); resume != "" {
			printHumanInfo("Resume with: %s", resume)
		}
		printHumanInfo(
			"Verified cleanup remains available with: %s",
			cloudRemoveCommand(),
		)
		return
	}
	if err == nil && cfg.ProfileID != "" {
		printHumanInfo(
			"Home Assistant Cloud setup was cancelled before an authorization checkpoint was saved. The existing server profile was not changed.",
		)
		return
	}
	printHumanInfo(
		"Home Assistant Cloud setup was cancelled before a checkpoint was saved. No authorization was changed.",
	)
}

func loadCloudConnectConfig(
	paths runtimePaths,
	options cloudCommandFlags,
	reconnect bool,
) (runtimeConfig, error) {
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err == nil {
		if err := validateServerProfileName(activeServerProfile()); err != nil {
			return runtimeConfig{}, fmt.Errorf(
				"invalid selected server profile: %w",
				err,
			)
		}
		return cfg, nil
	}
	if !reconnect &&
		options.server != "" &&
		errors.Is(err, errUnknownServerProfile) {
		// Explicit --server is creation intent. Seed only that missing profile;
		// saveConfig's document editor preserves every existing sibling and
		// default. Environment-only selection never creates profiles.
		setActiveServerProfile(options.server)
		return cloudProfileSeedConfig(paths)
	}
	if reconnect || !errors.Is(err, os.ErrNotExist) {
		return runtimeConfig{}, err
	}
	profile := options.server
	if profile == "" {
		if selected, source := requestedServerSelection(); source == serverSelectionEnvVar &&
			selected != "" && selected != defaultServerProfileName {
			return runtimeConfig{}, fmt.Errorf(
				"creating server profile %q requires the explicit flag --server %s; %s alone is not accepted for a fresh Cloud setup",
				selected,
				selected,
				serverSelectionEnvVar,
			)
		}
		profile = defaultServerProfileName
	}
	setActiveServerProfile(profile)
	return cloudProfileSeedConfig(paths)
}

func cloudProfileSeedConfig(paths runtimePaths) (runtimeConfig, error) {
	cfg := runtimeConfig{RoutePolicy: routePolicyLocal}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err == nil {
		cfg.ClientInstallID = doc.meta.ClientInstallID
		if err := ensureProfileID(&cfg); err != nil {
			return runtimeConfig{}, err
		}
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		if err := ensureProfileID(&cfg); err != nil {
			return runtimeConfig{}, err
		}
		return cfg, nil
	}
	return runtimeConfig{}, err
}

func connectCloudCommandLocked(
	ctx context.Context,
	paths runtimePaths,
	cfg runtimeConfig,
	options cloudCommandFlags,
	reconnect bool,
) (runtimeConfig, error) {
	remoteURL := cloudRemoteURLForCommand(cfg, options, reconnect)
	var remoteOrigin CloudOrigin
	useRemote := remoteURL != ""
	if remoteURL == "" &&
		cfg.RelaySecureBaseURL == "" &&
		cfg.RelaySpkiPin == "" {
		var err error
		remoteOrigin, err = promptCloudRemoteOriginForCommand(ctx)
		if err != nil {
			if errors.Is(err, errSetupBack) ||
				errors.Is(err, errSetupExit) {
				return cfg, errCloudURLPromptCancelled
			}
			return cfg, err
		}
		useRemote = true
	}
	save := func(value runtimeConfig) error {
		return saveConfig(paths, value)
	}
	if useRemote {
		remoteCoordinator, ok := cloudCoordinatorForSetup.(cloudRemoteSetupCoordinator)
		if !ok {
			return cfg, cloudAdapterUnavailableProblem()
		}
		if remoteOrigin.CanonicalOrigin == "" {
			var err error
			remoteOrigin, err = resolveCanonicalNabuOriginForCloudCommand(
				ctx,
				remoteURL,
				NetCloudCNAMEResolver{},
			)
			if err != nil {
				return cfg, err
			}
		}
		updated, err := connectRemoteToCloud(
			ctx,
			paths,
			cfg,
			remoteCoordinator,
			remoteOrigin,
			interactiveRemotePairingCode,
			reconnect,
			save,
		)
		return updated, err
	}
	updated, err := connectExistingDeviceToCloud(
		ctx,
		paths,
		cfg,
		cloudCoordinatorForSetup,
		reconnect,
		save,
	)
	return updated, err
}

var promptCloudRemoteOriginForCommand = func(
	ctx context.Context,
) (CloudOrigin, error) {
	return promptCloudRemoteOriginFromReader(
		ctx,
		bufio.NewReader(os.Stdin),
		os.Stdout,
		NetCloudCNAMEResolver{},
	)
}

var resolveCanonicalNabuOriginForCloudCommand = ResolveCanonicalNabuOrigin

func promptCloudRemoteOriginFromReader(
	ctx context.Context,
	reader *bufio.Reader,
	out io.Writer,
	resolver CloudCNAMEResolver,
) (CloudOrigin, error) {
	renderSetupParagraph(
		out,
		"Enter the Home Assistant Cloud remote URL shown in Home Assistant under Settings > Home Assistant Cloud.",
	)
	return promptValidatedCloudRemoteOrigin(
		ctx,
		reader,
		out,
		"",
		resolver,
	)
}

func cloudRemoteURLForCommand(
	cfg runtimeConfig,
	options cloudCommandFlags,
	reconnect bool,
) string {
	if options.url != "" {
		return options.url
	}
	if reconnect {
		// Reconnect is a Cloud operation and must work away from the LAN. The
		// persisted Relay identity still binds the remote flow fail-closed; a
		// different app instance cannot inherit the old device or OAuth grant.
		return cloudOriginFromReconnectConfig(cfg)
	}
	if cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
		return ""
	}
	return cloudOriginFromReconnectConfig(cfg)
}

func interactiveRemotePairingCode(
	prompt cloudRemotePairingPrompt,
) (string, error) {
	return promptRemoteOwnerPairingCode(
		bufio.NewReader(os.Stdin),
		os.Stdout,
		prompt,
	)
}

func cloudOriginFromReconnectConfig(cfg runtimeConfig) string {
	if cfg.Cloud == nil {
		return ""
	}
	if cfg.Cloud.Pending != nil {
		return strings.TrimSpace(cfg.Cloud.Pending.Origin)
	}
	if cfg.Cloud.Current != nil {
		return strings.TrimSpace(cfg.Cloud.Current.Origin)
	}
	return ""
}
