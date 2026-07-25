package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCloudAuthorizationCleanupRejectsOutcomeUnknownWithoutNativeSlot(
	t *testing.T,
) {
	cfg, metadata := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	cfg.Cloud.RecoveryHold = &cloudRecoveryHold{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
	}
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())

	_, err := inspectCloudAuthorizationCleanup(
		context.Background(),
		cfg,
		store,
	)
	var problem *cloudProblem
	if !errors.As(err, &problem) ||
		problem.Remediation != cloudRemediationSecurityStop ||
		!strings.Contains(problem.Detail, "revoke HA NOVA sessions") {
		t.Fatalf("cleanup inspection error = %#v", err)
	}
	if metadata.CredentialGeneration == "" {
		t.Fatal("test fixture did not create pending metadata")
	}
}

func TestCloudAuthorizationCleanupRequiresEveryConfiguredGrant(
	t *testing.T,
) {
	for _, state := range []cloudLifecycleState{
		cloudStateAuthorizing,
		cloudStateReady,
		cloudStateTokenStored,
	} {
		t.Run(string(state), func(t *testing.T) {
			cfg, metadata := cloudCleanupTestConfig(t, state)
			if state == cloudStateReady {
				cfg.Cloud.Current = &metadata
				cfg.Cloud.Pending = nil
			}
			store := productionCloudTestStore(
				t,
				newMemoryOAuthSecretBackend(),
			)

			_, err := inspectCloudAuthorizationCleanup(
				context.Background(),
				cfg,
				store,
			)
			var problem *cloudProblem
			if !errors.As(err, &problem) ||
				problem.Remediation != cloudRemediationSecurityStop {
				t.Fatalf("cleanup inspection error = %#v", err)
			}
		})
	}
}

func TestCloudAuthorizationCleanupReadsLaterSlotsBeforeMutation(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	old := establishProductionCloudCurrent(t, store, "old-refresh")
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelopeWithTokenAndGeneration(
			"new-refresh",
			"fedcba9876543210fedcba9876543210",
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
	origin, err := cloudOriginFromCanonical(current.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, current)
	cfg := runtimeConfig{
		ProfileID:       current.ProfileID,
		RelayInstanceID: current.RelayInstanceID,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	backend.values[oauthSecretCurrentService+"\x00profile-1"] = "{broken"
	backend.operations = nil

	_, err = inspectCloudAuthorizationCleanup(
		context.Background(),
		cfg,
		store,
	)
	if err == nil || !IsCloudErrorCode(err, CloudErrSecretCorrupt) {
		t.Fatalf("cleanup inspection error = %v", err)
	}
	for _, operation := range backend.operations {
		if operation != "get" {
			t.Fatalf("cleanup mutated before later-slot validation: %v", backend.operations)
		}
	}
	if raw := backend.values[oauthSecretRetiringService+"\x00profile-1"]; raw == "" {
		t.Fatalf("retiring generation %q was removed", old.Generation)
	}
}

func TestCloudAuthorizationCleanupRevokesDistinctGrantsWithSameGeneration(
	t *testing.T,
) {
	generation := "0123456789abcdef0123456789abcdef"
	pending := productionCloudTestEnvelopeWithTokenAndGeneration(
		"pending-refresh",
		generation,
	)
	current := productionCloudTestEnvelopeWithTokenAndGeneration(
		"current-refresh",
		generation,
	)
	plan := cloudAuthorizationCleanupPlan{
		pending:    pending,
		hasPending: true,
		current:    current,
		hasCurrent: true,
	}
	originalRevoke := revokeAndVerifyCloudAuthorizationForCLI
	var revoked []string
	revokeAndVerifyCloudAuthorizationForCLI = func(
		_ context.Context,
		envelope OAuthSecretEnvelope,
	) error {
		revoked = append(revoked, envelope.RefreshToken)
		return nil
	}
	t.Cleanup(func() {
		revokeAndVerifyCloudAuthorizationForCLI = originalRevoke
	})

	if err := revokeCloudAuthorizationCleanupPlan(
		context.Background(),
		productionCloudTestStore(t, newMemoryOAuthSecretBackend()),
		plan,
	); err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 2 {
		t.Fatalf("revoked grants = %v", revoked)
	}
}

func TestManualCloudAuthorizationCleanupDeletesEveryReadSlot(
	t *testing.T,
) {
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	_ = establishProductionCloudCurrent(t, store, "old-refresh")
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelopeWithTokenAndGeneration(
			"current-refresh",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	current, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists {
		t.Fatalf("current exists=%v err=%v", exists, err)
	}
	pendingEnvelope := current
	pendingEnvelope.State = OAuthSecretPending
	pendingEnvelope.Generation = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	pendingEnvelope.RefreshToken = "pending-refresh"
	if err := store.write(
		context.Background(),
		oauthSecretPendingService,
		pendingEnvelope,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	cfg, _ := cloudCleanupTestConfig(t, cloudStateReady)
	plan, err := inspectCloudAuthorizationCleanup(
		context.Background(),
		cfg,
		store,
	)
	if !errors.Is(err, errCloudAuthorizationCleanupUnverifiable) ||
		!plan.hasCurrent ||
		!plan.hasPending ||
		!plan.hasRetiring {
		t.Fatalf("manual cleanup plan=%+v err=%v", plan, err)
	}
	if err := deleteManuallyRevokedCloudAuthorizationPlan(
		context.Background(),
		store,
		plan,
	); err != nil {
		t.Fatal(err)
	}
	for name, load := range map[string]func(
		context.Context,
		SecretStoreUIPolicy,
	) (OAuthSecretEnvelope, bool, error){
		"current":  store.LoadCurrent,
		"pending":  store.LoadPending,
		"retiring": store.LoadRetiring,
	} {
		_, exists, err := load(
			context.Background(),
			SecretStoreForbidUI,
		)
		if err != nil || exists {
			t.Fatalf("%s exists=%v err=%v", name, exists, err)
		}
	}
}

func cloudCleanupTestConfig(
	t *testing.T,
	state cloudLifecycleState,
) (runtimeConfig, cloudConnectionMetadata) {
	t.Helper()
	envelope := productionCloudTestEnvelope()
	envelope.Generation = "0123456789abcdef0123456789abcdef"
	origin, err := cloudOriginFromCanonical(envelope.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, envelope)
	return runtimeConfig{
		ProfileID:       envelope.ProfileID,
		RelayInstanceID: envelope.RelayInstanceID,
		Cloud: &cloudLifecycleMetadata{
			State:   state,
			Pending: &metadata,
		},
	}, metadata
}

func establishProductionCloudCurrent(
	t *testing.T,
	store *KeyringOAuthSecretStore,
	token string,
) OAuthSecretEnvelope {
	t.Helper()
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelopeWithToken(token),
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
	return current
}

func productionCloudTestEnvelopeWithToken(
	token string,
) OAuthSecretEnvelope {
	return productionCloudTestEnvelopeWithTokenAndGeneration(
		token,
		productionCloudTestGeneration,
	)
}

func productionCloudTestEnvelopeWithTokenAndGeneration(
	token string,
	generation string,
) OAuthSecretEnvelope {
	envelope := productionCloudTestEnvelope()
	envelope.RefreshToken = token
	envelope.RefreshTokenID = "token-" + token
	envelope.Generation = generation
	return envelope
}
