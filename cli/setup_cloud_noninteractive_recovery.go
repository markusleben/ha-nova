package main

import "os"

func handleNonInteractiveCloudSetupRecovery(
	paths runtimePaths,
	cfg runtimeConfig,
) bool {
	if cfg.Cloud == nil {
		return false
	}
	if handleNonInteractiveCloudRecoveryHold(paths, cfg) {
		return true
	}
	if !cloudRemoteFeatureAvailable() {
		printHumanErr("%s", cloudAdapterUnavailableProblem())
		printHumanInfo(
			"Local access remains available, but saved Home Assistant Cloud state requires explicit cleanup.",
		)
		renderCloudCheckpointActions(
			os.Stdout,
			paths,
			cfg,
			false,
		)
		return true
	}
	if cfg.Cloud.ready() {
		return false
	}
	printHumanErr(
		"Home Assistant Cloud setup is incomplete and requires an interactive desktop session.",
	)
	renderCloudCheckpointActions(
		os.Stdout,
		paths,
		cfg,
		true,
	)
	return true
}

func handleNonInteractiveCloudRecoveryHold(
	paths runtimePaths,
	cfg runtimeConfig,
) bool {
	problem := cloudRecoveryHoldProblem(cfg)
	if problem == nil {
		return false
	}
	printHumanErr(
		"Home Assistant Cloud setup is paused by a recovery safety hold: %s",
		problem,
	)
	profile, err := cloudRecoveryCommandProfile(paths)
	if err != nil {
		printHumanInfo(
			"No cleanup command is shown until the server profile selection is repaired: %v",
			err,
		)
		return true
	}
	printHumanInfo(
		"Verified cleanup is the only non-interactive recovery action: %s",
		cloudProfileCommandFor("remove", profile),
	)
	return true
}
