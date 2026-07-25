package main

import (
	"context"
	"testing"
)

func TestCloudRemovePersistsAmbiguousHoldAndRetryStillRemoves(t *testing.T) {
	paths, store, backend, _ := cloudRemoveCommandFixture(t)
	revokeErr := newCloudError(
		CloudErrOAuthOutcomeUnknown,
		"ambiguous removal revocation",
		nil,
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			return revokeErr
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 {
		t.Fatalf("ambiguous remove exit=%d output=%s", exit, output)
	}
	held, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if held.Cloud == nil ||
		held.Cloud.RecoveryHold == nil ||
		held.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("ambiguous remove did not persist hold: %+v", held.Cloud)
	}

	revokeErr = nil
	exit, output = captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("held remove retry exit=%d output=%s", exit, output)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud != nil {
		t.Fatalf("held remove retry left Cloud state: %+v", saved.Cloud)
	}
}
