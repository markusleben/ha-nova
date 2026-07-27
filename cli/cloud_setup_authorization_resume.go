package main

import "context"

func resumableCloudEnvelope(
	ctx context.Context,
	store OAuthSecretStore,
	cfg runtimeConfig,
	origin CloudOrigin,
	ui SecretStoreUIPolicy,
) (OAuthSecretEnvelope, bool, error) {
	pending, hasPending, err := store.LoadPending(ctx, ui)
	if err != nil {
		return OAuthSecretEnvelope{}, false, err
	}
	if hasPending {
		if cfg.Cloud == nil || cfg.Cloud.Pending == nil {
			return OAuthSecretEnvelope{}, false, cloudResumeIdentityError()
		}
		if err := validateResumableCloudBinding(
			cfg,
			*cfg.Cloud.Pending,
			pending,
			origin,
		); err != nil {
			return OAuthSecretEnvelope{}, false, err
		}
		return pending, false, nil
	}
	current, hasCurrent, err := store.LoadCurrent(ctx, ui)
	if err != nil {
		return OAuthSecretEnvelope{}, false, err
	}
	if hasCurrent && cfg.Cloud != nil && cfg.Cloud.Pending != nil &&
		cfg.Cloud.State == cloudStateDeviceBoundOrPaired {
		if err := validateResumableCloudBinding(
			cfg,
			*cfg.Cloud.Pending,
			current,
			origin,
		); err != nil {
			return OAuthSecretEnvelope{}, false, err
		}
		return current, true, nil
	}
	if cfg.Cloud != nil && cfg.Cloud.Pending != nil &&
		cfg.Cloud.State != cloudStateAuthorizing {
		return OAuthSecretEnvelope{}, false, newCloudError(
			CloudErrSecretNotFound,
			"resume Cloud authorization",
			nil,
		)
	}
	return OAuthSecretEnvelope{}, false, nil
}

func validateResumableCloudBinding(
	cfg runtimeConfig,
	metadata cloudConnectionMetadata,
	envelope OAuthSecretEnvelope,
	origin CloudOrigin,
) error {
	if cfg.ProfileID == "" ||
		envelope.ProfileID != cfg.ProfileID ||
		metadata.Origin != origin.InputOrigin ||
		metadata.CanonicalOrigin != origin.CanonicalOrigin ||
		envelope.CanonicalOrigin != origin.CanonicalOrigin ||
		(metadata.HAUserID != "" && envelope.HAUserID != metadata.HAUserID) {
		return cloudResumeIdentityError()
	}
	if cfg.RelayInstanceID != "" &&
		envelope.RelayInstanceID != "" &&
		envelope.RelayInstanceID != cfg.RelayInstanceID {
		return newCloudError(
			CloudErrRelayInstance,
			"resume Cloud authorization",
			nil,
		)
	}
	if metadata.CredentialGeneration != envelope.Generation ||
		metadata.OAuthClientID != envelope.ClientID {
		return newCloudError(
			CloudErrSecretConflict,
			"resume Cloud authorization",
			nil,
		)
	}
	return nil
}

func cloudResumeIdentityError() error {
	return newCloudError(
		CloudErrIdentityMismatch,
		"resume Cloud authorization",
		nil,
	)
}

func promoteCloudAuthorization(
	ctx context.Context,
	store OAuthSecretStore,
	generation string,
	verified OAuthSecretEnvelope,
) (OAuthSecretEnvelope, error) {
	if verified.Generation != generation {
		return OAuthSecretEnvelope{}, newCloudError(
			CloudErrSecretConflict,
			"promote Cloud authorization",
			nil,
		)
	}
	current, exists, err := store.LoadCurrent(ctx, SecretStoreForbidUI)
	if err != nil {
		return OAuthSecretEnvelope{}, err
	}
	if exists && current.Generation == generation {
		pending, pendingExists, err := store.LoadPending(
			ctx,
			SecretStoreForbidUI,
		)
		if err != nil {
			return OAuthSecretEnvelope{}, err
		}
		if !pendingExists {
			return current, nil
		}
		if pending.Generation != generation {
			return OAuthSecretEnvelope{}, newCloudError(
				CloudErrSecretConflict,
				"promote Cloud authorization",
				nil,
			)
		}
		// Resume the store transaction so an interrupted pending deletion is
		// completed. Returning the matching current without this step leaves an
		// orphan pending slot that blocks every later authorization rotation.
		return store.PromotePending(
			ctx,
			generation,
			SecretStoreForbidUI,
		)
	}
	return store.PromotePending(ctx, generation, SecretStoreForbidUI)
}

func normalizedVerifiedEnvelope(
	envelope OAuthSecretEnvelope,
	current bool,
) OAuthSecretEnvelope {
	if current {
		envelope.State = OAuthSecretCurrent
	} else {
		envelope.State = OAuthSecretPending
	}
	return envelope
}

func cloudMetadataFromEnvelope(
	origin CloudOrigin,
	envelope OAuthSecretEnvelope,
) cloudConnectionMetadata {
	return cloudConnectionMetadata{
		Origin:               origin.InputOrigin,
		CanonicalOrigin:      envelope.CanonicalOrigin,
		OAuthClientID:        envelope.ClientID,
		CredentialGeneration: envelope.Generation,
		HAUserID:             envelope.HAUserID,
	}
}
