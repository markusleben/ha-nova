package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestOAuthSecretGenerationAndCreatePendingPreserveReservation(t *testing.T) {
	first, err := NewOAuthSecretGeneration()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOAuthSecretGeneration()
	if err != nil {
		t.Fatal(err)
	}
	if !oauthSecretGenerationPattern.MatchString(first) ||
		!oauthSecretGenerationPattern.MatchString(second) ||
		first == second {
		t.Fatalf("invalid generations: %q %q", first, second)
	}

	store := newTestOAuthStore(t, newMemoryOAuthSecretBackend())
	reserved := strings.Repeat("a", 32)
	envelope := testOAuthEnvelope("refresh", "token-1", "user-1", "relay-1")
	envelope.Generation = reserved
	pending, err := store.CreatePending(
		context.Background(),
		envelope,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Generation != reserved {
		t.Fatalf("generation changed: got %q, want %q", pending.Generation, reserved)
	}
}

func TestOAuthSecretStorePreflightIsScopedProbeWithExplicitPolicy(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	store.random = bytes.NewReader(bytes.Repeat([]byte{0x2a}, 16))

	if err := PreflightOAuthSecretStore(
		context.Background(),
		store,
		SecretStoreAllowUI,
	); err != nil {
		t.Fatal(err)
	}
	if len(backend.values) != 0 {
		t.Fatalf("preflight left credential-store entries: %v", backend.values)
	}
	if len(backend.policies) != 3 {
		t.Fatalf("backend calls = %d, want write/read/delete", len(backend.policies))
	}
	wantPolicies := []SecretStoreUIPolicy{
		SecretStoreAllowUI,
		SecretStoreForbidUI,
		SecretStoreForbidUI,
	}
	for index, policy := range backend.policies {
		if policy != wantPolicies[index] {
			t.Fatalf(
				"backend policy[%d] = %q, want %q",
				index,
				policy,
				wantPolicies[index],
			)
		}
	}
}

func TestOAuthSecretStorePreflightFailsClosedIfStoreRelocksAfterWrite(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	store.random = bytes.NewReader(bytes.Repeat([]byte{0x31}, 16))
	backend.fail = func(op, service string) error {
		if op == "get" && strings.HasPrefix(service, oauthSecretPreflightServicePrefix) {
			return newCloudError(
				CloudErrSecretUIForbidden,
				"read relocked preflight secret",
				nil,
			)
		}
		return nil
	}

	err := store.Preflight(context.Background(), SecretStoreAllowUI)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
		t.Fatalf("preflight error = %v, want SecretUIForbidden", err)
	}
	if len(backend.policies) != 3 ||
		backend.policies[0] != SecretStoreAllowUI ||
		backend.policies[1] != SecretStoreForbidUI ||
		backend.policies[2] != SecretStoreForbidUI {
		t.Fatalf("backend policies = %v", backend.policies)
	}
}

func TestOAuthSecretStorePreflightCleansProbeAfterReadFailure(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	store.random = bytes.NewReader(bytes.Repeat([]byte{0x19}, 16))
	readFailure := errors.New("simulated read failure")
	backend.fail = func(op, service string) error {
		if op == "get" && strings.HasPrefix(service, oauthSecretPreflightServicePrefix) {
			return readFailure
		}
		return nil
	}

	err := store.Preflight(context.Background(), SecretStoreForbidUI)
	if !errors.Is(err, readFailure) {
		t.Fatalf("preflight error = %v", err)
	}
	if len(backend.values) != 0 {
		t.Fatalf("failed preflight left probe: %v", backend.values)
	}
}
