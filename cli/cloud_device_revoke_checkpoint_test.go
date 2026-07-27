package main

import (
	"context"
	"strings"
	"testing"
)

func acceptCloudDeviceRevocationCheckpoint(
	cloudDeviceRevocationCheckpoint,
) error {
	return nil
}

func TestBoundPendingCloudDeviceUsesPendingProvenanceBeforeOAuth(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	setActiveServerProfile(remoteOnlyCloudTestProfile)

	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(pending.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pending)
	credential := validCredential(22)
	cfg := runtimeConfig{
		ProfileID:       pending.ProfileID,
		RelayInstanceID: pending.RelayInstanceID,
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:                    cloudStateDeviceBoundOrPaired,
			Pending:                  &metadata,
			DeviceActivationStarted:  true,
			DeviceActivationDeviceID: deviceIDOf(credential),
		},
	}
	if err := writePendingCloudDeviceCredential(
		credential,
		cfg.RelayInstanceID,
	); err != nil {
		t.Fatal(err)
	}

	selectedMetadata, selectedEnvelope, err :=
		cloudDeviceRevocationAuthorization(
			context.Background(),
			cfg,
			store,
		)
	if err != nil {
		t.Fatal(err)
	}
	if selectedMetadata != metadata ||
		selectedEnvelope.Generation != pending.Generation {
		t.Fatalf(
			"selected authorization metadata=%+v envelope=%+v",
			selectedMetadata,
			selectedEnvelope,
		)
	}
	promoted, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	selectedMetadata, selectedEnvelope, err =
		cloudDeviceRevocationAuthorization(
			context.Background(),
			cfg,
			store,
		)
	if err != nil {
		t.Fatal(err)
	}
	if selectedMetadata != metadata ||
		selectedEnvelope.Generation != promoted.Generation {
		t.Fatalf(
			"selected promoted authorization metadata=%+v envelope=%+v",
			selectedMetadata,
			selectedEnvelope,
		)
	}

	calls := 0
	installRemoteDeviceRevokeHook(
		t,
		func(
			_ context.Context,
			gotConfig runtimeConfig,
			gotStore OAuthSecretStore,
			gotCredential string,
		) error {
			calls++
			if gotConfig.Cloud == nil ||
				gotConfig.Cloud.Pending == nil ||
				gotStore != store ||
				gotCredential != credential {
				t.Fatalf(
					"bound pending revoke cfg=%+v store=%T credential=%q",
					gotConfig,
					gotStore,
					gotCredential,
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
		acceptCloudDeviceRevocationCheckpoint,
	); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("remote device revokes = %d", calls)
	}
	if _, exists, err := readPendingDeviceCredentialRecord(); err != nil ||
		exists {
		t.Fatalf(
			"bound pending credential remained: exists=%v err=%v",
			exists,
			err,
		)
	}
}

func TestReconnectCurrentDeviceUsesOnlyCurrentOAuthProvenance(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	setActiveServerProfile(remoteOnlyCloudTestProfile)

	envelope := productionCloudTestEnvelope()
	envelope.ProfileID = "profile-1"
	origin, err := cloudOriginFromCanonical(envelope.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	current := cloudMetadataFromEnvelope(origin, envelope)
	pending := current
	pending.CredentialGeneration = strings.Repeat("e", 32)
	pending.OAuthClientID = "http://127.0.0.1:54322/ha-nova"
	cfg := runtimeConfig{
		ProfileID:       envelope.ProfileID,
		RelayInstanceID: envelope.RelayInstanceID,
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateCloudVerified,
			Current: &current,
			Pending: &pending,
		},
	}
	credential := validCredential(80)
	if err := secretSet(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
		credential,
	); err != nil {
		t.Fatal(err)
	}

	calls := 0
	installRemoteDeviceRevokeHook(
		t,
		func(
			_ context.Context,
			gotConfig runtimeConfig,
			_ OAuthSecretStore,
			gotCredential string,
		) error {
			calls++
			if gotCredential != credential ||
				gotConfig.Cloud == nil ||
				gotConfig.Cloud.State != cloudStateReady ||
				gotConfig.Cloud.Pending != nil ||
				gotConfig.Cloud.Current == nil ||
				*gotConfig.Cloud.Current != current {
				t.Fatalf(
					"current-device revocation provenance cfg=%+v credential=%q",
					gotConfig,
					gotCredential,
				)
			}
			return nil
		},
	)
	if _, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
		context.Background(),
		cfg,
		remoteOnlyCloudTestProfile,
		nil,
		nil,
		acceptCloudDeviceRevocationCheckpoint,
	); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("current-device revokes = %d", calls)
	}
}
