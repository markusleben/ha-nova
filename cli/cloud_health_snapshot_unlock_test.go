package main

import (
	"context"
	"strings"
	"testing"
)

type cloudUnlockPreflightMutationCoordinator struct {
	*fakeCloudSetupCoordinator
	mutate func() error
}

func (c *cloudUnlockPreflightMutationCoordinator) Preflight(
	context.Context,
	string,
) error {
	if c.mutate == nil {
		return nil
	}
	return c.mutate()
}

func heldReadyCloudUnlockConfig(
	generation string,
) (runtimeConfig, cloudRecoveryHold) {
	current := cloudMetadataForTest(generation)
	hold := cloudRecoveryHold{
		Code:        cloudProblemSecureStorage,
		Remediation: cloudRemediationVerifyState,
	}
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-unlock-snapshot"
	cfg.RelayInstanceID = "relay-unlock-snapshot"
	cfg.RoutePolicy = routePolicyAutomatic
	cfg.Cloud = &cloudLifecycleMetadata{
		State:        cloudStateReady,
		Current:      &current,
		RecoveryHold: &hold,
	}
	return cfg, hold
}

func nonReadyCloudUnlockConfig(cfg runtimeConfig) runtimeConfig {
	lifecycle := *cfg.Cloud
	lifecycle.State = cloudStateRetiringPrevious
	cfg.Cloud = &lifecycle
	return cfg
}

func assertCloudUnlockDidNotClear(
	t *testing.T,
	paths runtimePaths,
	output string,
) {
	t.Helper()
	if strings.Contains(output, "Cloud access is ready") ||
		strings.Contains(output, "recovery safety hold was cleared") {
		t.Fatalf("unlock accepted non-ready health: %s", output)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.ready() ||
		saved.Cloud.RecoveryHold == nil {
		t.Fatalf("unlock cleared or rewrote non-ready hold: %+v", saved.Cloud)
	}
}

func TestCloudUnlockDoesNotClearHoldChangedDuringSuccessfulHealth(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	current := cloudMetadataForTest(strings.Repeat("7", 32))
	storageHold := cloudRecoveryHold{
		Code:        cloudProblemSecureStorage,
		Remediation: cloudRemediationVerifyState,
	}
	securityHold := cloudRecoveryHold{
		Code:        cloudProblemIdentityMismatch,
		Remediation: cloudRemediationSecurityStop,
	}
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-unlock-stale-success"
	cfg.RelayInstanceID = "relay-unlock-stale-success"
	cfg.RoutePolicy = routePolicyAutomatic
	cfg.Cloud = &cloudLifecycleMetadata{
		State:        cloudStateReady,
		Current:      &current,
		RecoveryHold: &storageHold,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandCoordinator(t, successfulCloudCoordinatorForTest())
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			replacement := cfg
			lifecycle := *replacement.Cloud
			lifecycle.RecoveryHold = &securityHold
			replacement.Cloud = &lifecycle
			return saveConfig(paths, replacement)
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(paths, nil)
	})
	if exit != 1 ||
		strings.Contains(output, "Cloud access is ready") ||
		strings.Contains(output, "recovery safety hold was cleared") {
		t.Fatalf("stale unlock exit=%d output=%s", exit, output)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.RecoveryHold == nil ||
		*saved.Cloud.RecoveryHold != securityHold {
		t.Fatalf("unlock cleared changed hold: %+v", saved.Cloud)
	}
}

func TestCloudUnlockRejectsLifecycleChangedDuringNativePreflight(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg, _ := heldReadyCloudUnlockConfig(strings.Repeat("8", 32))
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	coordinator := &cloudUnlockPreflightMutationCoordinator{
		fakeCloudSetupCoordinator: successfulCloudCoordinatorForTest(),
		mutate: func() error {
			return saveConfig(paths, nonReadyCloudUnlockConfig(cfg))
		},
	}
	installCloudCommandCoordinator(t, coordinator)
	verifyCalls := 0
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			verifyCalls++
			return nil
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(paths, nil)
	})
	if exit != 1 || verifyCalls != 0 {
		t.Fatalf(
			"preflight lifecycle race exit=%d calls=%d output=%s",
			exit,
			verifyCalls,
			output,
		)
	}
	assertCloudUnlockDidNotClear(t, paths, output)
}

func TestCloudUnlockHealthRejectsNonReadyLockedSnapshot(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg, hold := heldReadyCloudUnlockConfig(strings.Repeat("9", 32))
	cfg = nonReadyCloudUnlockConfig(cfg)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	verifyCalls := 0
	_, err := loadAndVerifyCloudHealthWithCheckpoint(
		context.Background(),
		paths,
		func(context.Context, runtimeConfig) error {
			verifyCalls++
			return nil
		},
		&hold,
	)
	if err == nil || verifyCalls != 0 {
		t.Fatalf("non-ready health err=%v calls=%d", err, verifyCalls)
	}
	saved, loadErr := loadSelectedRuntimeConfigUnchecked(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.Cloud == nil ||
		saved.Cloud.ready() ||
		saved.Cloud.RecoveryHold == nil {
		t.Fatalf("non-ready health changed hold: %+v", saved.Cloud)
	}
}

func TestCloudUnlockDoesNotClearLifecycleChangedDuringSuccessfulHealth(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg, _ := heldReadyCloudUnlockConfig(strings.Repeat("a", 32))
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandCoordinator(t, successfulCloudCoordinatorForTest())
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return saveConfig(paths, nonReadyCloudUnlockConfig(cfg))
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(paths, nil)
	})
	if exit != 1 {
		t.Fatalf("health lifecycle race exit=%d output=%s", exit, output)
	}
	assertCloudUnlockDidNotClear(t, paths, output)
}

func TestCloudRecoveryClearIndependentlyRejectsNonReadySnapshot(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg, hold := heldReadyCloudUnlockConfig(strings.Repeat("b", 32))
	cfg = nonReadyCloudUnlockConfig(cfg)
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
	if _, err := clearCloudRecoveryHoldAtSnapshot(
		paths,
		expected,
		hold,
	); err == nil {
		t.Fatal("non-ready exact snapshot cleared recovery hold")
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.ready() ||
		saved.Cloud.RecoveryHold == nil {
		t.Fatalf("independent clear changed non-ready hold: %+v", saved.Cloud)
	}
}
