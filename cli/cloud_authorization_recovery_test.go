package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAuthorizingReconnectWithoutPendingSecretRestartsOAuth(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	oldPending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	oldCurrent, err := store.PromotePending(
		context.Background(),
		oldPending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	oldMetadata := cloudMetadataFromEnvelope(origin, oldCurrent)
	newEnvelope := productionCloudTestEnvelope()
	newEnvelope.Generation = strings.Repeat("b", 32)
	newEnvelope.ClientID = "http://127.0.0.1:43124/ha-nova"
	newMetadata := cloudMetadataFromEnvelope(origin, newEnvelope)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateAuthorizing,
			Current: &oldMetadata,
			Pending: &newMetadata,
		},
	}

	resetProductionCloudPolicies(backend)
	envelope, alreadyCurrent, err := resumableCloudEnvelope(
		context.Background(),
		store,
		cfg,
		origin,
		SecretStoreForbidUI,
	)
	if err != nil || alreadyCurrent || envelope.Generation != "" {
		t.Fatalf(
			"authorizing resume = %+v already_current=%v err=%v",
			envelope,
			alreadyCurrent,
			err,
		)
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
}

func TestDeviceBoundReconnectRecoversAlreadyPromotedCurrent(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	oldPending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	oldCurrent, err := store.PromotePending(
		context.Background(),
		oldPending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	replacement := productionCloudTestEnvelope()
	replacement.Generation = strings.Repeat("c", 32)
	replacement.ClientID = "http://127.0.0.1:43125/ha-nova"
	replacement.RefreshToken = "replacement-refresh"
	replacement.RefreshTokenID = "replacement-token"
	newPending, err := store.CreatePending(
		context.Background(),
		replacement,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	newCurrent, err := store.PromotePending(
		context.Background(),
		newPending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	oldMetadata := cloudMetadataFromEnvelope(origin, oldCurrent)
	newMetadata := cloudMetadataFromEnvelope(origin, newCurrent)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateDeviceBoundOrPaired,
			Current: &oldMetadata,
			Pending: &newMetadata,
		},
	}

	resetProductionCloudPolicies(backend)
	envelope, alreadyCurrent, err := resumableCloudEnvelope(
		context.Background(),
		store,
		cfg,
		origin,
		SecretStoreForbidUI,
	)
	if err != nil || !alreadyCurrent ||
		envelope.Generation != newCurrent.Generation {
		t.Fatalf(
			"promoted resume = %+v already_current=%v err=%v",
			envelope,
			alreadyCurrent,
			err,
		)
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
}

func TestPromoteCloudAuthorizationClearsInterruptedPendingDeletion(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	failDelete := true
	backend.fail = func(op, service string) error {
		if failDelete && op == "delete" &&
			service == oauthSecretPendingService {
			failDelete = false
			return errors.New("simulated pending delete failure")
		}
		return nil
	}
	if _, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreForbidUI,
	); err == nil {
		t.Fatal("promotion interruption was not injected")
	}
	backend.fail = nil

	current, err := promoteCloudAuthorization(
		context.Background(),
		store,
		pending.Generation,
		pending,
	)
	if err != nil || current.Generation != pending.Generation {
		t.Fatalf("promotion recovery current=%+v err=%v", current, err)
	}
	if _, exists, err := store.LoadPending(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf("promotion recovery left pending: exists=%v err=%v", exists, err)
	}
}

func TestFreshCloudAuthorizationNeverOpensBrowserBeforeOriginProof(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	proofErr := errors.New("origin proof failed")
	oldProof := verifyCloudOriginForOAuth
	oldBrowser := openCloudOAuthBrowserForSetup
	verifyCloudOriginForOAuth = func(context.Context, CloudOrigin) error {
		return proofErr
	}
	browserOpened := false
	openCloudOAuthBrowserForSetup = func(context.Context, string) error {
		browserOpened = true
		return nil
	}
	t.Cleanup(func() {
		verifyCloudOriginForOAuth = oldProof
		openCloudOAuthBrowserForSetup = oldBrowser
	})

	_, _, err = authorizeOrRefreshCloud(
		context.Background(),
		cloudSetupRequest{Config: runtimeConfig{ProfileID: "profile-1"}},
		origin,
		store,
		OAuthSecretEnvelope{},
	)
	if !errors.Is(err, proofErr) {
		t.Fatalf("authorization error = %v", err)
	}
	if browserOpened {
		t.Fatal("browser opened before the Cloud origin was proved")
	}
}
