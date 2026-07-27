package main

import "context"

type cloudSelectedSecretSessionPreflighter interface {
	PreflightSelectedSecretSession(
		context.Context,
		runtimeConfig,
		cloudSecretPreflightMode,
	) (context.Context, error)
}

func preflightCloudSecretAccessSession(
	ctx context.Context,
	coordinator cloudSetupCoordinator,
	cfg runtimeConfig,
	mode cloudSecretPreflightMode,
) (context.Context, error) {
	if selected, ok := coordinator.(cloudSelectedSecretSessionPreflighter); ok {
		return selected.PreflightSelectedSecretSession(
			ctx,
			cfg,
			mode,
		)
	}
	return ctx, preflightCloudSecretAccess(
		ctx,
		coordinator,
		cfg,
		mode,
	)
}

func (productionCloudSetupCoordinator) PreflightSelectedSecret(
	ctx context.Context,
	cfg runtimeConfig,
	mode cloudSecretPreflightMode,
) error {
	_, err := productionCloudSetupCoordinator{}.
		PreflightSelectedSecretSession(ctx, cfg, mode)
	return err
}

func (productionCloudSetupCoordinator) PreflightSelectedSecretSession(
	ctx context.Context,
	cfg runtimeConfig,
	mode cloudSecretPreflightMode,
) (context.Context, error) {
	if !cloudInteractivePromptSessionForSetup() {
		return ctx, newCloudError(
			CloudErrUnsupportedPlatform,
			"prepare Cloud authorization",
			nil,
		)
	}
	store, err := newCloudSecretStoreForCLI(cfg.ProfileID)
	if err != nil {
		return ctx, err
	}
	sessionStore, activateCache := stageOAuthSecretStoreMemoization(store)
	err = preflightSelectedCloudSecret(ctx, sessionStore, cfg, mode)
	if err != nil {
		return ctx, err
	}
	activateCache()
	return installCloudSecretAccessSession(
		ctx,
		cfg.ProfileID,
		sessionStore,
	), nil
}
