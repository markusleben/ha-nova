package main

import (
	"context"
	"strings"
	"sync"
)

type cloudSecretAccessContextKey struct{}
type cloudSecretAccessHolderContextKey struct{}

type cloudSecretAccessSession struct {
	profileID string
	store     OAuthSecretStore
}

type cloudSecretAccessHolder struct {
	mu      sync.Mutex
	session *cloudSecretAccessSession
}

type memoizedOAuthSecretBackend struct {
	mu           sync.Mutex
	backend      OAuthSecretBackend
	entries      map[string]memoizedOAuthSecretEntry
	cacheEnabled bool
}

type memoizedOAuthSecretEntry struct {
	value  string
	exists bool
}

func installCloudSecretAccessSession(
	ctx context.Context,
	profileID string,
	sessionStore OAuthSecretStore,
) context.Context {
	session := &cloudSecretAccessSession{
		profileID: profileID,
		store:     sessionStore,
	}
	if holder, ok := ctx.Value(
		cloudSecretAccessHolderContextKey{},
	).(*cloudSecretAccessHolder); ok && holder != nil {
		holder.mu.Lock()
		holder.session = session
		holder.mu.Unlock()
		return ctx
	}
	return context.WithValue(
		ctx,
		cloudSecretAccessContextKey{},
		session,
	)
}

func withCloudSecretAccessHolder(ctx context.Context) context.Context {
	if _, ok := ctx.Value(
		cloudSecretAccessHolderContextKey{},
	).(*cloudSecretAccessHolder); ok {
		return ctx
	}
	return context.WithValue(
		ctx,
		cloudSecretAccessHolderContextKey{},
		&cloudSecretAccessHolder{},
	)
}

func cloudSecretStoreForOperation(
	ctx context.Context,
	profileID string,
) (OAuthSecretStore, bool) {
	session, _ := ctx.Value(
		cloudSecretAccessContextKey{},
	).(*cloudSecretAccessSession)
	if session == nil {
		if holder, ok := ctx.Value(
			cloudSecretAccessHolderContextKey{},
		).(*cloudSecretAccessHolder); ok && holder != nil {
			holder.mu.Lock()
			session = holder.session
			holder.mu.Unlock()
		}
	}
	if session == nil ||
		session.profileID != profileID ||
		session.store == nil {
		return nil, false
	}
	return session.store, true
}

func stageOAuthSecretStoreMemoization(
	store OAuthSecretStore,
) (OAuthSecretStore, func()) {
	keyringStore, ok := store.(*KeyringOAuthSecretStore)
	if !ok || keyringStore == nil || keyringStore.backend == nil {
		return store, func() {}
	}
	clone := *keyringStore
	backend := &memoizedOAuthSecretBackend{
		backend: keyringStore.backend,
		entries: make(map[string]memoizedOAuthSecretEntry),
	}
	clone.backend = backend
	return &clone, backend.enableCache
}

func (b *memoizedOAuthSecretBackend) enableCache() {
	b.mu.Lock()
	b.cacheEnabled = true
	b.mu.Unlock()
}

func memoizableOAuthSecretService(service string) bool {
	return !strings.HasPrefix(service, oauthSecretPreflightServicePrefix)
}

func (b *memoizedOAuthSecretBackend) Get(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) (string, bool, error) {
	if err := validateOAuthSecretBackendKey(ctx, service, account, ui); err != nil {
		return "", false, err
	}
	key := service + "\x00" + account
	cacheable := memoizableOAuthSecretService(service)
	if cacheable {
		b.mu.Lock()
		entry, cached := b.entries[key]
		cacheEnabled := b.cacheEnabled
		b.mu.Unlock()
		if cacheEnabled && cached {
			return entry.value, entry.exists, nil
		}
	}
	value, exists, err := b.backend.Get(ctx, service, account, ui)
	if err != nil {
		return "", false, err
	}
	if cacheable {
		b.mu.Lock()
		b.entries[key] = memoizedOAuthSecretEntry{
			value:  value,
			exists: exists,
		}
		b.mu.Unlock()
	}
	return value, exists, nil
}

func (b *memoizedOAuthSecretBackend) Set(
	ctx context.Context,
	service, account, value string,
	ui SecretStoreUIPolicy,
) error {
	if err := b.backend.Set(ctx, service, account, value, ui); err != nil {
		return err
	}
	if !memoizableOAuthSecretService(service) {
		return nil
	}
	b.mu.Lock()
	b.entries[service+"\x00"+account] = memoizedOAuthSecretEntry{
		value:  value,
		exists: true,
	}
	b.mu.Unlock()
	return nil
}

func (b *memoizedOAuthSecretBackend) Delete(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) error {
	if err := b.backend.Delete(ctx, service, account, ui); err != nil {
		return err
	}
	if !memoizableOAuthSecretService(service) {
		return nil
	}
	b.mu.Lock()
	b.entries[service+"\x00"+account] = memoizedOAuthSecretEntry{}
	b.mu.Unlock()
	return nil
}
