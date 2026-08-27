package main

import "context"

type cloudSecretPreflightMode uint8

const (
	cloudSecretPreflightSetup cloudSecretPreflightMode = iota
	cloudSecretPreflightUnlock
)

type cloudSelectedSecretPreflighter interface {
	PreflightSelectedSecret(
		context.Context,
		runtimeConfig,
		cloudSecretPreflightMode,
	) error
}

func preflightCloudSecretAccess(
	ctx context.Context,
	coordinator cloudSetupCoordinator,
	cfg runtimeConfig,
	mode cloudSecretPreflightMode,
) error {
	if selected, ok := coordinator.(cloudSelectedSecretPreflighter); ok {
		return selected.PreflightSelectedSecret(ctx, cfg, mode)
	}
	return coordinator.Preflight(ctx, cfg.ProfileID)
}

func preflightSelectedCloudSecret(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
	mode cloudSecretPreflightMode,
) error {
	if cfg.Cloud != nil &&
		cfg.Cloud.Pending != nil &&
		cfg.Cloud.State != cloudStateCommitted {
		return preflightPendingCloudSecret(ctx, store, cfg)
	}
	slot := selectedCloudPreflightSlot(cfg, mode)
	switch slot {
	case OAuthSecretCurrent:
		return preflightCloudSecretStep(func(ui SecretStoreUIPolicy) error {
			envelope, exists, err := store.LoadCurrent(ctx, ui)
			if err != nil {
				return err
			}
			if !exists || cfg.Cloud == nil || cfg.Cloud.Current == nil {
				return newCloudError(
					CloudErrSecretNotFound,
					"unlock current Cloud authorization",
					nil,
				)
			}
			return matchCloudConfigToSecret(
				cfg,
				*cfg.Cloud.Current,
				envelope,
			)
		})
	case OAuthSecretPending:
		return preflightCloudSecretStep(func(ui SecretStoreUIPolicy) error {
			envelope, exists, err := store.LoadPending(ctx, ui)
			if err != nil {
				return err
			}
			if !exists || cfg.Cloud == nil || cfg.Cloud.Pending == nil {
				return newCloudError(
					CloudErrSecretNotFound,
					"unlock pending Cloud authorization",
					nil,
				)
			}
			return validatePendingCloudPreflight(cfg, envelope)
		})
	case OAuthSecretRetiring:
		return preflightRetiringOrCurrentCloudSecret(ctx, store, cfg)
	default:
		return PreflightOAuthSecretStore(ctx, store, secretStoreUIPolicyForSetup(SecretStoreAllowUI))
	}
}

func preflightRetiringOrCurrentCloudSecret(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
) error {
	if err := preflightCloudSecretStep(
		func(ui SecretStoreUIPolicy) error {
			retiring, exists, err := store.LoadRetiring(ctx, ui)
			if err != nil || !exists {
				return err
			}
			return matchRetiringCloudSecret(cfg, retiring)
		},
	); err != nil {
		return err
	}
	return preflightCloudSecretStep(
		func(ui SecretStoreUIPolicy) error {
			return preflightCurrentCloudSecret(ctx, store, cfg, ui)
		},
	)
}

func preflightCurrentCloudSecret(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
	ui SecretStoreUIPolicy,
) error {
	if cfg.Cloud == nil || cfg.Cloud.Current == nil {
		return newCloudError(
			CloudErrSecretNotFound,
			"unlock current Cloud authorization",
			nil,
		)
	}
	current, exists, err := store.LoadCurrent(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		return newCloudError(
			CloudErrSecretNotFound,
			"unlock current Cloud authorization",
			nil,
		)
	}
	return matchCloudConfigToSecret(cfg, *cfg.Cloud.Current, current)
}

func preflightPendingCloudSecret(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
) error {
	pending, exists, err := store.LoadPending(ctx, SecretStoreForbidUI)
	switch {
	case err == nil && exists:
		if err := validatePendingCloudPreflight(cfg, pending); err != nil {
			return err
		}
		return preflightPromotionDependencies(ctx, store, cfg)
	case err == nil:
		return preflightCloudSecretAfterMissingPending(
			ctx,
			store,
			cfg,
		)
	case IsCloudErrorCode(err, CloudErrSecretUIForbidden),
		IsCloudErrorCode(err, CloudErrSecretStoreLocked):
		pending, exists, err = store.LoadPending(ctx, secretStoreUIPolicyForSetup(SecretStoreAllowUI))
		if err != nil {
			return err
		}
		if exists {
			if err := validatePendingCloudPreflight(cfg, pending); err != nil {
				return err
			}
			return preflightPromotionDependencies(ctx, store, cfg)
		}
		// The pending-slot prompt may reveal that promotion already deleted it.
		// Continue with the finite fallback slots; each independently gets one
		// no-UI attempt and at most one interactive retry.
		return preflightCloudSecretAfterMissingPending(
			ctx,
			store,
			cfg,
		)
	default:
		return err
	}
}

func preflightPromotionDependencies(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
) error {
	if err := preflightCloudSecretStep(
		func(ui SecretStoreUIPolicy) error {
			return preflightCurrentForPendingLifecycle(
				ctx,
				store,
				cfg,
				ui,
			)
		},
	); err != nil {
		return err
	}
	return preflightCloudSecretStep(
		func(ui SecretStoreUIPolicy) error {
			retiring, exists, err := store.LoadRetiring(ctx, ui)
			if err != nil || !exists {
				return err
			}
			return matchRetiringCloudSecret(cfg, retiring)
		},
	)
}

func preflightCurrentForPendingLifecycle(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
	ui SecretStoreUIPolicy,
) error {
	current, exists, err := store.LoadCurrent(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		if cfg.Cloud != nil && cfg.Cloud.Current == nil {
			return nil
		}
		return newCloudError(
			CloudErrSecretNotFound,
			"unlock current Cloud authorization",
			nil,
		)
	}
	if cfg.Cloud != nil && cfg.Cloud.Current != nil {
		if err := matchCloudConfigToSecret(
			cfg,
			*cfg.Cloud.Current,
			current,
		); err == nil {
			return nil
		}
	}
	return validatePendingCloudPreflight(cfg, current)
}

func preflightCloudSecretStep(
	step func(SecretStoreUIPolicy) error,
) error {
	err := step(SecretStoreForbidUI)
	if err == nil ||
		(!IsCloudErrorCode(err, CloudErrSecretUIForbidden) &&
			!IsCloudErrorCode(err, CloudErrSecretStoreLocked)) {
		return err
	}
	return step(secretStoreUIPolicyForSetup(SecretStoreAllowUI))
}

func preflightCloudSecretAfterMissingPending(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
) error {
	if cfg.Cloud != nil &&
		cfg.Cloud.State == cloudStateDeviceBoundOrPaired {
		return preflightPromotedCurrentAndRetiringSecret(
			ctx,
			store,
			cfg,
		)
	}
	if cfg.Cloud != nil && cfg.Cloud.Current != nil {
		return preflightCloudSecretStep(
			func(ui SecretStoreUIPolicy) error {
				return preflightCurrentCloudSecret(ctx, store, cfg, ui)
			},
		)
	}
	return PreflightOAuthSecretStore(ctx, store, secretStoreUIPolicyForSetup(SecretStoreAllowUI))
}

func validatePendingCloudPreflight(
	cfg runtimeConfig,
	envelope OAuthSecretEnvelope,
) error {
	origin, err := cloudOriginFromMetadata(*cfg.Cloud.Pending)
	if err != nil {
		return err
	}
	return validateResumableCloudBinding(
		cfg,
		*cfg.Cloud.Pending,
		envelope,
		origin,
	)
}

func preflightPromotedCurrentAndRetiringSecret(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
) error {
	if err := preflightCloudSecretStep(
		func(ui SecretStoreUIPolicy) error {
			return preflightPromotedCurrentSecret(ctx, store, cfg, ui)
		},
	); err != nil {
		return err
	}
	return preflightCloudSecretStep(
		func(ui SecretStoreUIPolicy) error {
			retiring, exists, err := store.LoadRetiring(ctx, ui)
			if err != nil || !exists {
				return err
			}
			return matchRetiringCloudSecret(cfg, retiring)
		},
	)
}

func preflightPromotedCurrentSecret(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
	ui SecretStoreUIPolicy,
) error {
	current, exists, err := store.LoadCurrent(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		return newCloudError(
			CloudErrSecretNotFound,
			"unlock promoted Cloud authorization",
			nil,
		)
	}
	return validatePendingCloudPreflight(cfg, current)
}

func selectedCloudPreflightSlot(
	cfg runtimeConfig,
	mode cloudSecretPreflightMode,
) OAuthSecretState {
	if cfg.Cloud == nil {
		return ""
	}
	if cfg.Cloud.State == cloudStateCommitted ||
		cfg.Cloud.State == cloudStateRetiringPrevious {
		return OAuthSecretRetiring
	}
	if cfg.Cloud.Pending != nil {
		return OAuthSecretPending
	}
	if cfg.Cloud.Current != nil {
		return OAuthSecretCurrent
	}
	return ""
}

func matchRetiringCloudSecret(
	cfg runtimeConfig,
	envelope OAuthSecretEnvelope,
) error {
	// Retiring is the previous, independently validated authorization. A
	// legitimate reconnect may change HA user or remote URL, so neither can be
	// compared with the new Current metadata after the commit checkpoint. The
	// native store account and durable Relay identity are the available stable
	// bindings.
	if cfg.Cloud == nil || cfg.Cloud.Current == nil ||
		envelope.ProfileID != cfg.ProfileID {
		return newCloudError(
			CloudErrIdentityMismatch,
			"match retiring Cloud authorization",
			nil,
		)
	}
	if envelope.RelayInstanceID != cfg.RelayInstanceID {
		return newCloudError(
			CloudErrRelayInstance,
			"match retiring Cloud authorization",
			nil,
		)
	}
	return nil
}
