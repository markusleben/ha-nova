package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func verifiedStorageRecoveryConfig() runtimeConfig {
	cfg := hybridCheckpointUXConfig(cloudStateReady, true)
	cfg.Cloud.Pending = nil
	cfg.Cloud.RecoveryHold = &cloudRecoveryHold{
		Code:            cloudProblemSecureStorage,
		Remediation:     cloudRemediationVerifyState,
		StorageVerified: true,
	}
	return cfg
}

func TestCloudRemoveRawDeviceRelockReturnsStatusToUnlock(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths, _ := saveHybridCheckpointUXProfile(
		t,
		"cabin",
		verifiedStorageRecoveryConfig(),
	)
	previousRead := readPendingDeviceCredentialForCloudRemove
	readPendingDeviceCredentialForCloudRemove = func(
		context.Context,
		SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		return pendingDeviceCredentialRecord{},
			false,
			errDesktopKeyringLocked
	}
	t.Cleanup(func() {
		readPendingDeviceCredentialForCloudRemove = previousRead
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(
			paths,
			[]string{"--server", "cabin", "--yes"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudRemediationUnlockStorage)) {
		t.Fatalf("remove exit=%d output=%s", exit, output)
	}
	exit, output = captureCommandOutput(t, func() int {
		return runCloudStatusCommand(
			paths,
			[]string{"--server", "cabin", "--json"},
		)
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatalf("status JSON=%q: %v", output, err)
	}
	if exit != 1 || summary.NextCommand != cloudUnlockCommand() {
		t.Fatalf("status exit=%d summary=%+v", exit, summary)
	}
}

func TestCloudUnlockHealthRelockResetsStorageProof(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths, _ := saveHybridCheckpointUXProfile(
		t,
		"cabin",
		verifiedStorageRecoveryConfig(),
	)
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandCoordinator(
		t,
		successfulCloudCoordinatorForTest(),
	)
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return errDesktopKeyringLocked
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			paths,
			[]string{"--server", "cabin"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudRemediationUnlockStorage)) {
		t.Fatalf("unlock exit=%d output=%s", exit, output)
	}
	requireStorageProofReset(t, paths, "health relock")
}

func TestCloudUnlockDevicePreflightRelockResetsStorageProof(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths, _ := saveHybridCheckpointUXProfile(
		t,
		"cabin",
		verifiedStorageRecoveryConfig(),
	)
	installCloudCommandPromptSession(t, true)
	previousRead := readCloudPendingDeviceForSetup
	readCloudPendingDeviceForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		return pendingDeviceCredentialRecord{},
			false,
			errDesktopKeyringLocked
	}
	t.Cleanup(func() {
		readCloudPendingDeviceForSetup = previousRead
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			paths,
			[]string{"--server", "cabin"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudRemediationUnlockStorage)) {
		t.Fatalf("unlock exit=%d output=%s", exit, output)
	}
	requireStorageProofReset(t, paths, "device preflight relock")
}

func requireStorageProofReset(
	t *testing.T,
	paths runtimePaths,
	phase string,
) {
	t.Helper()
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.RecoveryHold == nil ||
		saved.Cloud.RecoveryHold.StorageVerified {
		t.Fatalf("%s kept storage proof: %+v", phase, saved.Cloud)
	}
}
