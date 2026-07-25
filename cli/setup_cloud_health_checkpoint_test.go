package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func installSetupCloudHealthVerifier(
	t *testing.T,
	verify func(context.Context, runtimeConfig) error,
) {
	t.Helper()
	old := verifyCloudDeviceHealthForSetup
	verifyCloudDeviceHealthForSetup = verify
	t.Cleanup(func() { verifyCloudDeviceHealthForSetup = old })
}

func TestCloudOnlyWizardPersistsSecurityHealthFailure(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)
	installSetupCloudHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return newCloudError(
				CloudErrRelayInstance,
				"verify final Wizard health",
				nil,
			)
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runInteractiveCloudOnlySetup(
			bufio.NewReader(strings.NewReader(
				productionCloudTestOrigin+"\n123456\n",
			)),
			os.Stdout,
			paths,
			runtimeConfig{},
			&installState{},
			"",
			nil,
			nil,
		)
	})
	if exit != 1 || coordinator.remoteCalls != 1 {
		t.Fatalf(
			"Wizard exit=%d remoteCalls=%d output=%s",
			exit,
			coordinator.remoteCalls,
			output,
		)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.RecoveryHold == nil ||
		saved.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("Wizard security failure was not held: %+v", saved.Cloud)
	}
}

func TestCloudOnlyWizardDoesNotFinishChangedSuccessfulHealth(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)
	installSetupCloudHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			top := readTestConfigTopLevel(t, paths)
			var servers map[string]json.RawMessage
			if err := json.Unmarshal(top["servers"], &servers); err != nil {
				return err
			}
			var profile map[string]json.RawMessage
			if err := json.Unmarshal(
				servers[defaultServerProfileName],
				&profile,
			); err != nil {
				return err
			}
			profile["changed_during_health"] = json.RawMessage(`true`)
			updated, err := json.Marshal(profile)
			if err != nil {
				return err
			}
			servers[defaultServerProfileName] = updated
			top["servers"], err = json.Marshal(servers)
			if err != nil {
				return err
			}
			return writeJSONFile(paths.ConfigFile, top, 0o600)
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runInteractiveCloudOnlySetup(
			bufio.NewReader(strings.NewReader(
				productionCloudTestOrigin+"\n123456\n",
			)),
			os.Stdout,
			paths,
			runtimeConfig{},
			&installState{},
			"",
			nil,
			nil,
		)
	})
	if exit != 1 ||
		coordinator.remoteCalls != 1 ||
		strings.Contains(output, "Setup complete") {
		t.Fatalf(
			"stale Wizard exit=%d remoteCalls=%d output=%s",
			exit,
			coordinator.remoteCalls,
			output,
		)
	}
	if _, err := os.Stat(paths.StateFile); !os.IsNotExist(err) {
		t.Fatalf("stale Wizard persisted install state: %v", err)
	}
}

func TestCloudHealthDoesNotVerifyAfterMutationLockTimeout(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-health-busy"
	cfg.RelayInstanceID = "relay-health-busy"
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
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		t.Fatal("could not acquire competing mutation lock")
	}
	verifyCalls := 0
	started := time.Now()
	err = verifyCloudHealthAtSnapshot(
		context.Background(),
		paths,
		snapshot,
		func(context.Context, runtimeConfig) error {
			verifyCalls++
			return newCloudError(
				CloudErrRelayInstance,
				"must not run after lock timeout",
				nil,
			)
		},
		nil,
	)
	release()
	if err == nil ||
		!strings.Contains(err.Error(), "client update is still in progress") ||
		verifyCalls != 0 ||
		time.Since(started) < cloudRecoveryCheckpointLockTimeout {
		t.Fatalf(
			"lock timeout err=%v calls=%d elapsed=%s",
			err,
			verifyCalls,
			time.Since(started),
		)
	}
}

func TestCloudHealthRejectsRemovedSnapshotAfterSuccessfulVerifier(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-health-removed"
	cfg.RelayInstanceID = "relay-health-removed"
	current := cloudMetadataForTest(strings.Repeat("c", 32))
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
	err = verifyCloudHealthAtSnapshot(
		context.Background(),
		paths,
		snapshot,
		func(context.Context, runtimeConfig) error {
			return os.Remove(paths.ConfigFile)
		},
		nil,
	)
	problem := cloudProblemForError(err)
	if problem.Code != cloudProblemIdentityMismatch ||
		problem.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("removed snapshot health error = %v", err)
	}
}

func TestCloudHealthPersistsSecurityFailureAfterMutationLockWait(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-health-wait"
	cfg.RelayInstanceID = "relay-health-wait"
	current := cloudMetadataForTest(strings.Repeat("b", 32))
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
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		t.Fatal("could not acquire competing mutation lock")
	}
	result := make(chan error, 1)
	verifyCalls := make(chan struct{}, 1)
	go func() {
		result <- verifyCloudHealthAtSnapshot(
			context.Background(),
			paths,
			snapshot,
			func(context.Context, runtimeConfig) error {
				verifyCalls <- struct{}{}
				return newCloudError(
					CloudErrRelayInstance,
					"security failure after lock wait",
					nil,
				)
			},
			nil,
		)
	}()
	select {
	case <-verifyCalls:
		release()
		t.Fatal("health verifier ran before mutation lock was released")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	err = <-result
	if !IsCloudErrorCode(err, CloudErrRelayInstance) {
		t.Fatalf("health verification error = %v", err)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.RecoveryHold == nil ||
		saved.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("security failure was not held after lock wait: %+v", saved.Cloud)
	}
}
