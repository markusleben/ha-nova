package main

func resumeInterruptedPairingForDoctor(
	paths runtimePaths,
	cfg *runtimeConfig,
	lifecycleGeneration []byte,
	configSnapshot []byte,
	hadConfigSnapshot bool,
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
		resumed, err = resumePendingActivationAfterRetirementGuard(
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
