package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

type cloudCommandConnectionTarget struct {
	remote bool
	origin CloudOrigin
}

func resolveCloudCommandConnectionTarget(
	ctx context.Context,
	cfg runtimeConfig,
	options cloudCommandFlags,
	reconnect bool,
) (cloudCommandConnectionTarget, error) {
	remoteURL := cloudRemoteURLForCommand(cfg, options, reconnect)
	if remoteURL == "" &&
		cfg.RelaySecureBaseURL == "" &&
		cfg.RelaySpkiPin == "" {
		origin, err := promptCloudRemoteOriginForCommand(ctx)
		if err != nil {
			if errors.Is(err, errSetupBack) ||
				errors.Is(err, errSetupExit) {
				return cloudCommandConnectionTarget{},
					fmt.Errorf("%w: %w", errCloudURLPromptCancelled, err)
			}
			return cloudCommandConnectionTarget{}, err
		}
		return cloudCommandConnectionTarget{
			remote: true,
			origin: origin,
		}, nil
	}
	if remoteURL == "" {
		return cloudCommandConnectionTarget{}, nil
	}
	origin, err := resolveCanonicalNabuOriginForCloudCommand(
		ctx,
		remoteURL,
		NetCloudCNAMEResolver{},
	)
	if err != nil {
		return cloudCommandConnectionTarget{}, err
	}
	return cloudCommandConnectionTarget{
		remote: true,
		origin: origin,
	}, nil
}

func printCloudURLPromptCancellation(paths runtimePaths) {
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err == nil && cloudHoldIsSecurityStop(cfg) {
		printHumanInfo("Nothing was changed.")
		renderCloudSecurityHold(os.Stdout, cfg, selectedCloudCommandProfile(), true)
		return
	}
	if err == nil && cfg.Cloud != nil {
		printHumanInfo(
			"Home Assistant Link setup remains paused at the saved checkpoint %q. No authorization was changed by this attempt.",
			cfg.Cloud.State,
		)
		if hold := cloudRecoveryHoldProblem(cfg); hold != nil {
			printHumanInfo("The recovery safety hold remains active: %s", hold.Detail)
			if cloudRecoveryHoldNeedsUnlockForConfig(cfg) {
				printHumanInfo("Verify native secure storage with: %s", cloudUnlockCommand())
			}
			printHumanInfo("Verified cleanup remains available with: %s", cloudRemoveCommand())
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
			"Home Assistant Link setup was cancelled before an authorization checkpoint was saved. The existing server profile was not changed.",
		)
		return
	}
	printHumanInfo(
		"Home Assistant Link setup was cancelled before a checkpoint was saved. No authorization was changed.",
	)
}
