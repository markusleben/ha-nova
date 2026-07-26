package main

import (
	"context"
	"errors"
	"testing"
)

func TestMultiProfileUninstallResumesAfterLaterLocalOAuthDeleteFailure(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := setupServerCommandTest(t, `{"schema_version":1}`)

	createProfile := func(
		profileID string,
		generation string,
	) (
		runtimeConfig,
		*KeyringOAuthSecretStore,
		*memoryOAuthSecretBackend,
	) {
		backend := newMemoryOAuthSecretBackend()
		envelope := productionCloudTestEnvelope()
		envelope.ProfileID = profileID
		envelope.Generation = generation
		store, err := NewOAuthSecretStore(backend, profileID)
		if err != nil {
			t.Fatal(err)
		}
		pending, err := store.CreatePending(
			context.Background(),
			envelope,
			SecretStoreAllowUI,
		)
		if err != nil {
			t.Fatal(err)
		}
		current, err := store.PromotePending(
			context.Background(),
			pending.Generation,
			SecretStoreAllowUI,
		)
		if err != nil {
			t.Fatal(err)
		}
		origin, err := cloudOriginFromCanonical(
			current.CanonicalOrigin,
		)
		if err != nil {
			t.Fatal(err)
		}
		metadata := cloudMetadataFromEnvelope(origin, current)
		return runtimeConfig{
			ProfileID:          profileID,
			RelayInstanceID:    current.RelayInstanceID,
			RoutePolicy:        routePolicyAutomatic,
			RelaySecureBaseURL: "https://local.example:8792",
			RelaySpkiPin:       "pin",
			Cloud: &cloudLifecycleMetadata{
				State:   cloudStateReady,
				Current: &metadata,
			},
		}, store, backend
	}

	cabin, cabinStore, _ := createProfile(
		"profile-cabin-local-retry",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	defaultProfile, defaultStore, defaultBackend := createProfile(
		"profile-default-local-retry",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)
	writeCloudRetryProfiles(t, paths, cabin, defaultProfile)

	oldStore := newCloudSecretStoreForCLI
	oldRevoke := revokeAndVerifyCloudAuthorizationForCLI
	newCloudSecretStoreForCLI = func(
		profileID string,
	) (OAuthSecretStore, error) {
		switch profileID {
		case cabin.ProfileID:
			return cabinStore, nil
		case defaultProfile.ProfileID:
			return defaultStore, nil
		default:
			t.Fatalf("unexpected profile id %q", profileID)
			return nil, nil
		}
	}
	var revocations []string
	revokeAndVerifyCloudAuthorizationForCLI = func(
		_ context.Context,
		envelope OAuthSecretEnvelope,
	) error {
		revocations = append(revocations, envelope.ProfileID)
		return nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = oldStore
		revokeAndVerifyCloudAuthorizationForCLI = oldRevoke
	})

	deleteFailure := true
	defaultBackend.fail = func(op, service string) error {
		if deleteFailure &&
			op == "delete" &&
			service == oauthSecretCurrentService {
			return errors.New("simulated keyring relock")
		}
		return nil
	}
	if err := purgeCloudAuthorizationsForUninstall(
		paths,
		&uninstallReport{},
	); err == nil {
		t.Fatal("first purge unexpectedly survived local OAuth delete failure")
	}
	if len(revocations) != 2 {
		t.Fatalf("first remote revocations = %v", revocations)
	}
	if _, exists, err := cabinStore.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf(
			"earlier local proof exists=%v err=%v",
			exists,
			err,
		)
	}
	if _, exists, err := defaultStore.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || !exists {
		t.Fatalf(
			"later local proof exists=%v err=%v",
			exists,
			err,
		)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, profileName := range []string{"cabin", defaultServerProfileName} {
		cfg, ok := doc.flatProfile(profileName)
		if !ok ||
			cfg.Cloud == nil ||
			cfg.Cloud.AuthorizationRevocationCompleted == nil {
			t.Fatalf(
				"server %q authorization checkpoint = %+v",
				profileName,
				cfg.Cloud,
			)
		}
	}

	deleteFailure = false
	if err := purgeCloudAuthorizationsForUninstall(
		paths,
		&uninstallReport{},
	); err != nil {
		t.Fatalf("retry purge: %v", err)
	}
	if len(revocations) != 2 {
		t.Fatalf("retry repeated remote revocation: %v", revocations)
	}
	for name, store := range map[string]OAuthSecretStore{
		"cabin":   cabinStore,
		"default": defaultStore,
	} {
		if _, exists, err := store.LoadCurrent(
			context.Background(),
			SecretStoreForbidUI,
		); err != nil || exists {
			t.Fatalf(
				"%s proof after retry exists=%v err=%v",
				name,
				exists,
				err,
			)
		}
	}
}
