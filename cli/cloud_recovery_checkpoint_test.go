package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCloudRecoveryCheckpointSkipsNewerCredentialGeneration(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-generation-race"
	cfg.RelayInstanceID = "relay-generation-race"
	current := cloudMetadataForTest(strings.Repeat("a", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadCloudManagementSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		t.Fatal(err)
	}

	top := readTestConfigTopLevel(t, paths)
	raw := top["servers"]
	replaced := strings.Replace(
		string(raw),
		strings.Repeat("a", 32),
		strings.Repeat("b", 32),
		1,
	)
	top["servers"] = []byte(replaced)
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}

	outcome, err := checkpointCloudRecoveryHold(
		paths,
		expected,
		newCloudError(
			CloudErrOAuthOutcomeUnknown,
			"late failure from generation A",
			nil,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != cloudRecoveryCheckpointSkippedStale {
		t.Fatalf("checkpoint outcome = %q", outcome)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.Current == nil ||
		saved.Cloud.Current.CredentialGeneration != strings.Repeat("b", 32) ||
		saved.Cloud.RecoveryHold != nil {
		t.Fatalf("late generation held newer profile: %+v", saved.Cloud)
	}
}

func TestCloudRecoveryCheckpointSkipsReplacementProfileIdentity(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-old"
	cfg.RelayInstanceID = "relay-old"
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadCloudManagementSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		t.Fatal(err)
	}

	top := readTestConfigTopLevel(t, paths)
	top["servers"] = []byte(strings.Replace(
		string(top["servers"]),
		"profile-old",
		"profile-new",
		1,
	))
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}

	outcome, err := checkpointCloudRecoveryHold(
		paths,
		expected,
		newCloudError(
			CloudErrOAuthOutcomeUnknown,
			"late failure from old identity",
			nil,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != cloudRecoveryCheckpointSkippedStale {
		t.Fatalf("checkpoint outcome = %q", outcome)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ProfileID != "profile-new" ||
		saved.Cloud == nil ||
		saved.Cloud.RecoveryHold != nil {
		t.Fatalf("late identity held replacement profile: %+v", saved)
	}
}

func TestCloudManagementSnapshotNormalizesLegacyReadyState(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-normalized"
	cfg.RelayInstanceID = "relay-normalized"
	current := cloudMetadataForTest(strings.Repeat("c", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	top := readTestConfigTopLevel(t, paths)
	top["servers"] = []byte(strings.Replace(
		string(top["servers"]),
		`"state":"ready",`,
		"",
		1,
	))
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadCloudManagementSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	normalized := snapshot.Config
	if normalized.Cloud == nil || normalized.Cloud.State != cloudStateReady {
		t.Fatalf("legacy lifecycle was not normalized: %+v", normalized.Cloud)
	}
	if _, err := snapshot.recoveryExpectation(); err != nil {
		t.Fatalf("capture rejected equivalent normalized lifecycle: %v", err)
	}
}

func TestCloudHealthCheckpointRejectsUnknownOnlyProfileMutation(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-unknown-race"
	cfg.RelayInstanceID = "relay-unknown-race"
	current := cloudMetadataForTest(strings.Repeat("d", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadCloudManagementSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}

	top := readTestConfigTopLevel(t, paths)
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(
		servers[defaultServerProfileName],
		&profile,
	); err != nil {
		t.Fatal(err)
	}
	profile["future_profile"] = json.RawMessage(`{"keep":true}`)
	servers[defaultServerProfileName], err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	top["servers"], err = json.Marshal(servers)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}

	verifyCalls := 0
	verifyErr := verifyCloudHealthAtSnapshot(
		context.Background(),
		paths,
		snapshot,
		func(context.Context, runtimeConfig) error {
			verifyCalls++
			return newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"late failure from profile A",
				nil,
			)
		},
		nil,
	)
	problem := cloudProblemForError(verifyErr)
	if problem.Remediation != cloudRemediationSecurityStop ||
		verifyCalls != 0 {
		t.Fatalf(
			"verification error = %v calls=%d",
			verifyErr,
			verifyCalls,
		)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil || saved.Cloud.RecoveryHold != nil {
		t.Fatalf("unknown-only replacement received stale hold: %+v", saved.Cloud)
	}
	top = readTestConfigTopLevel(t, paths)
	if !strings.Contains(string(top["servers"]), `"future_profile"`) {
		t.Fatal("unknown-only replacement was overwritten")
	}
}

func TestCloudRecoveryCheckpointWaitsForMutationLock(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-lock-wait"
	cfg.RelayInstanceID = "relay-lock-wait"
	current := cloudMetadataForTest(strings.Repeat("e", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadCloudManagementSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		t.Fatal(err)
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		t.Fatal("could not acquire competing mutation lock")
	}
	result := make(chan error, 1)
	go func() {
		_, checkpointErr := checkpointCloudRecoveryHold(
			paths,
			expected,
			newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"wait for lock",
				nil,
			),
		)
		result <- checkpointErr
	}()
	time.Sleep(30 * time.Millisecond)
	release()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.RecoveryHold == nil ||
		saved.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("checkpoint was lost after lock wait: %+v", saved.Cloud)
	}
}

func TestCloudRecoveryCheckpointRechecksSnapshotAfterLockWait(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-lock-stale"
	cfg.RelayInstanceID = "relay-lock-stale"
	current := cloudMetadataForTest(strings.Repeat("1", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, err := loadCloudManagementSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		t.Fatal(err)
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		t.Fatal("could not acquire competing mutation lock")
	}
	type checkpointResult struct {
		outcome cloudRecoveryCheckpointOutcome
		err     error
	}
	result := make(chan checkpointResult, 1)
	go func() {
		outcome, checkpointErr := checkpointCloudRecoveryHold(
			paths,
			expected,
			newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"stale after lock wait",
				nil,
			),
		)
		result <- checkpointResult{outcome: outcome, err: checkpointErr}
	}()
	time.Sleep(30 * time.Millisecond)
	top := readTestConfigTopLevel(t, paths)
	top["servers"] = json.RawMessage(strings.Replace(
		string(top["servers"]),
		strings.Repeat("1", 32),
		strings.Repeat("2", 32),
		1,
	))
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	got := <-result
	if got.err != nil ||
		got.outcome != cloudRecoveryCheckpointSkippedStale {
		t.Fatalf("checkpoint result = %+v", got)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.Current == nil ||
		saved.Cloud.Current.CredentialGeneration != strings.Repeat("2", 32) ||
		saved.Cloud.RecoveryHold != nil {
		t.Fatalf("stale lock waiter held replacement: %+v", saved.Cloud)
	}
}

func TestCloudRecoveryCheckpointRelockResetsStorageProof(
	t *testing.T,
) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{
			name: "typed Cloud error",
			err: newCloudError(
				CloudErrSecretStoreLocked,
				"relock after verified storage",
				nil,
			),
		},
		{
			name: "raw device keyring error",
			err:  errDesktopKeyringLocked,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(
				t,
				`{"schema_version":1}`,
			)
			cfg := completedLocalCloudTestConfig()
			cfg.ProfileID = "profile-storage-relock"
			cfg.RelayInstanceID = "relay-storage-relock"
			cfg.Cloud = &cloudLifecycleMetadata{
				State: cloudStateAuthorizing,
				RecoveryHold: &cloudRecoveryHold{
					Code:            cloudProblemSecureStorage,
					Remediation:     cloudRemediationVerifyState,
					StorageVerified: true,
				},
			}
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}
			snapshot, err := loadCloudManagementSnapshot(paths)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := snapshot.recoveryExpectation()
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := checkpointCloudRecoveryHold(
				paths,
				expected,
				test.err,
			)
			if err != nil ||
				outcome != cloudRecoveryCheckpointPersisted {
				t.Fatalf(
					"checkpoint outcome=%q error=%v",
					outcome,
					err,
				)
			}
			saved, err := loadSelectedRuntimeConfigUnchecked(paths)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Cloud == nil ||
				saved.Cloud.RecoveryHold == nil ||
				saved.Cloud.RecoveryHold.StorageVerified {
				t.Fatalf(
					"relock did not reset storage proof: %+v",
					saved.Cloud,
				)
			}
		})
	}
}
