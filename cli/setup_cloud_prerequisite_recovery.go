package main

import "os"

func renderSetupCloudRecoveryBeforePrerequisiteFailure(
	paths runtimePaths,
) {
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		return
	}
	if err := validateLoadedRuntimeConfig(&cfg); err != nil {
		return
	}
	renderSetupCloudRecoveryForValidatedConfig(cfg)
}

func renderSetupCloudRecoveryForValidatedConfig(
	cfg runtimeConfig,
) {
	if cfg.Cloud == nil {
		return
	}
	if problem := cloudRecoveryHoldProblem(cfg); problem != nil {
		renderCloudRecoveryGuidance(os.Stdout, cfg, problem)
		return
	}
	renderCloudCheckpointActionsForProfile(
		os.Stdout,
		cfg,
		cloudRemoteFeatureAvailable() && !cfg.Cloud.ready(),
		selectedCloudCommandProfile(),
	)
}
