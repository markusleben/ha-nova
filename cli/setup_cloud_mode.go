package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

type setupConnectionMode string

const (
	setupConnectionLocal  setupConnectionMode = "local"
	setupConnectionHybrid setupConnectionMode = "hybrid"
	setupConnectionCloud  setupConnectionMode = "cloud"
)

var verifyCloudDeviceHealthForSetup = verifyCloudDeviceHealth

func promptSetupConnectionMode(
	reader *bufio.Reader,
	out io.Writer,
) (setupConnectionMode, error) {
	for {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  How should this computer connect to Home Assistant?")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "    1) Local only (recommended)")
		fmt.Fprintln(out, "       No Home Assistant Cloud subscription required.")
		fmt.Fprintln(out, "    2) Local + Home Assistant Cloud")
		fmt.Fprintln(out, "       Fast at home; secure remote access when away.")
		fmt.Fprintln(out, "    3) Home Assistant Cloud only")
		fmt.Fprintln(out, "       For a computer that is not on the Home Assistant network.")
		fmt.Fprintln(out)
		fmt.Fprint(out, "  Enter [1-3] (default 1, or type 'back'/'exit'): ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "1", "local", "local only":
			return setupConnectionLocal, nil
		case "2", "hybrid", "local + cloud":
			return setupConnectionHybrid, nil
		case "3", "cloud", "cloud only":
			return setupConnectionCloud, nil
		case "back":
			return "", errSetupBack
		case "exit":
			return "", errSetupExit
		default:
			renderSetupErrorLine(out, "Invalid choice. Please enter 1, 2, or 3.")
		}
	}
}

func shouldOfferSetupConnectionMode(
	cfg runtimeConfig,
	hostFlag, haURLFlag, relayURLFlag, relayTokenFlag string,
	serviceMode bool,
) bool {
	return !serviceMode &&
		cloudCoordinatorForSetup != nil &&
		cloudCoordinatorForSetup.Available() &&
		cloudConnectionModePromptAvailable(cloudCoordinatorForSetup) &&
		cfg.Cloud == nil &&
		cfg.HAHost == "" &&
		cfg.HAURL == "" &&
		cfg.RelayBaseURL == "" &&
		cfg.RelaySecureBaseURL == "" &&
		strings.TrimSpace(hostFlag) == "" &&
		strings.TrimSpace(haURLFlag) == "" &&
		strings.TrimSpace(relayURLFlag) == "" &&
		strings.TrimSpace(relayTokenFlag) == ""
}

func cloudConnectionModePromptAvailable(
	coordinator cloudSetupCoordinator,
) bool {
	if _, production := coordinator.(productionCloudSetupCoordinator); production {
		return cloudInteractivePromptSessionForSetup()
	}
	return true
}

func hybridCloudSetupPending(cfg runtimeConfig) bool {
	return cfg.Cloud != nil &&
		!cfg.Cloud.ready() &&
		strings.TrimSpace(cfg.RelaySecureBaseURL) != "" &&
		strings.TrimSpace(cfg.RelaySpkiPin) != ""
}

func runInteractiveCloudOnlySetup(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	cfg runtimeConfig,
	state *installState,
	target string,
	selectedClients, skippedClients []string,
	lifecycleMarker ...[]byte,
) int {
	exit, back := runInteractiveCloudOnlySetupForWizard(
		reader,
		out,
		paths,
		cfg,
		state,
		target,
		selectedClients,
		skippedClients,
		lifecycleMarker...,
	)
	if back {
		renderSetupCancelledNote(out)
	}
	return exit
}

func runInteractiveCloudOnlySetupForWizard(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	cfg runtimeConfig,
	state *installState,
	target string,
	selectedClients, skippedClients []string,
	lifecycleMarker ...[]byte,
) (exit int, back bool) {
	if problem := cloudRecoveryHoldProblem(cfg); problem != nil {
		renderCloudRecoveryGuidance(out, cfg, problem)
		return 1, false
	}
	if err := requireCloudRemoteFeatureForSetup(); err != nil {
		printHumanErr("%s", err)
		return 1, false
	}
	coordinator, ok := cloudCoordinatorForSetup.(cloudRemoteSetupCoordinator)
	if !ok {
		printHumanErr("%s", cloudAdapterUnavailableProblem())
		return 1, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	ctx = withCloudSecretAccessHolder(ctx)
	renderSetupHeader(out)
	renderSetupParagraph(
		out,
		"Enter the Home Assistant Cloud remote URL shown in Home Assistant under Settings > Home Assistant Cloud.",
		"OAuth credentials will stay only in this computer's native secure storage.",
	)
	origin, err := promptValidatedCloudRemoteOrigin(
		ctx,
		reader,
		out,
		"",
		NetCloudCNAMEResolver{},
	)
	if err == errSetupBack {
		return 0, true
	}
	if err == errSetupExit {
		renderSetupCancelledNote(out)
		return 0, false
	}
	if err != nil {
		renderCloudFailure(out, paths, err)
		return 1, false
	}
	pairingCode := cloudOnlyPairingCodePrompt(reader, out)
	err = withClientMutationLock(paths, func() error {
		save := func(value runtimeConfig) error {
			return saveSetupConfigWithLifecycleUnlocked(
				paths,
				value,
				lifecycleMarker...,
			)
		}
		updated, connectErr := connectRemoteToCloud(
			ctx,
			paths,
			cfg,
			coordinator,
			origin,
			pairingCode,
			false,
			save,
		)
		cfg = updated
		return connectErr
	})
	if err != nil {
		if handlePausedCloudOwnerPairing(out, paths, err) {
			return 0, false
		}
		renderCloudFailure(out, paths, err)
		return 1, false
	}
	if _, err := loadAndVerifyCloudHealthWithCheckpoint(
		ctx,
		paths,
		verifyCloudDeviceHealthForSetup,
		nil,
	); err != nil {
		renderCloudFailure(out, paths, err)
		return 1, false
	}
	return finishInteractiveCloudOnlySetup(
		reader,
		out,
		paths,
		state,
		target,
		selectedClients,
		skippedClients,
		lifecycleMarker...,
	), false
}

func addHybridCloudAfterLocal(
	paths runtimePaths,
	cfg runtimeConfig,
	lifecycleMarker ...[]byte,
) (runtimeConfig, error) {
	if err := requireCloudRemoteFeatureForSetup(); err != nil {
		return cfg, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	ctx = withCloudSecretAccessHolder(ctx)
	updated := cfg
	err := withClientMutationLock(paths, func() error {
		var connectErr error
		updated, connectErr = connectExistingDeviceToCloud(
			ctx,
			paths,
			cfg,
			cloudCoordinatorForSetup,
			false,
			func(value runtimeConfig) error {
				return saveSetupConfigWithLifecycleUnlocked(
					paths,
					value,
					lifecycleMarker...,
				)
			},
		)
		return connectErr
	})
	return updated, err
}
