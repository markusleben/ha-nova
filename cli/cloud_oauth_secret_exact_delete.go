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
	DeletePendingGrantExact(
		context.Context,
		string,
		string,
		string,
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

func (s *KeyringOAuthSecretStore) markMemoizedDeleted(
	service string,
) {
	for backend := s.backend; ; {
		memoized, ok := backend.(*memoizedOAuthSecretBackend)
		if !ok || memoized == nil {
			return
		}
		memoized.mu.Lock()
		memoized.entries[service+"\x00"+s.account] =
			memoizedOAuthSecretEntry{}
		memoized.mu.Unlock()
		backend = memoized.backend
	}
}

func (s *KeyringOAuthSecretStore) DeletePendingGrantExact(
	ctx context.Context,
	generation string,
	refreshToken string,
	clientID string,
	ui SecretStoreUIPolicy,
) error {
	fresh := s.freshStore()
	actual, exists, err := fresh.LoadPending(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		s.markMemoizedDeleted(oauthSecretPendingService)
		return nil
	}
	if actual.Generation != generation ||
		actual.RefreshToken != refreshToken ||
		actual.ClientID != clientID {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact pending OAuth grant",
			nil,
		)
	}
	if err := fresh.backend.Delete(
		ctx,
		oauthSecretPendingService,
		fresh.account,
		ui,
	); err != nil {
		return wrapOAuthSecretBackendError(
			"delete exact pending OAuth grant",
			err,
		)
	}
	s.markMemoizedDeleted(oauthSecretPendingService)
	return nil
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
		s.markMemoizedDeleted(oauthSecretCurrentService)
		return nil
	}
	if !sameOAuthSecretEnvelope(actual, expected) {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact current OAuth secret",
			nil,
		)
	}
	if err := fresh.backend.Delete(
		ctx,
		oauthSecretCurrentService,
		fresh.account,
		ui,
	); err != nil {
		return wrapOAuthSecretBackendError(
			"delete exact current OAuth secret",
			err,
		)
	}
	s.markMemoizedDeleted(oauthSecretCurrentService)
	return nil
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
		s.markMemoizedDeleted(oauthSecretPendingService)
		return nil
	}
	if !sameOAuthSecretEnvelope(actual, expected) {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact pending OAuth secret",
			nil,
		)
	}
	if err := fresh.backend.Delete(
		ctx,
		oauthSecretPendingService,
		fresh.account,
		ui,
	); err != nil {
		return wrapOAuthSecretBackendError(
			"delete exact pending OAuth secret",
			err,
		)
	}
	s.markMemoizedDeleted(oauthSecretPendingService)
	return nil
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
		s.markMemoizedDeleted(oauthSecretRetiringService)
		return nil
	}
	if !sameOAuthSecretEnvelope(actual, expected) {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact retiring OAuth secret",
			nil,
		)
	}
	if err := fresh.backend.Delete(
		ctx,
		oauthSecretRetiringService,
		fresh.account,
		ui,
	); err != nil {
		return wrapOAuthSecretBackendError(
			"delete exact retiring OAuth secret",
			err,
		)
	}
	s.markMemoizedDeleted(oauthSecretRetiringService)
	return nil
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
