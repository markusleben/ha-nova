package main

import "os"

func renderSetupCloudRecoveryBeforePrerequisiteFailure(
	paths runtimePaths,
) {
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil || cfg.Cloud == nil {
		return
	}
	if problem := cloudRecoveryHoldProblem(cfg); problem != nil {
		renderCloudRecoveryGuidance(os.Stdout, cfg, problem)
		return
	}
	renderCloudCheckpointActions(
		os.Stdout,
		paths,
		cfg,
		cloudRemoteFeatureAvailable() && !cfg.Cloud.ready(),
	)
}
