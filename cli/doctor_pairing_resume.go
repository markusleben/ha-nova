package main

func resumeInterruptedPairingForDoctor(
	paths runtimePaths,
	cfg *runtimeConfig,
	lifecycleGeneration []byte,
	configSnapshot []byte,
	hadConfigSnapshot bool,
) (bool, error) {
	return resumeInterruptedPairing(
		paths,
		cfg,
		lifecycleGeneration,
		configSnapshot,
		hadConfigSnapshot,
		resumePendingActivationAfterRetirementGuard,
	)
}

func resumeInterruptedPairingForExplicitPair(
	paths runtimePaths,
	cfg *runtimeConfig,
	lifecycleGeneration []byte,
	configSnapshot []byte,
	hadConfigSnapshot bool,
) (bool, error) {
	return resumeInterruptedPairingForPairCommand(
		paths,
		cfg,
		lifecycleGeneration,
		configSnapshot,
		hadConfigSnapshot,
		SecretStoreAllowUI,
	)
}

func resumeInterruptedPairingForPairCommand(
	paths runtimePaths,
	cfg *runtimeConfig,
	lifecycleGeneration []byte,
	configSnapshot []byte,
	hadConfigSnapshot bool,
	ui SecretStoreUIPolicy,
) (bool, error) {
	resume := resumePendingActivationAfterRetirementGuard
	if ui == SecretStoreAllowUI {
		resume =
			resumePendingActivationForExplicitPairAfterRetirementGuard
	}
	return resumeInterruptedPairing(
		paths,
		cfg,
		lifecycleGeneration,
		configSnapshot,
		hadConfigSnapshot,
		resume,
	)
}

func resumeInterruptedPairing(
	paths runtimePaths,
	cfg *runtimeConfig,
	lifecycleGeneration []byte,
	configSnapshot []byte,
	hadConfigSnapshot bool,
	resume func(
		runtimePaths,
		*runtimeConfig,
		func(*runtimeConfig) error,
	) (bool, error),
) (bool, error) {
	if cfg == nil ||
		cfg.PendingSecureBaseURL == "" ||
		cfg.PendingSpkiPin == "" {
		return false, nil
	}
	resumed := false
	err := withClientMutationLock(paths, func() error {
		if err := ensureUpdateLifecycleCurrent(
			paths,
			lifecycleGeneration,
		); err != nil {
			return err
		}
		if err := ensureOptionalFileSnapshotCurrent(
			paths.ConfigFile,
			configSnapshot,
			hadConfigSnapshot,
		); err != nil {
			return err
		}
		var err error
		resumed, err = resume(
			paths,
			cfg,
			func(value *runtimeConfig) error {
				return saveConfig(paths, *value)
			},
		)
		return err
	})
	return resumed, err
}
