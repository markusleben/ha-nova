package main

import (
	"context"
	"errors"
	"testing"
)

func TestCommittedPreflightFallsThroughMissingRetiringToCurrent(t *testing.T) {
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
	current, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, current)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateCommitted,
			Current: &metadata,
			Pending: &metadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	resetProductionCloudPolicies(backend)
	if err := preflightCloudSecretAccess(
		context.Background(),
		productionCloudSetupCoordinator{},
		cfg,
		cloudSecretPreflightUnlock,
	); err != nil {
		t.Fatalf("committed preflight: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.operations) != 2 ||
		backend.operations[0] != "get" ||
		backend.operations[1] != "get" ||
		len(backend.policies) != 2 ||
		backend.policies[0] != SecretStoreForbidUI ||
		backend.policies[1] != SecretStoreForbidUI {
		t.Fatalf(
			"committed preflight operations=%v policies=%v",
			backend.operations,
			backend.policies,
		)
	}
}

func TestCommittedPreflightUnlocksCurrentWhenRetiringAlreadyGone(t *testing.T) {
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
	current, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, current)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateCommitted,
			Current: &metadata,
			Pending: &metadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	currentReads := 0
	backend.fail = func(op, service string) error {
		if op == "get" && service == oauthSecretCurrentService {
			currentReads++
			if currentReads == 1 {
				return newCloudError(
					CloudErrSecretUIForbidden,
					"test locked current",
					nil,
				)
			}
		}
		return nil
	}
	resetProductionCloudPolicies(backend)
	if err := preflightCloudSecretAccess(
		context.Background(),
		productionCloudSetupCoordinator{},
		cfg,
		cloudSecretPreflightUnlock,
	); err != nil {
		t.Fatalf("committed current unlock: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if currentReads != 2 ||
		len(backend.policies) != 3 ||
		backend.policies[0] != SecretStoreForbidUI ||
		backend.policies[1] != SecretStoreForbidUI ||
		backend.policies[2] != SecretStoreAllowUI {
		t.Fatalf(
			"committed current reads=%d operations=%v policies=%v",
			currentReads,
			backend.operations,
			backend.policies,
		)
	}
}

func TestCommittedPreflightAllowsPreviousHAUserToRetire(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	oldEnvelope := productionCloudTestEnvelope()
	oldEnvelope.HAUserID = "user-old"
	oldPending, err := store.CreatePending(
		context.Background(),
		oldEnvelope,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	oldCurrent, err := store.PromotePending(
		context.Background(),
		oldPending.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	newEnvelope := productionCloudTestEnvelope()
	newEnvelope.Generation = "dddddddddddddddddddddddddddddddd"
	newEnvelope.ClientID = "http://127.0.0.1:43126/ha-nova"
	newEnvelope.RefreshToken = "new-user-refresh"
	newEnvelope.RefreshTokenID = "new-user-token"
	newEnvelope.HAUserID = "user-new"
	newPending, err := store.CreatePending(
		context.Background(),
		newEnvelope,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	newCurrent, err := store.PromotePending(
		context.Background(),
		newPending.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	newMetadata := cloudMetadataFromEnvelope(origin, newCurrent)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateCommitted,
			Current: &newMetadata,
			Pending: &newMetadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	resetProductionCloudPolicies(backend)
	if err := preflightCloudSecretAccess(
		context.Background(),
		productionCloudSetupCoordinator{},
		cfg,
		cloudSecretPreflightUnlock,
	); err != nil {
		t.Fatalf("cross-user committed preflight: %v", err)
	}
	var revoked OAuthSecretEnvelope
	if err := store.RevokeRetiring(
		context.Background(),
		oldCurrent.Generation,
		SecretStoreForbidUI,
		func(_ context.Context, envelope OAuthSecretEnvelope) error {
			revoked = envelope
			return nil
		},
	); err != nil {
		t.Fatalf("retire previous user: %v", err)
	}
	if revoked.HAUserID != "user-old" {
		t.Fatalf("retired HA user = %q", revoked.HAUserID)
	}
}

func TestPendingPreflightAcceptsInterruptedPromotionAndTouchesRetiring(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	oldPending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	oldCurrent, err := store.PromotePending(
		context.Background(),
		oldPending.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	replacement := productionCloudTestEnvelope()
	replacement.Generation = "cccccccccccccccccccccccccccccccc"
	replacement.ClientID = "http://127.0.0.1:43125/ha-nova"
	replacement.RefreshToken = "replacement-refresh"
	replacement.RefreshTokenID = "replacement-token"
	newPending, err := store.CreatePending(
		context.Background(),
		replacement,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	backend.fail = func(op, service string) error {
		if op == "delete" && service == oauthSecretPendingService {
			return errors.New("interrupted pending deletion")
		}
		return nil
	}
	if _, err := store.PromotePending(
		context.Background(),
		newPending.Generation,
		SecretStoreForbidUI,
	); err == nil {
		t.Fatal("promotion interruption was not injected")
	}
	backend.fail = nil

	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	oldMetadata := cloudMetadataFromEnvelope(origin, oldCurrent)
	newMetadata := cloudMetadataFromEnvelope(origin, newPending)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateDeviceBoundOrPaired,
			Current: &oldMetadata,
			Pending: &newMetadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	var services []string
	backend.fail = func(op, service string) error {
		if op == "get" {
			services = append(services, service)
		}
		return nil
	}
	resetProductionCloudPolicies(backend)
	if err := preflightCloudSecretAccess(
		context.Background(),
		productionCloudSetupCoordinator{},
		cfg,
		cloudSecretPreflightSetup,
	); err != nil {
		t.Fatalf("interrupted promotion preflight: %v", err)
	}
	if len(services) != 3 ||
		services[0] != oauthSecretPendingService ||
		services[1] != oauthSecretCurrentService ||
		services[2] != oauthSecretRetiringService {
		t.Fatalf("interrupted promotion services=%v", services)
	}
}
