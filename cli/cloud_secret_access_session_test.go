package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCloudSecretAccessSessionReusesPromptedSlot(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	current := establishOAuthCurrent(t, store, "refresh-session")

	backend.mu.Lock()
	backend.operations = nil
	backend.policies = nil
	backend.mu.Unlock()

	ctx := withCloudSecretAccessHolder(context.Background())
	ctx, sessionStore := withCloudSecretAccessSession(
		ctx,
		"default",
		store,
	)
	first, exists, err := sessionStore.LoadCurrent(
		ctx,
		SecretStoreAllowUI,
	)
	if err != nil || !exists || first.Generation != current.Generation {
		t.Fatalf("prompted current = %+v, exists=%v, err=%v", first, exists, err)
	}
	second, exists, err := sessionStore.LoadCurrent(
		ctx,
		SecretStoreForbidUI,
	)
	if err != nil || !exists || second.Generation != current.Generation {
		t.Fatalf("cached current = %+v, exists=%v, err=%v", second, exists, err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.operations) != 1 ||
		backend.operations[0] != "get" ||
		len(backend.policies) != 1 ||
		backend.policies[0] != SecretStoreAllowUI {
		t.Fatalf(
			"native operations=%v policies=%v",
			backend.operations,
			backend.policies,
		)
	}
}

func TestCloudSecretAccessSessionCoversInternalStoreReads(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	current := establishOAuthCurrent(t, store, "refresh-update")

	backend.mu.Lock()
	backend.operations = nil
	backend.policies = nil
	backend.mu.Unlock()

	ctx, sessionStore := withCloudSecretAccessSession(
		context.Background(),
		"default",
		store,
	)
	current, exists, err := sessionStore.LoadCurrent(
		ctx,
		SecretStoreAllowUI,
	)
	if err != nil || !exists {
		t.Fatalf("prompted current: exists=%v err=%v", exists, err)
	}
	expires := time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC)
	current.RefreshTokenExpiresAt = &expires
	if err := sessionStore.UpdateCurrent(
		ctx,
		current,
		current.Generation,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatalf("UpdateCurrent: %v", err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	gets := 0
	for _, operation := range backend.operations {
		if operation == "get" {
			gets++
		}
	}
	if gets != 1 {
		t.Fatalf("native Get operations = %d; all=%v", gets, backend.operations)
	}
}

func TestCloudSecretAccessSessionIsProfileBound(t *testing.T) {
	store := newTestOAuthStore(t, newMemoryOAuthSecretBackend())
	ctx, _ := withCloudSecretAccessSession(
		context.Background(),
		"default",
		store,
	)
	if _, ok := cloudSecretStoreForOperation(ctx, "other"); ok {
		t.Fatal("Cloud secret session crossed profile boundary")
	}
	if got, ok := cloudSecretStoreForOperation(ctx, "default"); !ok ||
		got == nil {
		t.Fatal("Cloud secret session missing for its bound profile")
	}
}

func TestProductionPreflightSessionFlowsIntoRuntimeStore(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	current := establishOAuthCurrent(t, store, "refresh-plumbing")
	metadata := cloudConnectionMetadata{
		Origin:               current.CanonicalOrigin,
		CanonicalOrigin:      current.CanonicalOrigin,
		OAuthClientID:        current.ClientID,
		CredentialGeneration: current.Generation,
		HAUserID:             current.HAUserID,
	}
	cfg := runtimeConfig{
		ProfileID:       "default",
		RelayInstanceID: current.RelayInstanceID,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	originalStore := newCloudSecretStoreForCLI
	originalInteractive := cloudInteractivePromptSessionForSetup
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) {
		return store, nil
	}
	cloudInteractivePromptSessionForSetup = func() bool { return true }
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = originalStore
		cloudInteractivePromptSessionForSetup = originalInteractive
	})
	currentReads := 0
	backend.fail = func(op, service string) error {
		if op == "get" && service == oauthSecretCurrentService {
			currentReads++
			if currentReads == 1 {
				return newCloudError(
					CloudErrSecretUIForbidden,
					"test locked current secret",
					nil,
				)
			}
		}
		return nil
	}
	backend.mu.Lock()
	backend.operations = nil
	backend.policies = nil
	backend.mu.Unlock()

	ctx := withCloudSecretAccessHolder(context.Background())
	ctx, err := preflightCloudSecretAccessSession(
		ctx,
		productionCloudSetupCoordinator{},
		cfg,
		cloudSecretPreflightUnlock,
	)
	if err != nil {
		t.Fatalf("production preflight: %v", err)
	}
	runtimeStore, ok := cloudSecretStoreForOperation(ctx, cfg.ProfileID)
	if !ok {
		t.Fatal("preflight store did not flow into the operation context")
	}
	if _, exists, err := runtimeStore.LoadCurrent(
		ctx,
		SecretStoreForbidUI,
	); err != nil || !exists {
		t.Fatalf("runtime current: exists=%v err=%v", exists, err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.operations) != 2 ||
		backend.operations[0] != "get" ||
		backend.operations[1] != "get" ||
		backend.policies[0] != SecretStoreForbidUI ||
		backend.policies[1] != SecretStoreAllowUI {
		t.Fatalf(
			"native operations=%v policies=%v",
			backend.operations,
			backend.policies,
		)
	}
}

func TestProductionPreflightSessionReadsProbeFromNativeStore(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	backend.dropSet = func(service string) bool {
		return strings.HasPrefix(service, oauthSecretPreflightServicePrefix)
	}
	store := newTestOAuthStore(t, backend)
	originalStore := newCloudSecretStoreForCLI
	originalInteractive := cloudInteractivePromptSessionForSetup
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) {
		return store, nil
	}
	cloudInteractivePromptSessionForSetup = func() bool { return true }
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = originalStore
		cloudInteractivePromptSessionForSetup = originalInteractive
	})

	ctx := withCloudSecretAccessHolder(context.Background())
	_, err := preflightCloudSecretAccessSession(
		ctx,
		productionCloudSetupCoordinator{},
		runtimeConfig{ProfileID: "default"},
		cloudSecretPreflightSetup,
	)
	if !IsCloudErrorCode(err, CloudErrSecretStore) {
		t.Fatalf("non-persisting native Set preflight error = %v", err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.operations) != 3 ||
		backend.operations[0] != "set" ||
		backend.operations[1] != "get" ||
		backend.operations[2] != "delete" {
		t.Fatalf("native preflight operations = %v", backend.operations)
	}
}

func TestCloudSecretAccessSessionNeverMemoizesPreflightProbe(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	backend.dropSet = func(service string) bool {
		return strings.HasPrefix(service, oauthSecretPreflightServicePrefix)
	}
	store := newTestOAuthStore(t, backend)
	ctx, sessionStore := withCloudSecretAccessSession(
		context.Background(),
		"default",
		store,
	)

	err := PreflightOAuthSecretStore(
		ctx,
		sessionStore,
		SecretStoreAllowUI,
	)
	if !IsCloudErrorCode(err, CloudErrSecretStore) {
		t.Fatalf("memoized non-persisting Set preflight error = %v", err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.operations) != 3 ||
		backend.operations[0] != "set" ||
		backend.operations[1] != "get" ||
		backend.operations[2] != "delete" {
		t.Fatalf("memoized native preflight operations = %v", backend.operations)
	}
}

func TestProductionRetirePreviousReusesPreflightSession(t *testing.T) {
	backend := newMemoryOAuthSecretBackend()
	store := newTestOAuthStore(t, backend)
	oldCurrent := establishOAuthCurrent(t, store, "refresh-retiring")
	pending, err := store.CreatePending(
		context.Background(),
		testOAuthEnvelope(
			"refresh-current",
			"token-current",
			"user-current",
			"relay-1",
		),
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
	metadata := cloudConnectionMetadata{
		Origin:               current.CanonicalOrigin,
		CanonicalOrigin:      current.CanonicalOrigin,
		OAuthClientID:        current.ClientID,
		CredentialGeneration: current.Generation,
		HAUserID:             current.HAUserID,
	}
	cfg := runtimeConfig{
		ProfileID:       "default",
		RelayInstanceID: current.RelayInstanceID,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateCommitted,
			Current: &metadata,
		},
	}

	originalStore := newCloudSecretStoreForCLI
	originalInteractive := cloudInteractivePromptSessionForSetup
	originalRevoke := revokeAndVerifyCloudAuthorizationForCLI
	storeCalls := 0
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) {
		storeCalls++
		return store, nil
	}
	cloudInteractivePromptSessionForSetup = func() bool { return true }
	var revoked OAuthSecretEnvelope
	revokeAndVerifyCloudAuthorizationForCLI = func(
		_ context.Context,
		envelope OAuthSecretEnvelope,
	) error {
		revoked = envelope
		return nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = originalStore
		cloudInteractivePromptSessionForSetup = originalInteractive
		revokeAndVerifyCloudAuthorizationForCLI = originalRevoke
	})

	retiringReads := 0
	backend.fail = func(op, service string) error {
		if op == "get" && service == oauthSecretRetiringService {
			retiringReads++
			if retiringReads == 1 {
				return newCloudError(
					CloudErrSecretUIForbidden,
					"test one-time Keychain authorization",
					nil,
				)
			}
		}
		return nil
	}
	backend.mu.Lock()
	backend.operations = nil
	backend.policies = nil
	backend.mu.Unlock()

	ctx := withCloudSecretAccessHolder(context.Background())
	ctx, err = preflightCloudSecretAccessSession(
		ctx,
		productionCloudSetupCoordinator{},
		cfg,
		cloudSecretPreflightSetup,
	)
	if err != nil {
		t.Fatalf("preflight retiring authorization: %v", err)
	}
	if err := (productionCloudSetupCoordinator{}).RetirePrevious(
		ctx,
		cfg.ProfileID,
	); err != nil {
		t.Fatalf("RetirePrevious: %v", err)
	}
	if storeCalls != 1 {
		t.Fatalf("native store constructions = %d, want 1", storeCalls)
	}
	if retiringReads != 2 {
		t.Fatalf("native retiring reads = %d, want preflight retry only", retiringReads)
	}
	if revoked.Generation != oldCurrent.Generation {
		t.Fatalf(
			"revoked generation = %q, want %q",
			revoked.Generation,
			oldCurrent.Generation,
		)
	}
}
