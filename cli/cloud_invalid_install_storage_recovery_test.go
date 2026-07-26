package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInvalidInstallIdentityLockedStorageRecoveryProgresses(
	t *testing.T,
) {
	paths, store, backend, _ := cloudRemoveCommandFixture(t)
	corruptClientInstallID(t, paths)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			return nil
		},
	)
	resetProductionCloudPolicies(backend)
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
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudRemediationUnlockStorage)) {
		t.Fatalf("locked remove exit=%d output=%s", exit, output)
	}
	exit, output = captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatal(err)
	}
	if exit != 1 ||
		summary.NextCommand !=
			"ha-nova cloud unlock --server default" {
		t.Fatalf("locked status exit=%d summary=%+v", exit, summary)
	}

	readPendingDeviceCredentialForCloudRemove = previousRead
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandCoordinator(
		t,
		successfulCloudCoordinatorForTest(),
	)
	exit, output = captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			paths,
			[]string{"--server", "default"},
		)
	})
	if exit != 0 ||
		!strings.Contains(
			output,
			"Continue verified cleanup",
		) {
		t.Fatalf("unlock exit=%d output=%s", exit, output)
	}
	exit, output = captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatal(err)
	}
	if exit != 1 ||
		summary.NextCommand !=
			"ha-nova cloud remove --server default" {
		t.Fatalf("verified status exit=%d summary=%+v", exit, summary)
	}
	exit, output = captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("final remove exit=%d output=%s", exit, output)
	}
	recovered, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Config.Cloud != nil ||
		recovered.Config.ClientInstallID !=
			" invalid install id " {
		t.Fatalf("final recovery config=%+v", recovered.Config)
	}
}
