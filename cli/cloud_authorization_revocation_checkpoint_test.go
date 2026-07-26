package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAuthorizationRevocationCheckpointPersistsWithoutToken(
	t *testing.T,
) {
	paths, store, _, current := cloudRemoveCommandFixture(t)
	snapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := inspectCloudAuthorizationCleanup(
		context.Background(),
		snapshot.Config,
		store,
	)
	if err != nil {
		t.Fatal(err)
	}
	checkpointed, _, err :=
		checkpointCloudAuthorizationRevocationUnlocked(
			paths,
			expected,
			plan,
			false,
		)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := checkpointed.Cloud.AuthorizationRevocationCompleted
	if checkpoint == nil ||
		checkpoint.Current == nil ||
		!checkpoint.Current.matches(current) {
		t.Fatalf("authorization checkpoint = %+v", checkpoint)
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), current.RefreshToken) {
		t.Fatal("authorization checkpoint persisted the refresh token")
	}

	remaining, err := inspectCloudAuthorizationCleanup(
		context.Background(),
		checkpointed,
		store,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !remaining.revocationCheckpointed {
		t.Fatal("persisted checkpoint did not suppress remote retry")
	}
}

func TestAuthorizationRevocationCheckpointRejectsReplacementToken(
	t *testing.T,
) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	snapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := inspectCloudAuthorizationCleanup(
		context.Background(),
		snapshot.Config,
		store,
	)
	if err != nil {
		t.Fatal(err)
	}
	checkpointed, _, err :=
		checkpointCloudAuthorizationRevocationUnlocked(
			paths,
			expected,
			plan,
			false,
		)
	if err != nil {
		t.Fatal(err)
	}
	replacement := current
	replacement.RefreshToken = "different-refresh-secret"
	encoded, err := json.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	backend.values[oauthSecretCurrentService+"\x00"+current.ProfileID] =
		string(encoded)
	backend.mu.Unlock()
	if _, err := inspectCloudAuthorizationCleanup(
		context.Background(),
		checkpointed,
		store,
	); err == nil ||
		!IsCloudErrorCode(err, CloudErrIdentityMismatch) {
		t.Fatalf("replacement token checkpoint validation error = %v", err)
	}
}

func TestOwnerConfirmedAuthorizationRevocationCheckpointCanBeEmpty(
	t *testing.T,
) {
	checkpoint := newCloudAuthorizationRevocationCheckpoint(
		cloudAuthorizationCleanupPlan{},
		true,
	)
	if checkpoint == nil ||
		!checkpoint.OwnerConfirmedAllRemoteAccessRevoked {
		t.Fatalf("Owner-confirmed checkpoint = %+v", checkpoint)
	}
	if err := validateCloudAuthorizationRevocationCheckpoint(
		checkpoint,
	); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizationRevocationCheckpointRawShapeRejectsUnknownFields(
	t *testing.T,
) {
	for name, raw := range map[string]json.RawMessage{
		"checkpoint": json.RawMessage(`{
			"owner_confirmed_all_remote_access_revoked":true,
			"unexpected":true
		}`),
		"slot": json.RawMessage(`{
			"owner_confirmed_all_remote_access_revoked":true,
			"current":{"unexpected":true}
		}`),
	} {
		if err := validateKnownAuthorizationRevocationCheckpointShape(
			raw,
		); err == nil {
			t.Fatalf(
				"unknown %s field was accepted",
				name,
			)
		}
	}
}

func TestCloudStatusDoesNotUseRevokedAuthorizationCheckpoint(
	t *testing.T,
) {
	paths, store, _, _ := cloudRemoveCommandFixture(t)
	snapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := inspectCloudAuthorizationCleanup(
		context.Background(),
		snapshot.Config,
		store,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err :=
		checkpointCloudAuthorizationRevocationUnlocked(
			paths,
			expected,
			plan,
			false,
		); err != nil {
		t.Fatal(err)
	}
	oldVerify := verifyCloudDeviceHealthForCommand
	verifyCalls := 0
	verifyCloudDeviceHealthForCommand = func(
		context.Context,
		runtimeConfig,
	) error {
		verifyCalls++
		return nil
	}
	t.Cleanup(func() {
		verifyCloudDeviceHealthForCommand = oldVerify
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	if exit != 1 ||
		!strings.Contains(output, `"status":"cleanup_pending"`) ||
		!strings.Contains(output, `"next_command":"ha-nova cloud remove`) {
		t.Fatalf("checkpointed status exit=%d output=%s", exit, output)
	}
	if verifyCalls != 0 {
		t.Fatalf("checkpointed status made %d remote calls", verifyCalls)
	}
}
