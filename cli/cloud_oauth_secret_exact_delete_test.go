package main

import (
	"context"
	"testing"
)

func TestExactOAuthCleanupPreservesReplacementSlots(
	t *testing.T,
) {
	for _, slot := range []OAuthSecretState{
		OAuthSecretCurrent,
		OAuthSecretPending,
		OAuthSecretRetiring,
	} {
		t.Run(string(slot), func(t *testing.T) {
			backend := newMemoryOAuthSecretBackend()
			store := newTestOAuthStore(t, backend)
			current := establishOAuthCurrent(t, store, "current-a")
			expected := current
			switch slot {
			case OAuthSecretPending:
				var err error
				expected, err = store.CreatePending(
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
			case OAuthSecretRetiring:
				expected.State = OAuthSecretRetiring
				if err := store.write(
					context.Background(),
					oauthSecretRetiringService,
					expected,
					SecretStoreForbidUI,
				); err != nil {
					t.Fatal(err)
				}
			}
			memoized := memoizeOAuthSecretStore(store)
			switch slot {
			case OAuthSecretCurrent:
				_, _, _ = memoized.LoadCurrent(
					context.Background(),
					SecretStoreForbidUI,
				)
			case OAuthSecretPending:
				_, _, _ = memoized.LoadPending(
					context.Background(),
					SecretStoreForbidUI,
				)
			case OAuthSecretRetiring:
				_, _, _ = memoized.LoadRetiring(
					context.Background(),
					SecretStoreForbidUI,
				)
			}
			replacement := expected
			replacement.RefreshToken = "replacement-b"
			service := map[OAuthSecretState]string{
				OAuthSecretCurrent:  oauthSecretCurrentService,
				OAuthSecretPending:  oauthSecretPendingService,
				OAuthSecretRetiring: oauthSecretRetiringService,
			}[slot]
			if err := store.write(
				context.Background(),
				service,
				replacement,
				SecretStoreForbidUI,
			); err != nil {
				t.Fatal(err)
			}
			exact, err := exactOAuthCleanupStoreFor(memoized)
			if err != nil {
				t.Fatal(err)
			}
			switch slot {
			case OAuthSecretCurrent:
				err = exact.DeleteCurrentExact(
					context.Background(),
					expected,
					SecretStoreForbidUI,
				)
			case OAuthSecretPending:
				err = exact.DeletePendingExact(
					context.Background(),
					expected,
					SecretStoreForbidUI,
				)
			case OAuthSecretRetiring:
				err = exact.DeleteRetiringExact(
					context.Background(),
					expected,
					SecretStoreForbidUI,
				)
			}
			if !IsCloudErrorCode(err, CloudErrSecretConflict) {
				t.Fatalf("exact delete error = %v", err)
			}
			value, exists, err := store.load(
				context.Background(),
				service,
				slot,
				SecretStoreForbidUI,
			)
			if err != nil || !exists ||
				!sameOAuthSecretEnvelope(value, replacement) {
				t.Fatalf(
					"replacement exists=%v value=%+v err=%v",
					exists,
					value,
					err,
				)
			}
		})
	}
}
