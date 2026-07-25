package main

import (
	"context"
	"testing"
)

func TestProductionUnlockPreflightStartsCurrentSlotWithoutUI(t *testing.T) {
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
			State:   cloudStateReady,
			Current: &metadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	var accessedService string
	backend.fail = func(op, service string) error {
		if op == "get" {
			accessedService = service
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
		t.Fatalf("unlock preflight: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if accessedService != oauthSecretCurrentService ||
		len(backend.operations) != 1 ||
		backend.operations[0] != "get" ||
		len(backend.policies) != 1 ||
		backend.policies[0] != SecretStoreForbidUI {
		t.Fatalf(
			"selected preflight service=%q operations=%v policies=%v",
			accessedService,
			backend.operations,
			backend.policies,
		)
	}
}

func TestProductionSetupPreflightChecksPendingWithoutUnneededUI(t *testing.T) {
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
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pending)
	cfg := runtimeConfig{
		ProfileID: "profile-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateTokenStored,
			Pending: &metadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	var accessedServices []string
	backend.fail = func(op, service string) error {
		if op == "get" {
			accessedServices = append(accessedServices, service)
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
		t.Fatalf("setup preflight: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(accessedServices) != 3 ||
		accessedServices[0] != oauthSecretPendingService ||
		accessedServices[1] != oauthSecretCurrentService ||
		accessedServices[2] != oauthSecretRetiringService ||
		len(backend.operations) != 3 ||
		backend.operations[0] != "get" ||
		backend.operations[1] != "get" ||
		backend.operations[2] != "get" ||
		len(backend.policies) != 3 ||
		backend.policies[0] != SecretStoreForbidUI ||
		backend.policies[1] != SecretStoreForbidUI ||
		backend.policies[2] != SecretStoreForbidUI {
		t.Fatalf(
			"pending preflight services=%v operations=%v policies=%v",
			accessedServices,
			backend.operations,
			backend.policies,
		)
	}
}

func TestProductionSetupPreflightFindsAlreadyPromotedCurrent(t *testing.T) {
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
			State:   cloudStateDeviceBoundOrPaired,
			Pending: &metadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	var accessedServices []string
	backend.fail = func(op, service string) error {
		if op == "get" {
			accessedServices = append(accessedServices, service)
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
		t.Fatalf("post-promotion setup preflight: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(accessedServices) != 3 ||
		accessedServices[0] != oauthSecretPendingService ||
		accessedServices[1] != oauthSecretCurrentService ||
		accessedServices[2] != oauthSecretRetiringService ||
		len(backend.operations) != 3 ||
		backend.operations[0] != "get" ||
		backend.operations[1] != "get" ||
		backend.operations[2] != "get" ||
		len(backend.policies) != 3 ||
		backend.policies[0] != SecretStoreForbidUI ||
		backend.policies[1] != SecretStoreForbidUI ||
		backend.policies[2] != SecretStoreForbidUI {
		t.Fatalf(
			"post-promotion preflight services=%v operations=%v policies=%v",
			accessedServices,
			backend.operations,
			backend.policies,
		)
	}
}

func TestProductionUnlockPreflightPrefersResumablePendingOverCurrent(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	firstPending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.PromotePending(
		context.Background(),
		firstPending.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	nextEnvelope := productionCloudTestEnvelope()
	nextEnvelope.Generation = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	pending, err := store.CreatePending(
		context.Background(),
		nextEnvelope,
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
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateTokenStored,
			Current: &currentMetadata,
			Pending: &pendingMetadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	var accessedServices []string
	serviceReads := map[string]int{}
	backend.fail = func(op, service string) error {
		if op == "get" {
			accessedServices = append(accessedServices, service)
			serviceReads[service]++
			if serviceReads[service] == 1 {
				return newCloudError(
					CloudErrSecretUIForbidden,
					"test locked selected secret",
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
		t.Fatalf("resumable unlock preflight: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(accessedServices) != 6 ||
		accessedServices[0] != oauthSecretPendingService ||
		accessedServices[1] != oauthSecretPendingService ||
		accessedServices[2] != oauthSecretCurrentService ||
		accessedServices[3] != oauthSecretCurrentService ||
		accessedServices[4] != oauthSecretRetiringService ||
		accessedServices[5] != oauthSecretRetiringService ||
		len(backend.policies) != 6 ||
		backend.policies[0] != SecretStoreForbidUI ||
		backend.policies[1] != SecretStoreAllowUI ||
		backend.policies[2] != SecretStoreForbidUI ||
		backend.policies[3] != SecretStoreAllowUI ||
		backend.policies[4] != SecretStoreForbidUI ||
		backend.policies[5] != SecretStoreAllowUI {
		t.Fatalf(
			"resumable unlock services=%v operations=%v policies=%v",
			accessedServices,
			backend.operations,
			backend.policies,
		)
	}
}

func TestProductionPendingPreflightAllowsExactlyOneUnlockRetry(t *testing.T) {
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
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pending)
	cfg := runtimeConfig{
		ProfileID: "profile-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateTokenStored,
			Pending: &metadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	pendingReads := 0
	backend.fail = func(op, service string) error {
		if op == "get" && service == oauthSecretPendingService {
			pendingReads++
			if pendingReads == 1 {
				return newCloudError(
					CloudErrSecretUIForbidden,
					"test locked pending secret",
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
		t.Fatalf("locked pending preflight: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if pendingReads != 2 ||
		len(backend.operations) != 4 ||
		len(backend.policies) != 4 ||
		backend.policies[0] != SecretStoreForbidUI ||
		backend.policies[1] != SecretStoreAllowUI ||
		backend.policies[2] != SecretStoreForbidUI ||
		backend.policies[3] != SecretStoreForbidUI {
		t.Fatalf(
			"locked pending reads=%d operations=%v policies=%v",
			pendingReads,
			backend.operations,
			backend.policies,
		)
	}
}

func installProductionCloudPreflightStore(
	t *testing.T,
	profileID string,
	store OAuthSecretStore,
) {
	t.Helper()
	oldPromptSession := cloudInteractivePromptSessionForSetup
	oldStore := newCloudSecretStoreForCLI
	cloudInteractivePromptSessionForSetup = func() bool { return true }
	newCloudSecretStoreForCLI = func(gotProfileID string) (OAuthSecretStore, error) {
		if gotProfileID != profileID {
			t.Fatalf("secret-store profile = %q", gotProfileID)
		}
		return store, nil
	}
	t.Cleanup(func() {
		cloudInteractivePromptSessionForSetup = oldPromptSession
		newCloudSecretStoreForCLI = oldStore
	})
}
