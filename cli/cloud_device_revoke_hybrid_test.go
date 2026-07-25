package main

import (
	"context"
	"testing"
)

func TestHybridCloudPendingIsRevokedBeforeDeletion(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	setActiveServerProfile(remoteOnlyCloudTestProfile)
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	pendingEnvelope, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(
		pendingEnvelope.CanonicalOrigin,
	)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pendingEnvelope)
	cfg := runtimeConfig{
		ProfileID:            pendingEnvelope.ProfileID,
		RelayInstanceID:      pendingEnvelope.RelayInstanceID,
		RelaySecureBaseURL:   "https://local-relay:8792",
		RelaySpkiPin:         "local-pin",
		PendingSecureBaseURL: "https://local-pending:8792",
		PendingSpkiPin:       "local-pending-pin",
		Cloud: &cloudLifecycleMetadata{
			State:                   cloudStateDeviceBoundOrPaired,
			Pending:                 &metadata,
			DeviceActivationStarted: true,
		},
	}
	currentCredential := validCredential(95)
	pendingCredential := validCredential(96)
	cfg.Cloud.DeviceActivationDeviceID = deviceIDOf(pendingCredential)
	if err := writeDeviceCredential(currentCredential); err != nil {
		t.Fatal(err)
	}
	if err := writePendingCloudDeviceCredential(
		pendingCredential,
		cfg.RelayInstanceID,
	); err != nil {
		t.Fatal(err)
	}
	revokeCalls := 0
	installRemoteDeviceRevokeHook(
		t,
		func(
			_ context.Context,
			got runtimeConfig,
			_ OAuthSecretStore,
			credential string,
		) error {
			revokeCalls++
			if credential != pendingCredential ||
				got.Cloud == nil ||
				got.Cloud.Pending == nil {
				t.Fatalf(
					"hybrid revoke cfg=%+v credential=%q",
					got,
					credential,
				)
			}
			return nil
		},
	)

	if _, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
		context.Background(),
		cfg,
		remoteOnlyCloudTestProfile,
		store,
		nil,
		false,
		acceptCloudDeviceRevocationCheckpoint,
	); err != nil {
		t.Fatal(err)
	}
	if revokeCalls != 1 {
		t.Fatalf("remote revokes = %d", revokeCalls)
	}
	if current, exists, err := readDeviceCredential(); err != nil ||
		!exists ||
		current != currentCredential {
		t.Fatalf(
			"local current was not preserved: exists=%v err=%v",
			exists,
			err,
		)
	}
	if _, exists, err :=
		readPendingDeviceCredentialRecord(); err != nil || exists {
		t.Fatalf("pending remains: exists=%v err=%v", exists, err)
	}
}
