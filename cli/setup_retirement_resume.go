package main

func namedSetupIsRetirementOnly(
	cfg runtimeConfig,
	retirementPending bool,
) bool {
	if activeServerProfile() == defaultServerProfileName ||
		!retirementPending {
		return false
	}
	namedCloudWorkflow := remoteOnlyCloudSetup(cfg) ||
		(cfg.Cloud != nil &&
			(cloudRecoveryHoldProblem(cfg) != nil ||
				!cfg.Cloud.ready() ||
				!cloudRemoteFeatureAvailable()))
	return !namedCloudWorkflow
}

func resumeSetupDeviceCredentialRetirement(
	paths runtimePaths,
	cfg runtimeConfig,
) error {
	resumed, err := resumeDeviceCredentialRetirementCheckpoint(
		paths,
		cfg,
	)
	if err != nil {
		return err
	}
	if resumed {
		printHumanInfo(
			"Finished the interrupted device credential retirement.",
		)
	}
	return nil
}
