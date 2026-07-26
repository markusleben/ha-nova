package main

import "context"

type exactOAuthCleanupStore interface {
	DeleteCurrentExact(
		context.Context,
		OAuthSecretEnvelope,
		SecretStoreUIPolicy,
	) error
	DeletePendingExact(
		context.Context,
		OAuthSecretEnvelope,
		SecretStoreUIPolicy,
	) error
	DeleteRetiringExact(
		context.Context,
		OAuthSecretEnvelope,
		SecretStoreUIPolicy,
	) error
}

func (s *KeyringOAuthSecretStore) freshStore() *KeyringOAuthSecretStore {
	clone := *s
	for {
		memoized, ok := clone.backend.(*memoizedOAuthSecretBackend)
		if !ok || memoized == nil {
			return &clone
		}
		clone.backend = memoized.backend
	}
}

func (s *KeyringOAuthSecretStore) DeleteCurrentExact(
	ctx context.Context,
	expected OAuthSecretEnvelope,
	ui SecretStoreUIPolicy,
) error {
	fresh := s.freshStore()
	actual, exists, err := fresh.LoadCurrent(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !sameOAuthSecretEnvelope(actual, expected) {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact current OAuth secret",
			nil,
		)
	}
	return fresh.DeleteCurrent(ctx, ui)
}

func (s *KeyringOAuthSecretStore) DeletePendingExact(
	ctx context.Context,
	expected OAuthSecretEnvelope,
	ui SecretStoreUIPolicy,
) error {
	fresh := s.freshStore()
	actual, exists, err := fresh.LoadPending(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !sameOAuthSecretEnvelope(actual, expected) {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact pending OAuth secret",
			nil,
		)
	}
	return fresh.DeletePending(ctx, expected.Generation, ui)
}

func (s *KeyringOAuthSecretStore) DeleteRetiringExact(
	ctx context.Context,
	expected OAuthSecretEnvelope,
	ui SecretStoreUIPolicy,
) error {
	fresh := s.freshStore()
	actual, exists, err := fresh.LoadRetiring(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !sameOAuthSecretEnvelope(actual, expected) {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact retiring OAuth secret",
			nil,
		)
	}
	return fresh.RevokeRetiring(
		ctx,
		expected.Generation,
		ui,
		func(
			context.Context,
			OAuthSecretEnvelope,
		) error {
			return nil
		},
	)
}

func exactOAuthCleanupStoreFor(
	store OAuthSecretStore,
) (exactOAuthCleanupStore, error) {
	exact, ok := store.(exactOAuthCleanupStore)
	if !ok {
		return nil, newCloudError(
			CloudErrSecretStore,
			"initialize exact OAuth cleanup",
			nil,
		)
	}
	return exact, nil
}
