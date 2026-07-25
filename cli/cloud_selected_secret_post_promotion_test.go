package main

import (
	"context"
	"testing"
)

func TestProductionPostPromotionPreflightUnlocksRetiringSlot(t *testing.T) {
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
	newEnvelope := productionCloudTestEnvelope()
	newEnvelope.Generation = "cccccccccccccccccccccccccccccccc"
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

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	var accessedServices []string
	retiringReads := 0
	backend.fail = func(op, service string) error {
		if op == "get" {
			accessedServices = append(accessedServices, service)
		}
		if op == "get" && service == oauthSecretRetiringService {
			retiringReads++
			if retiringReads == 1 {
				return newCloudError(
					CloudErrSecretUIForbidden,
					"test locked retiring secret",
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
		t.Fatalf("post-promotion retiring preflight: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	wantServices := []string{
		oauthSecretPendingService,
		oauthSecretCurrentService,
		oauthSecretRetiringService,
		oauthSecretRetiringService,
	}
	if len(accessedServices) != len(wantServices) ||
		retiringReads != 2 ||
		len(backend.policies) != 4 {
		t.Fatalf(
			"post-promotion retiring services=%v policies=%v",
			accessedServices,
			backend.policies,
		)
	}
	for index := range wantServices {
		if accessedServices[index] != wantServices[index] {
			t.Fatalf(
				"post-promotion service[%d]=%q want %q",
				index,
				accessedServices[index],
				wantServices[index],
			)
		}
	}
	wantPolicies := []SecretStoreUIPolicy{
		SecretStoreForbidUI,
		SecretStoreForbidUI,
		SecretStoreForbidUI,
		SecretStoreAllowUI,
	}
	for index := range wantPolicies {
		if backend.policies[index] != wantPolicies[index] {
			t.Fatalf(
				"post-promotion policy[%d]=%v want %v",
				index,
				backend.policies[index],
				wantPolicies[index],
			)
		}
	}
}
