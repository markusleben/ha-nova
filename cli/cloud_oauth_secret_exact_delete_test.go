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

func TestExactOAuthCleanupInvalidatesMemoizedDeletedSlots(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	current := establishOAuthCurrent(t, store, "current-a")
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
	retiring := current
	retiring.State = OAuthSecretRetiring
	if err := store.write(
		context.Background(),
		oauthSecretRetiringService,
		retiring,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	memoized := memoizeOAuthSecretStore(store)
	_, _, _ = memoized.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	)
	_, _, _ = memoized.LoadPending(
		context.Background(),
		SecretStoreForbidUI,
	)
	_, _, _ = memoized.LoadRetiring(
		context.Background(),
		SecretStoreForbidUI,
	)
	exact, err := exactOAuthCleanupStoreFor(memoized)
	if err != nil {
		t.Fatal(err)
	}
	if err := exact.DeleteCurrentExact(
		context.Background(),
		current,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	if err := exact.DeletePendingExact(
		context.Background(),
		pending,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	if err := exact.DeleteRetiringExact(
		context.Background(),
		retiring,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	for name, load := range map[string]func() (
		OAuthSecretEnvelope,
		bool,
		error,
	){
		"current": func() (
			OAuthSecretEnvelope,
			bool,
			error,
		) {
			return memoized.LoadCurrent(
				context.Background(),
				SecretStoreForbidUI,
			)
		},
		"pending": func() (
			OAuthSecretEnvelope,
			bool,
			error,
		) {
			return memoized.LoadPending(
				context.Background(),
				SecretStoreForbidUI,
			)
		},
		"retiring": func() (
			OAuthSecretEnvelope,
			bool,
			error,
		) {
			return memoized.LoadRetiring(
				context.Background(),
				SecretStoreForbidUI,
			)
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, exists, err := load()
			if err != nil || exists {
				t.Fatalf(
					"memoized slot exists=%v err=%v",
					exists,
					err,
				)
			}
		})
	}
}

func TestExactOAuthCleanupInvalidatesMemoizedAlreadyAbsentSlots(
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
			expected := establishOAuthCurrent(
				t,
				store,
				"current-a",
			)
			service := oauthSecretCurrentService
			switch slot {
			case OAuthSecretPending:
				service = oauthSecretPendingService
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
				service = oauthSecretRetiringService
				expected.State = OAuthSecretRetiring
				if err := store.write(
					context.Background(),
					service,
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
			if err := backend.Delete(
				context.Background(),
				service,
				store.account,
				SecretStoreForbidUI,
			); err != nil {
				t.Fatal(err)
			}
			exact, err := exactOAuthCleanupStoreFor(
				memoized,
			)
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
			if err != nil {
				t.Fatal(err)
			}
			var exists bool
			switch slot {
			case OAuthSecretCurrent:
				_, exists, err = memoized.LoadCurrent(
					context.Background(),
					SecretStoreForbidUI,
				)
			case OAuthSecretPending:
				_, exists, err = memoized.LoadPending(
					context.Background(),
					SecretStoreForbidUI,
				)
			case OAuthSecretRetiring:
				_, exists, err = memoized.LoadRetiring(
					context.Background(),
					SecretStoreForbidUI,
				)
			}
			if err != nil || exists {
				t.Fatalf(
					"memoized absent slot exists=%v err=%v",
					exists,
					err,
				)
			}
		})
	}
}
