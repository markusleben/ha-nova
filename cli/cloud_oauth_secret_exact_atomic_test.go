package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExactOAuthDeleteRejectsReplacementInsideBackendOperation(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	expected, err := store.CreatePending(
		context.Background(),
		testOAuthEnvelope(
			"pending-a",
			"pending-a",
			"user-1",
			"relay-1",
		),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	replacement := expected
	replacement.RefreshToken = "replacement-b"
	encodedReplacement, err := json.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}
	backend.beforeDeleteExact = func(service, account string) {
		backend.values[service+"\x00"+account] =
			string(encodedReplacement)
		backend.beforeDeleteExact = nil
	}
	err = store.DeletePendingExact(
		context.Background(),
		expected,
		SecretStoreForbidUI,
	)
	if !IsCloudErrorCode(err, CloudErrSecretConflict) {
		t.Fatalf("exact delete error = %v", err)
	}
	assertPendingOAuthReplacement(t, store, replacement)
}

func TestExactPendingGrantDeleteBindsCanonicalOrigin(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	pending, err := store.CreatePending(
		context.Background(),
		testOAuthEnvelope(
			"pending-a",
			"pending-a",
			"user-1",
			"relay-1",
		),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	exact, err := exactOAuthCleanupStoreFor(store)
	if err != nil {
		t.Fatal(err)
	}
	err = exact.DeletePendingGrantExact(
		context.Background(),
		pending.Generation,
		"https://other.ui.nabu.casa",
		pending.RefreshToken,
		pending.ClientID,
		SecretStoreForbidUI,
	)
	if !IsCloudErrorCode(err, CloudErrSecretConflict) {
		t.Fatalf("cross-origin exact delete error = %v", err)
	}
	actual, exists, err := store.LoadPending(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists ||
		!sameOAuthSecretEnvelope(actual, pending) {
		t.Fatalf(
			"pending exists=%v actual=%+v err=%v",
			exists,
			actual,
			err,
		)
	}
}
