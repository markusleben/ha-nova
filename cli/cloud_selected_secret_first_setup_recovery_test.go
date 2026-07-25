package main

import (
	"context"
	"errors"
	"testing"
)

func TestFirstSetupPendingPreflightUnlocksInterruptedCurrentWrite(t *testing.T) {
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
			return errors.New("interrupted pending deletion")
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
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pending)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateDeviceBoundOrPaired,
			Pending: &metadata,
		},
	}

	installProductionCloudPreflightStore(t, cfg.ProfileID, store)
	var services []string
	currentReads := 0
	backend.fail = func(op, service string) error {
		if op == "get" {
			services = append(services, service)
		}
		if op == "get" && service == oauthSecretCurrentService {
			currentReads++
			if currentReads == 1 {
				return newCloudError(
					CloudErrSecretUIForbidden,
					"test locked interrupted current",
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
		t.Fatalf("first-setup interrupted preflight: %v", err)
	}
	wantServices := []string{
		oauthSecretPendingService,
		oauthSecretCurrentService,
		oauthSecretCurrentService,
		oauthSecretRetiringService,
	}
	if len(services) != len(wantServices) || currentReads != 2 {
		t.Fatalf(
			"first-setup services=%v current_reads=%d",
			services,
			currentReads,
		)
	}
	for index := range wantServices {
		if services[index] != wantServices[index] {
			t.Fatalf(
				"first-setup service[%d]=%q want %q",
				index,
				services[index],
				wantServices[index],
			)
		}
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	wantPolicies := []SecretStoreUIPolicy{
		SecretStoreForbidUI,
		SecretStoreForbidUI,
		SecretStoreAllowUI,
		SecretStoreForbidUI,
	}
	if len(backend.policies) != len(wantPolicies) {
		t.Fatalf("first-setup policies=%v", backend.policies)
	}
	for index := range wantPolicies {
		if backend.policies[index] != wantPolicies[index] {
			t.Fatalf(
				"first-setup policy[%d]=%v want %v",
				index,
				backend.policies[index],
				wantPolicies[index],
			)
		}
	}
}
