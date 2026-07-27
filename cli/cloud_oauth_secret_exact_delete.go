package main

import (
	"context"
	"encoding/json"
)

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
		string,
		SecretStoreUIPolicy,
	) error
	DeleteRetiringExact(
		context.Context,
		OAuthSecretEnvelope,
		SecretStoreUIPolicy,
	) error
}

type exactOAuthSecretBackend interface {
	DeleteExact(
		context.Context,
		string,
		string,
		string,
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
	canonicalOrigin string,
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
		return s.confirmEnvelopeAbsentExact(
			ctx,
			oauthSecretPendingService,
			ui,
			"delete exact pending OAuth grant",
		)
	}
	if actual.Generation != generation ||
		actual.CanonicalOrigin != canonicalOrigin ||
		actual.RefreshToken != refreshToken ||
		actual.ClientID != clientID {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact pending OAuth grant",
			nil,
		)
	}
	return s.deleteEnvelopeExact(
		ctx,
		oauthSecretPendingService,
		actual,
		ui,
		"delete exact pending OAuth grant",
	)
}

func (s *KeyringOAuthSecretStore) confirmEnvelopeAbsentExact(
	ctx context.Context,
	service string,
	ui SecretStoreUIPolicy,
	operation string,
) error {
	backend := s.freshStore().backend
	_, exists, err := backend.Get(
		ctx,
		service,
		s.account,
		ui,
	)
	if err != nil {
		return wrapOAuthSecretBackendError(operation, err)
	}
	if exists {
		return newCloudError(
			CloudErrSecretConflict,
			operation,
			nil,
		)
	}
	s.markMemoizedDeleted(service)
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
		return s.deleteEnvelopeExact(
			ctx,
			oauthSecretCurrentService,
			expected,
			ui,
			"delete exact current OAuth secret",
		)
	}
	if !sameOAuthSecretEnvelope(actual, expected) {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact current OAuth secret",
			nil,
		)
	}
	return s.deleteEnvelopeExact(
		ctx,
		oauthSecretCurrentService,
		actual,
		ui,
		"delete exact current OAuth secret",
	)
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
		return s.deleteEnvelopeExact(
			ctx,
			oauthSecretPendingService,
			expected,
			ui,
			"delete exact pending OAuth secret",
		)
	}
	if !sameOAuthSecretEnvelope(actual, expected) {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact pending OAuth secret",
			nil,
		)
	}
	return s.deleteEnvelopeExact(
		ctx,
		oauthSecretPendingService,
		actual,
		ui,
		"delete exact pending OAuth secret",
	)
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
		return s.deleteEnvelopeExact(
			ctx,
			oauthSecretRetiringService,
			expected,
			ui,
			"delete exact retiring OAuth secret",
		)
	}
	if !sameOAuthSecretEnvelope(actual, expected) {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact retiring OAuth secret",
			nil,
		)
	}
	return s.deleteEnvelopeExact(
		ctx,
		oauthSecretRetiringService,
		actual,
		ui,
		"delete exact retiring OAuth secret",
	)
}

func (s *KeyringOAuthSecretStore) deleteEnvelopeExact(
	ctx context.Context,
	service string,
	expected OAuthSecretEnvelope,
	ui SecretStoreUIPolicy,
	operation string,
) error {
	encoded, err := json.Marshal(expected)
	if err != nil {
		return newCloudError(
			CloudErrSecretStore,
			operation,
			err,
		)
	}
	backend := s.freshStore().backend
	exact, ok := backend.(exactOAuthSecretBackend)
	if !ok {
		return newCloudError(
			CloudErrSecretStore,
			operation,
			nil,
		)
	}
	if err := exact.DeleteExact(
		ctx,
		service,
		s.account,
		string(encoded),
		ui,
	); err != nil {
		return wrapOAuthSecretBackendError(operation, err)
	}
	s.markMemoizedDeleted(service)
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
