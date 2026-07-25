package main

import (
	"context"
	"errors"
	"testing"
)

func TestMultiProfileUninstallRetriesFromPerProfileDeviceCheckpoint(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := setupServerCommandTest(t, `{"schema_version":1}`)

	cabinBackend := newMemoryOAuthSecretBackend()
	cabinStore := newTestOAuthStore(t, cabinBackend)
	cabinEnvelope := productionCloudTestEnvelope()
	cabinEnvelope.ProfileID = "profile-cabin-retry"
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
	failing := runtimeConfig{
		ProfileID:   "profile-default-retry",
		RoutePolicy: routePolicyLocal,
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
		},
	}
	writeCloudRetryProfiles(t, paths, cabin, failing)

	credential := validCredential(120)
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		credential,
	); err != nil {
		t.Fatal(err)
	}
	defaultStore := newTestOAuthStore(
		t,
		newMemoryOAuthSecretBackend(),
	)
	failDefault := true
	oldStore := newCloudSecretStoreForCLI
	oldOAuthRevoke := revokeAndVerifyCloudAuthorizationForCLI
	newCloudSecretStoreForCLI = func(
		profileID string,
	) (OAuthSecretStore, error) {
		switch profileID {
		case cabin.ProfileID:
			return cabinStore, nil
		case failing.ProfileID:
			if failDefault {
				return nil, newCloudError(
					CloudErrOAuthOutcomeUnknown,
					"open default retry authorization",
					errors.New("simulated secure-storage failure"),
				)
			}
			return defaultStore, nil
		default:
			t.Fatalf("unexpected profile id %q", profileID)
			return nil, nil
		}
	}
	revokeAndVerifyCloudAuthorizationForCLI = func(
		context.Context,
		OAuthSecretEnvelope,
	) error {
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
		false,
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

	failDefault = false
	if err := purgeCloudAuthorizationsForUninstall(
		paths,
		&uninstallReport{},
		false,
	); err != nil {
		t.Fatalf("checkpointed multi-profile retry: %v", err)
	}
	if deviceRevokes != 1 {
		t.Fatalf("retry repeated device revocation: %d", deviceRevokes)
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
