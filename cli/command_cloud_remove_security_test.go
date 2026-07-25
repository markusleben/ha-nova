package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCloudRemoveKeepsConfigAndSecretWhenRevocationFails(t *testing.T) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	revokeFailure := newCloudError(
		CloudErrNetwork,
		"revoke test authorization",
		errors.New("offline"),
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			return revokeFailure
		},
	)
	resetProductionCloudPolicies(backend)
	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	if !strings.Contains(output, "Cloud configuration was kept") ||
		!strings.Contains(output, string(cloudProblemUnavailable)) {
		t.Fatalf("missing fail-closed removal guidance:\n%s", output)
	}
	if strings.Contains(output, current.RefreshToken) {
		t.Fatal("removal failure exposed the refresh token")
	}
	held, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if held.Cloud == nil ||
		held.Cloud.Current == nil ||
		held.Cloud.RecoveryHold != nil {
		t.Fatalf("retryable network failure changed Cloud state: %+v", held.Cloud)
	}
	remaining, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists || remaining.Generation != current.Generation {
		t.Fatalf(
			"failed revocation lost current secret: exists=%v current=%+v err=%v",
			exists,
			remaining,
			err,
		)
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
}

func TestCloudRemoveKeepsOutcomeUnknownCheckpointWithoutNativeSlot(
	t *testing.T,
) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	if err := store.DeleteCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	metadata := *cfg.Cloud.Current
	cfg.RoutePolicy = routePolicyLocal
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateAuthorizing,
		Pending: &metadata,
		RecoveryHold: &cloudRecoveryHold{
			Code:        cloudProblemAuthorization,
			Remediation: cloudRemediationSecurityStop,
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	revokeCalled := false
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			revokeCalled = true
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 ||
		!strings.Contains(output, "revoke HA NOVA sessions") {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	if revokeCalled {
		t.Fatal("cleanup attempted revocation without the configured credential")
	}
	held, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if held.Cloud == nil ||
		held.Cloud.Pending == nil ||
		held.Cloud.Pending.CredentialGeneration != current.Generation ||
		held.Cloud.RecoveryHold == nil ||
		held.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("ambiguous authorization checkpoint changed: %+v", held.Cloud)
	}
}
