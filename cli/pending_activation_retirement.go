package main

import "strings"

// Test seam for the activation reached only through the retirement guard.
var resumePendingActivationAfterRetirementCheck = resumePendingActivation
var resumePendingActivationForExplicitPairAfterRetirementCheck = func(
	cfg *runtimeConfig,
	save func(*runtimeConfig) error,
) (bool, error) {
	return resumePendingActivationWithPolicy(
		cfg,
		save,
		explicitPairingSecretStoreUIPolicy(),
	)
}

func resumeSetupPendingActivation(
	paths runtimePaths,
	cfg *runtimeConfig,
	lifecycleMarker ...[]byte,
) (bool, error) {
	return resumeSetupPendingActivationWithGuard(
		paths,
		cfg,
		resumePendingActivationAfterRetirementGuard,
		lifecycleMarker...,
	)
}

func resumeInteractiveSetupPendingActivation(
	paths runtimePaths,
	cfg *runtimeConfig,
	lifecycleMarker ...[]byte,
) (bool, error) {
	return resumeSetupPendingActivationWithGuard(
		paths,
		cfg,
		resumePendingActivationForExplicitPairAfterRetirementGuard,
		lifecycleMarker...,
	)
}

func resumeSetupPendingActivationWithGuard(
	paths runtimePaths,
	cfg *runtimeConfig,
	resume func(
		runtimePaths,
		*runtimeConfig,
		func(*runtimeConfig) error,
	) (bool, error),
	lifecycleMarker ...[]byte,
) (bool, error) {
	if cfg == nil ||
		strings.TrimSpace(cfg.PendingSecureBaseURL) == "" ||
		strings.TrimSpace(cfg.PendingSpkiPin) == "" {
		return false, nil
	}
	resumed := false
	err := withSetupLifecycleLock(paths, lifecycleMarker, func() error {
		var err error
		resumed, err = resume(
			paths,
			cfg,
			func(value *runtimeConfig) error {
				return saveSetupConfigWithLifecycleUnlocked(
					paths,
					*value,
					lifecycleMarker...,
				)
			},
		)
		return err
	})
	return resumed, err
}

func printPendingActivationResumeError(err error) {
	printHumanErr(
		"cannot resume the interrupted device activation: %s",
		err,
	)
	if hint := setupSecureStorageRecoveryHint(err); hint != "" {
		printHumanWarn("%s", hint)
	}
}

func resumePendingActivationAfterRetirementGuard(
	paths runtimePaths,
	cfg *runtimeConfig,
	save func(*runtimeConfig) error,
) (bool, error) {
	if cfg == nil ||
		strings.TrimSpace(cfg.PendingSecureBaseURL) == "" ||
		strings.TrimSpace(cfg.PendingSpkiPin) == "" {
		return false, nil
	}
	// A durable retirement checkpoint owns the exact credential/endpoints.
	// Never activate or promote a sibling pending slot until setup/purge has
	// settled that transaction.
	if err := requireSettledDeviceCredentialRetirement(
		paths,
		activeServerProfile(),
	); err != nil {
		return false, err
	}
	return resumePendingActivationAfterRetirementCheck(cfg, save)
}

func resumePendingActivationForExplicitPairAfterRetirementGuard(
	paths runtimePaths,
	cfg *runtimeConfig,
	save func(*runtimeConfig) error,
) (bool, error) {
	if cfg == nil ||
		strings.TrimSpace(cfg.PendingSecureBaseURL) == "" ||
		strings.TrimSpace(cfg.PendingSpkiPin) == "" {
		return false, nil
	}
	if err := requireSettledDeviceCredentialRetirement(
		paths,
		activeServerProfile(),
	); err != nil {
		return false, err
	}
	return resumePendingActivationForExplicitPairAfterRetirementCheck(
		cfg,
		save,
	)
}
