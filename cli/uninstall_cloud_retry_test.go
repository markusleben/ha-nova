package main

import (
	"context"
	"errors"
	"testing"
)

func TestMultiProfileUninstallKeepsEveryOAuthProofUntilRemotePhaseCompletes(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := setupServerCommandTest(t, `{"schema_version":1}`)

	cabinBackend := newMemoryOAuthSecretBackend()
	cabinEnvelope := productionCloudTestEnvelope()
	cabinEnvelope.ProfileID = "profile-cabin-retry"
	cabinStore, err := NewOAuthSecretStore(
		cabinBackend,
		cabinEnvelope.ProfileID,
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := cabinStore.CreatePending(
		context.Background(),
		cabinEnvelope,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := cabinStore.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(current.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, current)
	cabin := runtimeConfig{
		ProfileID:       cabinEnvelope.ProfileID,
		RelayInstanceID: cabinEnvelope.RelayInstanceID,
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	defaultBackend := newMemoryOAuthSecretBackend()
	defaultEnvelope := productionCloudTestEnvelope()
	defaultEnvelope.ProfileID = "profile-default-retry"
	defaultEnvelope.Generation = "ffffffffffffffffffffffffffffffff"
	defaultStore, err := NewOAuthSecretStore(
		defaultBackend,
		defaultEnvelope.ProfileID,
	)
	if err != nil {
		t.Fatal(err)
	}
	defaultPending, err := defaultStore.CreatePending(
		context.Background(),
		defaultEnvelope,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	defaultCurrent, err := defaultStore.PromotePending(
		context.Background(),
		defaultPending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	defaultMetadata := cloudMetadataFromEnvelope(origin, defaultCurrent)
	defaultProfile := runtimeConfig{
		ProfileID:          defaultEnvelope.ProfileID,
		RelayInstanceID:    defaultEnvelope.RelayInstanceID,
		RoutePolicy:        routePolicyLocal,
		RelaySecureBaseURL: "https://default.local:8792",
		RelaySpkiPin:       "pin",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &defaultMetadata,
		},
	}
	writeCloudRetryProfiles(t, paths, cabin, defaultProfile)

	credential := validCredential(120)
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		credential,
	); err != nil {
		t.Fatal(err)
	}
	failDefaultRevoke := true
	oldStore := newCloudSecretStoreForCLI
	oldOAuthRevoke := revokeAndVerifyCloudAuthorizationForCLI
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
	var oauthRevokes []string
	revokeAndVerifyCloudAuthorizationForCLI = func(
		_ context.Context,
		envelope OAuthSecretEnvelope,
	) error {
		oauthRevokes = append(oauthRevokes, envelope.ProfileID)
		if envelope.ProfileID == defaultProfile.ProfileID &&
			failDefaultRevoke {
			return errors.New("simulated later-profile OAuth failure")
		}
		return nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = oldStore
		revokeAndVerifyCloudAuthorizationForCLI = oldOAuthRevoke
	})
	deviceRevokes := 0
	installRemoteDeviceRevokeHook(
		t,
		func(
			context.Context,
			runtimeConfig,
			OAuthSecretStore,
			string,
		) error {
			deviceRevokes++
			return nil
		},
	)

	if err := purgeCloudAuthorizationsForUninstall(
		paths,
		&uninstallReport{},
	); err == nil {
		t.Fatal("first multi-profile purge unexpectedly succeeded")
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	checkpointed, ok := doc.flatProfile("cabin")
	if !ok ||
		checkpointed.Cloud == nil ||
		checkpointed.Cloud.DeviceRevocationCompleted == nil ||
		checkpointed.Cloud.DeviceRevocationCompleted.CurrentDeviceID !=
			deviceIDOf(credential) {
		t.Fatalf("successful profile checkpoint = %+v", checkpointed.Cloud)
	}
	if _, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile("cabin"),
	); err != nil || exists {
		t.Fatalf("first purge current exists=%v err=%v", exists, err)
	}
	if deviceRevokes != 1 {
		t.Fatalf("first purge device revokes = %d", deviceRevokes)
	}
	for name, store := range map[string]OAuthSecretStore{
		"cabin":   cabinStore,
		"default": defaultStore,
	} {
		if _, exists, err := store.LoadCurrent(
			context.Background(),
			SecretStoreForbidUI,
		); err != nil || !exists {
			t.Fatalf(
				"%s OAuth proof after remote failure exists=%v err=%v",
				name,
				exists,
				err,
			)
		}
	}
	if len(oauthRevokes) != 2 ||
		oauthRevokes[0] != cabin.ProfileID ||
		oauthRevokes[1] != defaultProfile.ProfileID {
		t.Fatalf("first remote revokes = %v", oauthRevokes)
	}

	failDefaultRevoke = false
	retryErr := purgeCloudAuthorizationsForUninstall(
		paths,
		&uninstallReport{},
	)
	if retryErr != nil {
		t.Fatalf("multi-profile retry: %v", retryErr)
	}
	if deviceRevokes != 1 {
		t.Fatalf("retry repeated device revocation: %d", deviceRevokes)
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
				"%s OAuth proof after retry exists=%v err=%v",
				name,
				exists,
				err,
			)
		}
	}
}

func writeCloudRetryProfiles(
	t *testing.T,
	paths runtimePaths,
	cabin runtimeConfig,
	defaultProfile runtimeConfig,
) {
	t.Helper()
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	top, err := doc.withProfile(defaultServerProfileName, defaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err = loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	top, err = doc.withProfile("cabin", cabin)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}
}
