package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryOAuthSecretBackend struct {
	mu         sync.Mutex
	values     map[string]string
	policies   []SecretStoreUIPolicy
	operations []string
	fail       func(op, service string) error
	dropSet    func(service string) bool
}

func newMemoryOAuthSecretBackend() *memoryOAuthSecretBackend {
	return &memoryOAuthSecretBackend{values: make(map[string]string)}
}

func (b *memoryOAuthSecretBackend) Get(_ context.Context, service, account string, ui SecretStoreUIPolicy) (string, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.policies = append(b.policies, ui)
	b.operations = append(b.operations, "get")
	if b.fail != nil {
		if err := b.fail("get", service); err != nil {
			return "", false, err
		}
	}
	value, ok := b.values[service+"\x00"+account]
	return value, ok, nil
}

func (b *memoryOAuthSecretBackend) Set(_ context.Context, service, account, value string, ui SecretStoreUIPolicy) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.policies = append(b.policies, ui)
	b.operations = append(b.operations, "set")
	if b.fail != nil {
		if err := b.fail("set", service); err != nil {
			return err
		}
	}
	if b.dropSet == nil || !b.dropSet(service) {
		b.values[service+"\x00"+account] = value
	}
	return nil
}

func (b *memoryOAuthSecretBackend) Delete(_ context.Context, service, account string, ui SecretStoreUIPolicy) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.policies = append(b.policies, ui)
	b.operations = append(b.operations, "delete")
	if b.fail != nil {
		if err := b.fail("delete", service); err != nil {
			return err
		}
	}
	delete(b.values, service+"\x00"+account)
	return nil
}

func testOAuthEnvelope(token, tokenID, userID, relayID string) OAuthSecretEnvelope {
	expires := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	return OAuthSecretEnvelope{
		CanonicalOrigin:       "https://unit.ui.nabu.casa",
		ClientID:              "http://127.0.0.1:43123/ha-nova",
		RefreshToken:          token,
		RefreshTokenID:        tokenID,
		RefreshTokenExpiresAt: &expires,
		HAUserID:              userID,
		RelayInstanceID:       relayID,
	}
}

func newTestOAuthStore(t *testing.T, backend *memoryOAuthSecretBackend) *KeyringOAuthSecretStore {
	t.Helper()
	store, err := NewOAuthSecretStore(backend, "default")
	if err != nil {
		t.Fatalf("NewOAuthSecretStore: %v", err)
	}
	store.now = func() time.Time { return time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC) }
	return store
}

func establishOAuthCurrent(t *testing.T, store *KeyringOAuthSecretStore, token string) OAuthSecretEnvelope {
	t.Helper()
	pending, err := store.CreatePending(
		context.Background(),
		testOAuthEnvelope(token, "token-"+token, "user-1", "relay-1"),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	current, err := store.PromotePending(context.Background(), pending.Generation, SecretStoreAllowUI)
	if err != nil {
		t.Fatalf("PromotePending: %v", err)
	}
	return current
}

func TestOAuthSecretStorePromotionPreservesAndRevokesPrevious(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	old := establishOAuthCurrent(t, store, "old-refresh")

	pending, err := store.CreatePending(
		context.Background(),
		testOAuthEnvelope("new-refresh", "token-new", "user-1", "relay-1"),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatalf("CreatePending new: %v", err)
	}
	current, err := store.PromotePending(context.Background(), pending.Generation, SecretStoreAllowUI)
	if err != nil {
		t.Fatalf("PromotePending new: %v", err)
	}
	if current.RefreshToken != "new-refresh" {
		t.Fatalf("current token = %q", current.RefreshToken)
	}
	retiring, ok, err := store.LoadRetiring(context.Background(), SecretStoreForbidUI)
	if err != nil || !ok || retiring.Generation != old.Generation || retiring.RefreshToken != "old-refresh" {
		t.Fatalf("retiring old token: ok=%v envelope=%+v err=%v", ok, retiring, err)
	}
	if _, ok, err := store.LoadPending(context.Background(), SecretStoreForbidUI); err != nil || ok {
		t.Fatalf("pending survived promotion: ok=%v err=%v", ok, err)
	}

	revokeFailure := errors.New("offline")
	err = store.RevokeRetiring(
		context.Background(),
		retiring.Generation,
		SecretStoreForbidUI,
		func(context.Context, OAuthSecretEnvelope) error { return revokeFailure },
	)
	if !errors.Is(err, revokeFailure) {
		t.Fatalf("revoke failure = %v", err)
	}
	if _, ok, _ := store.LoadRetiring(context.Background(), SecretStoreForbidUI); !ok {
		t.Fatal("failed revocation deleted retiring token")
	}
	var revoked OAuthSecretEnvelope
	err = store.RevokeRetiring(
		context.Background(),
		retiring.Generation,
		SecretStoreForbidUI,
		func(_ context.Context, envelope OAuthSecretEnvelope) error {
			revoked = envelope
			return nil
		},
	)
	if err != nil || revoked.RefreshToken != "old-refresh" {
		t.Fatalf("successful retire: revoked=%+v err=%v", revoked, err)
	}
	if _, ok, _ := store.LoadRetiring(context.Background(), SecretStoreForbidUI); ok {
		t.Fatal("successfully revoked token remains retiring")
	}
}

func TestOAuthSecretStorePromotionResumesEveryWriteBoundary(t *testing.T) {
	t.Run("current write failed after retiring persisted", func(t *testing.T) {
		backend := newMemoryOAuthSecretBackend()
		store := newTestOAuthStore(t, backend)
		old := establishOAuthCurrent(t, store, "old-refresh")
		pending, err := store.CreatePending(context.Background(), testOAuthEnvelope(
			"new-refresh", "token-new", "user-1", "relay-1",
		), SecretStoreForbidUI)
		if err != nil {
			t.Fatal(err)
		}
		failOnce := true
		backend.fail = func(op, service string) error {
			if failOnce && op == "set" && service == oauthSecretCurrentService {
				failOnce = false
				return errors.New("simulated current write failure")
			}
			return nil
		}
		if _, err := store.PromotePending(context.Background(), pending.Generation, SecretStoreForbidUI); err == nil {
			t.Fatal("promotion unexpectedly succeeded")
		}
		current, ok, _ := store.LoadCurrent(context.Background(), SecretStoreForbidUI)
		if !ok || current.Generation != old.Generation {
			t.Fatal("failed promotion replaced working current")
		}
		retiring, ok, _ := store.LoadRetiring(context.Background(), SecretStoreForbidUI)
		if !ok || retiring.Generation != old.Generation {
			t.Fatal("old current was not durably staged for retirement")
		}
		backend.fail = nil
		resumed, err := store.PromotePending(context.Background(), pending.Generation, SecretStoreForbidUI)
		if err != nil || resumed.Generation != pending.Generation {
			t.Fatalf("resume: current=%+v err=%v", resumed, err)
		}
	})

	t.Run("pending delete failed after current persisted", func(t *testing.T) {
		backend := newMemoryOAuthSecretBackend()
		store := newTestOAuthStore(t, backend)
		establishOAuthCurrent(t, store, "old-refresh")
		pending, err := store.CreatePending(context.Background(), testOAuthEnvelope(
			"new-refresh", "token-new", "user-1", "relay-1",
		), SecretStoreForbidUI)
		if err != nil {
			t.Fatal(err)
		}
		failOnce := true
		backend.fail = func(op, service string) error {
			if failOnce && op == "delete" && service == oauthSecretPendingService {
				failOnce = false
				return errors.New("simulated pending delete failure")
			}
			return nil
		}
		if _, err := store.PromotePending(context.Background(), pending.Generation, SecretStoreForbidUI); err == nil {
			t.Fatal("promotion unexpectedly reported success")
		}
		current, ok, _ := store.LoadCurrent(context.Background(), SecretStoreForbidUI)
		if !ok || current.Generation != pending.Generation {
			t.Fatal("new current was not durably written")
		}
		backend.fail = nil
		if _, err := store.PromotePending(context.Background(), pending.Generation, SecretStoreForbidUI); err != nil {
			t.Fatalf("idempotent resume: %v", err)
		}
		if _, ok, _ := store.LoadPending(context.Background(), SecretStoreForbidUI); ok {
			t.Fatal("resumed promotion left pending")
		}
	})
}

func TestOAuthSecretStoreRejectsGenerationMismatchAndCorruptEnvelope(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	pending, err := store.CreatePending(context.Background(), testOAuthEnvelope(
		"refresh", "token-1", "user-1", "relay-1",
	), SecretStoreForbidUI)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PromotePending(context.Background(), stringsRepeat("0", 32), SecretStoreForbidUI); !IsCloudErrorCode(err, CloudErrSecretConflict) {
		t.Fatalf("mismatch error = %v", err)
	}
	if _, ok, _ := store.LoadPending(context.Background(), SecretStoreForbidUI); !ok {
		t.Fatal("generation mismatch deleted pending")
	}
	backend.values[oauthSecretPendingService+"\x00default"] = `{"schema_version":1,"unknown":"secret"}`
	if _, _, err := store.LoadPending(context.Background(), SecretStoreForbidUI); !IsCloudErrorCode(err, CloudErrSecretCorrupt) {
		t.Fatalf("corrupt envelope error = %v", err)
	}
	if pending.Generation == "" {
		t.Fatal("pending generation was not created")
	}
	for _, policy := range backend.policies {
		if policy != SecretStoreForbidUI {
			t.Fatalf("backend policy = %q, expected forbid_ui", policy)
		}
	}
}

func TestOAuthSecretStoreUpdateCurrentUsesGenerationAndProfileBinding(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	current := establishOAuthCurrent(t, store, "refresh")
	advanced := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	current.RefreshTokenExpiresAt = &advanced
	if err := store.UpdateCurrent(
		context.Background(),
		current,
		current.Generation,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatalf("UpdateCurrent: %v", err)
	}
	updated, ok, err := store.LoadCurrent(context.Background(), SecretStoreForbidUI)
	if err != nil || !ok || updated.RefreshTokenExpiresAt == nil ||
		!updated.RefreshTokenExpiresAt.Equal(advanced) {
		t.Fatalf("updated current = %+v ok=%v err=%v", updated, ok, err)
	}
	current.RefreshToken = "rotated-under-same-generation"
	if err := store.UpdateCurrent(
		context.Background(),
		current,
		current.Generation,
		SecretStoreForbidUI,
	); !IsCloudErrorCode(err, CloudErrSecretConflict) {
		t.Fatalf("credential mutation error = %v", err)
	}
	current.RefreshToken = updated.RefreshToken
	current.HAUserID = "different-user"
	if err := store.UpdateCurrent(
		context.Background(),
		current,
		current.Generation,
		SecretStoreForbidUI,
	); !IsCloudErrorCode(err, CloudErrSecretConflict) {
		t.Fatalf("identity mutation error = %v", err)
	}

	other, err := NewOAuthSecretStore(backend, "other")
	if err != nil {
		t.Fatal(err)
	}
	backend.values[oauthSecretCurrentService+"\x00other"] =
		backend.values[oauthSecretCurrentService+"\x00default"]
	if _, _, err := other.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); !IsCloudErrorCode(err, CloudErrSecretCorrupt) {
		t.Fatalf("cross-profile copy error = %v", err)
	}
}

func TestOAuthSecretStoreKeepsRetiringUntilRevocationAndDeleteComplete(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	establishOAuthCurrent(t, store, "old-refresh")
	pending, err := store.CreatePending(context.Background(), testOAuthEnvelope(
		"new-refresh", "token-new", "user-1", "relay-1",
	), SecretStoreForbidUI)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	retiring, ok, _ := store.LoadRetiring(context.Background(), SecretStoreForbidUI)
	if !ok {
		t.Fatal("missing retiring token")
	}
	failOnce := true
	backend.fail = func(op, service string) error {
		if failOnce && op == "delete" && service == oauthSecretRetiringService {
			failOnce = false
			return errors.New("simulated retiring delete failure")
		}
		return nil
	}
	revokeCalls := 0
	revoker := func(context.Context, OAuthSecretEnvelope) error {
		revokeCalls++
		return nil
	}
	if err := store.RevokeRetiring(
		context.Background(),
		retiring.Generation,
		SecretStoreForbidUI,
		revoker,
	); err == nil {
		t.Fatal("retirement unexpectedly reported success")
	}
	if _, ok, _ := store.LoadRetiring(context.Background(), SecretStoreForbidUI); !ok {
		t.Fatal("delete failure lost retiring token")
	}
	backend.fail = nil
	if err := store.RevokeRetiring(
		context.Background(),
		retiring.Generation,
		SecretStoreForbidUI,
		revoker,
	); err != nil {
		t.Fatalf("retirement retry: %v", err)
	}
	if revokeCalls != 2 {
		t.Fatalf("idempotent revocation calls = %d", revokeCalls)
	}
}

func stringsRepeat(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
