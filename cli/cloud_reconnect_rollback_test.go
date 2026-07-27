package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRollingBackCloudStateRemainsRemovable(t *testing.T) {
	raw := json.RawMessage(`{
		"state":"rolling_back",
		"current":{"origin":"https://old.ui.nabu.casa"},
		"pending":{"origin":"https://new.ui.nabu.casa"}
	}`)
	if err := validateKnownCloudRemovalShape("default", raw); err != nil {
		t.Fatalf("rolling-back cleanup shape rejected: %v", err)
	}
}

func TestRejectCloudReconnectUserChangeBeforeDeviceBinding(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("a", 32))
	current.HAUserID = "user-current"
	cfg := runtimeConfig{
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &current,
		},
	}
	if err := rejectCloudReconnectUserChange(cfg, "user-current"); err != nil {
		t.Fatalf("same user rejected: %v", err)
	}
	if err := rejectCloudReconnectUserChange(cfg, "user-new"); !IsCloudErrorCode(
		err,
		CloudErrDeviceUserConflict,
	) {
		t.Fatalf("different user error = %v", err)
	}
}

func TestProductionCoordinatorsRejectChangedReconnectUserBeforeDeviceUse(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	current := cloudMetadataFromEnvelope(origin, productionCloudTestEnvelope())
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		ClientInstallID: "install-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &current,
		},
	}
	oldAuthorize := authorizeAndVerifyCloudForSetup
	oldDiscover := discoverCloudFromLocalRelayForSetup
	oldReadDevice := readCloudDeviceCredentialForSetup
	authorizeAndVerifyCloudForSetup = func(
		productionCloudSetupCoordinator,
		context.Context,
		cloudSetupRequest,
		CloudOrigin,
		string,
	) (cloudSetupResult, cloudVerifiedSession, OAuthSecretStore, error) {
		return cloudSetupResult{}, cloudVerifiedSession{
			User: HACurrentUser{ID: "user-new"},
		}, nil, nil
	}
	discoverCloudFromLocalRelayForSetup = func(
		context.Context,
		runtimeConfig,
	) (cloudLocalDiscovery, error) {
		return cloudLocalDiscovery{
			Origin:          origin,
			RelayInstanceID: "relay-1",
		}, nil
	}
	readCloudDeviceCredentialForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (string, bool, error) {
		return validCredential(91), true, nil
	}
	t.Cleanup(func() {
		authorizeAndVerifyCloudForSetup = oldAuthorize
		discoverCloudFromLocalRelayForSetup = oldDiscover
		readCloudDeviceCredentialForSetup = oldReadDevice
	})

	coordinator := productionCloudSetupCoordinator{}
	_, err = coordinator.AddAwayWithExistingDevice(
		context.Background(),
		cloudSetupRequest{Config: cfg},
	)
	if !IsCloudErrorCode(err, CloudErrDeviceUserConflict) {
		t.Fatalf("local reconnect error = %v", err)
	}

	_, err = coordinator.AddRemoteWithPairing(
		context.Background(),
		cloudRemoteSetupRequest{
			cloudSetupRequest: cloudSetupRequest{Config: cfg},
			Origin:            origin,
			PairingCode: func(cloudRemotePairingPrompt) (string, error) {
				t.Fatal("changed user reached remote owner pairing")
				return "", nil
			},
		},
	)
	if !IsCloudErrorCode(err, CloudErrDeviceUserConflict) {
		t.Fatalf("remote reconnect error = %v", err)
	}
}

func TestCloudReconnectUserConflictRollsBackToCurrentAuthorization(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	ctx := context.Background()
	current, err := store.CreatePending(
		ctx,
		productionCloudTestEnvelope(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err = store.PromotePending(
		ctx,
		current.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	pendingEnvelope := productionCloudTestEnvelope()
	pendingEnvelope.Generation = strings.Repeat("b", 32)
	pendingEnvelope.RefreshToken = "refresh-new"
	pendingEnvelope.RefreshTokenID = "refresh-new-id"
	pendingEnvelope.HAUserID = "user-new"
	pending, err := store.CreatePending(
		ctx,
		pendingEnvelope,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	currentMetadata := cloudMetadataFromEnvelope(origin, current)
	pendingMetadata := cloudMetadataFromEnvelope(origin, pending)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateCloudVerified,
			Current: &currentMetadata,
			Pending: &pendingMetadata,
		},
	}
	ctx, _ = withCloudSecretAccessSession(ctx, cfg.ProfileID, store)
	oldRevoke := revokeAndVerifyCloudAuthorizationForCLI
	revokeCalls := 0
	revokeAndVerifyCloudAuthorizationForCLI = func(
		_ context.Context,
		envelope OAuthSecretEnvelope,
	) error {
		revokeCalls++
		if envelope.Generation != pending.Generation {
			t.Fatalf("revoked generation = %q", envelope.Generation)
		}
		return nil
	}
	t.Cleanup(func() {
		revokeAndVerifyCloudAuthorizationForCLI = oldRevoke
	})
	savedStates := []cloudLifecycleState{}
	err = rollbackCloudReconnectAfterUserConflict(
		ctx,
		&cfg,
		func(value runtimeConfig) error {
			savedStates = append(savedStates, value.Cloud.State)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if revokeCalls != 1 ||
		len(savedStates) != 2 ||
		savedStates[0] != cloudStateRollingBack ||
		savedStates[1] != cloudStateReady ||
		!cfg.Cloud.ready() ||
		cfg.Cloud.Current.CredentialGeneration != current.Generation {
		t.Fatalf(
			"rollback calls=%d states=%v cfg=%+v",
			revokeCalls,
			savedStates,
			cfg.Cloud,
		)
	}
	if _, exists, err := store.LoadPending(
		ctx,
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf("pending after rollback: exists=%v err=%v", exists, err)
	}
}

func TestCloudReconnectRollbackResumesAfterPendingSecretDeletion(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	currentEnvelope, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	currentEnvelope, err = store.PromotePending(
		context.Background(),
		currentEnvelope.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current := cloudMetadataFromEnvelope(origin, currentEnvelope)
	pendingEnvelope := productionCloudTestEnvelope()
	pendingEnvelope.Generation = strings.Repeat("c", 32)
	pendingEnvelope.HAUserID = "user-new"
	pending := cloudMetadataFromEnvelope(origin, pendingEnvelope)
	cfg := runtimeConfig{
		ProfileID: "profile-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateRollingBack,
			Current: &current,
			Pending: &pending,
		},
	}
	ctx, _ := withCloudSecretAccessSession(
		context.Background(),
		cfg.ProfileID,
		store,
	)
	saveCalls := 0
	if err := rollbackCloudReconnectAfterUserConflict(
		ctx,
		&cfg,
		func(runtimeConfig) error {
			saveCalls++
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	if saveCalls != 1 || !cfg.Cloud.ready() {
		t.Fatalf("resumed rollback saves=%d cloud=%+v", saveCalls, cfg.Cloud)
	}
}
