package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCloudStatusExposesRecoveryWhenInstallIdentityIsInvalid(
	t *testing.T,
) {
	paths, _, _, _ := cloudRemoveCommandFixture(t)
	corruptClientInstallID(t, paths)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatalf("status JSON=%q: %v", output, err)
	}
	if exit != 1 ||
		summary.Status != "recovery_blocked" ||
		summary.Server != defaultServerProfileName ||
		summary.Lifecycle != cloudStateReady ||
		!summary.CurrentAvailable ||
		summary.VerificationError == nil ||
		summary.VerificationError.Code != cloudProblemConfigInvalid ||
		summary.VerificationError.Remediation !=
			cloudRemediationSecurityStop ||
		summary.NextCommand !=
			"ha-nova cloud remove --server default" {
		t.Fatalf("status exit=%d summary=%+v", exit, summary)
	}
}

func TestCloudRemoveRevokesWhilePreservingInvalidInstallIdentity(
	t *testing.T,
) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	corruptClientInstallID(t, paths)
	var revoked []string
	installCloudRemoveStore(
		t,
		store,
		func(_ context.Context, envelope OAuthSecretEnvelope) error {
			revoked = append(revoked, envelope.Generation)
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	if len(revoked) != 1 || revoked[0] != current.Generation {
		t.Fatalf("revoked generations = %v", revoked)
	}
	snapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Config.Cloud != nil ||
		snapshot.Config.ClientInstallID != " invalid install id " {
		t.Fatalf("cleanup result = %+v", snapshot.Config)
	}
	if _, err := loadSelectedRuntimeConfigUnchecked(paths); !errors.Is(
		err,
		errInvalidClientInstallID,
	) {
		t.Fatalf("normal load after cleanup = %v", err)
	}
}

func TestRemoteOnlyCloudRemoveCheckpointsWithInvalidInstallIdentity(
	t *testing.T,
) {
	paths, store, backend, _, _ := remoteOnlyCloudRemovalFixture(t)
	corruptClientInstallID(t, paths)
	deviceRevokes := 0
	installRemoteDeviceRevokeHook(
		t,
		func(
			context.Context,
			runtimeConfig,
			OAuthSecretStore,
			string,
		) error {
			deviceRevokes++
			return nil
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("remote-only remove exit=%d output=%s", exit, output)
	}
	snapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if deviceRevokes != 1 ||
		snapshot.Config.Cloud != nil ||
		snapshot.Config.RelayInstanceID != "" ||
		snapshot.Config.ClientInstallID != " invalid install id " {
		t.Fatalf(
			"device-revokes=%d cleanup=%+v",
			deviceRevokes,
			snapshot.Config,
		)
	}
}

func TestSetupRecoveryKeepsCloudCheckpointVisibleWithInvalidInstallIdentity(
	t *testing.T,
) {
	paths, _, _, _ := cloudRemoveCommandFixture(t)
	corruptClientInstallID(t, paths)
	_, loadErr := loadConfig(paths)
	if !errors.Is(loadErr, errInvalidClientInstallID) {
		t.Fatalf("load error = %v", loadErr)
	}

	recovered, err := recoverSetupConfigAfterLoadError(paths, loadErr)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Cloud == nil ||
		recovered.Cloud.State != cloudStateReady ||
		recovered.ProfileID != "profile-1" {
		t.Fatalf("recovered config = %+v", recovered)
	}
}

func corruptClientInstallID(t *testing.T, paths runtimePaths) {
	t.Helper()
	top := readTestConfigTopLevel(t, paths)
	top["client_install_id"] = json.RawMessage(`" invalid install id "`)
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}
}
