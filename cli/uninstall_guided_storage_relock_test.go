package main

import (
	"strings"
	"testing"
)

func TestGuidedPurgePreflightRelockResetsCloudStorageProof(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg, store := readyCloudPurgeProfile(
		t,
		"profile-guided-relock",
		"relay-guided-relock",
	)
	cfg.HAURL = "http://ha.local:8123"
	cfg.Cloud.RecoveryHold = &cloudRecoveryHold{
		Code:            cloudProblemSecureStorage,
		Remediation:     cloudRemediationVerifyState,
		StorageVerified: true,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	backend, ok := store.backend.(*memoryOAuthSecretBackend)
	if !ok {
		t.Fatalf("unexpected OAuth backend %T", store.backend)
	}
	backend.fail = func(op, _ string) error {
		if op == "get" {
			return newCloudError(
				CloudErrSecretStoreLocked,
				"test guided Cloud keyring relock",
				nil,
			)
		}
		return nil
	}
	previousStore := newCloudSecretStoreForCLI
	newCloudSecretStoreForCLI = func(
		profileID string,
	) (OAuthSecretStore, error) {
		if profileID != cfg.ProfileID {
			t.Fatalf("Cloud store profile=%q", profileID)
		}
		return store, nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = previousStore
	})

	err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			string(cloudRemediationUnlockStorage),
		) {
		t.Fatalf("guided preflight error = %v", err)
	}
	saved, loadErr := loadCloudRecoverySnapshotUnchecked(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.Config.Cloud == nil ||
		saved.Config.Cloud.RecoveryHold == nil ||
		saved.Config.Cloud.RecoveryHold.StorageVerified {
		t.Fatalf(
			"guided relock kept storage proof: %+v",
			saved.Config.Cloud,
		)
	}
}
