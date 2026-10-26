package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var errCloudURLPromptCancelled = errors.New(
	"Cloud URL prompt cancelled before setup started",
)

func runCloudConnectCommand(paths runtimePaths, args []string, reconnect bool) int {
	code, _ := runCloudConnect(paths, args, reconnect)
	return code
}

// runCloudConnect: exited when the person left a prompt with Ctrl+C.
func runCloudConnect(
	paths runtimePaths,
	args []string,
	reconnect bool,
) (code int, exited bool) {
	command := "add"
	if reconnect {
		command = "reconnect"
	}
	options, err := parseCloudCommandFlags(command, args)
	if errors.Is(err, errHelpShown) {
		return 0, false
	}
	if err != nil {
		printHumanErr("%s", err)
		return 1, false
	}
	loadedCfg, loadedCfgErr := loadCloudManagementConfig(paths)
	if loadedCfgErr == nil {
		if problem := cloudRecoveryHoldProblem(loadedCfg); problem != nil {
			renderCloudRecoveryGuidance(os.Stdout, loadedCfg, problem)
			return 1, false
		}
	}
	if err := requireCloudRemoteFeatureForSetup(); err != nil {
		printHumanErr("%s", err)
		renderDurableCloudRecoveryGuidance(
			os.Stdout,
			paths,
			cloudAdapterUnavailableProblem(),
		)
		return 1, false
	}
	if err := preflightSavedCloudConnectConfig(
		paths,
		options,
		reconnect,
	); err != nil {
		printHumanErr("%s", err)
		return 1, false
	}
	if options.remoteResume {
		if loadedCfgErr != nil || !cloudRemoteResumeCheckpointReady(loadedCfg) {
			printHumanErr(
				"--remote-resume only resumes an existing checkpoint at \"cloud_verified\" or later: run the authorization once from an interactive desktop session first; no authorization was changed.",
			)
			return 1, false
		}
		enableCloudRemoteResumeSession()
	}
	if !cloudInteractivePromptSessionForSetup() {
		printHumanErr(
			"Home Assistant Link setup requires an interactive desktop session: use a local, non-elevated graphical desktop terminal (not SSH, sudo/root, or WSL); no authorization was changed.",
		)
		if loadedCfgErr == nil && loadedCfg.Cloud != nil {
			renderCloudCheckpointActions(
				os.Stdout,
				paths,
				loadedCfg,
				true,
			)
			if cloudRemoteResumeCheckpointReady(loadedCfg) {
				fmt.Fprintln(
					os.Stdout,
					"  This checkpoint can also finish over SSH: add --remote-resume to the resume command above.",
				)
			}
		}
		return 1, false
	}
	ctx, cancel := newInteractiveCloudSetupContext()
	defer cancel()
	configSnapshot, hadConfig, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		printHumanErr("cannot inspect server configuration: %s", err)
		return 1, false
	}
	previewCfg, err := loadCloudConnectConfig(paths, options, reconnect)
	if err != nil {
		printHumanErr("%s", err)
		return 1, false
	}
	target, err := resolveCloudCommandConnectionTarget(
		ctx,
		previewCfg,
		options,
		reconnect,
	)
	if err != nil {
		if errors.Is(err, errCloudURLPromptCancelled) {
			printCloudURLPromptCancellation(paths)
			return 0, errors.Is(err, errSetupExit)
		}
		renderCloudFailure(os.Stdout, paths, err)
		return 1, false
	}

	var connected runtimeConfig
	var configPreflightErr error
	err = withPausableClientMutationLock(
		paths,
		func(mutation *pausableClientMutationLock) error {
			if err := ensureOptionalFileSnapshotCurrent(
				paths.ConfigFile,
				configSnapshot,
				hadConfig,
			); err != nil {
				return err
			}
			cfg, err := loadCloudConnectConfig(paths, options, reconnect)
			if err != nil {
				return err
			}
			if err := validateCloudConnectSavedConfig(paths, cfg); err != nil {
				configPreflightErr = err
				return configPreflightErr
			}
			if err := validateCloudConnectIntent(cfg, reconnect); err != nil {
				return err
			}
			connected = cfg
			connected, err = connectCloudCommandLocked(
				ctx,
				paths,
				cfg,
				reconnect,
				target,
				mutation.pairingCodeProvider(
					interactiveRemotePairingCode,
				),
				mutation,
			)
			return err
		},
	)
	if err != nil {
		if configPreflightErr != nil {
			printHumanErr("%s", configPreflightErr)
			return 1, false
		}
		if errors.Is(err, errCloudURLPromptCancelled) {
			printCloudURLPromptCancellation(paths)
			return 0, errors.Is(err, errSetupExit)
		}
		if handlePausedCloudOwnerPairing(os.Stdout, paths, err) {
			return 0, errors.Is(err, errSetupExit)
		}
		if errors.Is(err, errCloudAlreadyConfigured) {
			printHumanErr("%s", err)
			return 1, false
		}
		renderCloudFailure(os.Stdout, paths, err)
		return 1, false
	}
	if effectiveRoutePolicy(connected.RoutePolicy) == routePolicyCloud {
		printHumanInfo("Home Assistant Link is ready. This server always connects through Link.")
		return 0, false
	}
	printHumanInfo("Home Assistant Link access is ready. Local access stays preferred.")
	return 0, false
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
	reconnect bool,
	target cloudCommandConnectionTarget,
	pairingCode cloudRemotePairingCodeProvider,
	mutation *pausableClientMutationLock,
) (runtimeConfig, error) {
	save := func(value runtimeConfig) error {
		if err := mutation.requireHeld(); err != nil {
			return err
		}
		return saveConfig(paths, value)
	}
	if target.remote {
		remoteCoordinator, ok := cloudCoordinatorForSetup.(cloudRemoteSetupCoordinator)
		if !ok {
			return cfg, cloudAdapterUnavailableProblem()
		}
		updated, err := connectRemoteToCloud(
			ctx,
			paths,
			cfg,
			remoteCoordinator,
			target.origin,
			pairingCode,
			reconnect,
			save,
			mutation,
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
		mutation,
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
		"Enter the Home Assistant Link remote URL shown in Home Assistant under Settings > Home Assistant Cloud (Home Assistant 2026.12 and newer: Settings > Home Assistant Link).",
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
