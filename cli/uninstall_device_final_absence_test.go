package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fullPurgeCredentialConfig = `{
	"schema_version":3,
	"default_server":"default",
	"servers":{
		"default":{
			"profile_id":"profile-default",
			"relay_secure_base_url":"https://default.local:8792",
			"relay_spki_pin":"pin-default",
			"route_policy":"local"
		}
	}
}`

const fullPurgeEmptyCredentialConfig = `{
	"schema_version":3,
	"default_server":"default",
	"servers":{
		"default":{
			"profile_id":"profile-default",
			"route_policy":"local"
		}
	}
}`

func TestFullPurgeRejectsRecreatedUninventoriedRawCredential(
	t *testing.T,
) {
	for _, service := range []string{
		deviceCredentialServiceForProfile("orphan"),
		deviceCredentialPendingServiceForProfile("orphan"),
	} {
		t.Run(service, func(t *testing.T) {
			paths := setupServerCommandTest(
				t,
				fullPurgeEmptyCredentialConfig,
			)
			targets, err := collectProfilePurgeTargets(paths)
			if err != nil {
				t.Fatal(err)
			}
			rawPath, err := deviceSecretFilePath(service)
			if err != nil {
				t.Fatal(err)
			}
			replacement := validCredential(221)
			previousHook := profilePurgeFinalProofHook
			profilePurgeFinalProofHook = func() error {
				if err := os.MkdirAll(
					filepath.Dir(rawPath),
					0o700,
				); err != nil {
					return err
				}
				return os.WriteFile(
					rawPath,
					[]byte(replacement),
					0o600,
				)
			}
			t.Cleanup(func() {
				profilePurgeFinalProofHook = previousHook
			})
			err = purgeAllDeviceCredentialsWithReport(
				targets,
				&uninstallReport{},
				nil,
			)
			if err == nil ||
				!strings.Contains(
					err.Error(),
					"reappeared after full cleanup",
				) {
				t.Fatalf("full purge error = %v", err)
			}
			raw, readErr := os.ReadFile(rawPath)
			if readErr != nil || string(raw) != replacement {
				t.Fatalf(
					"replacement=%q err=%v",
					raw,
					readErr,
				)
			}
		})
	}
}

func TestFullUninstallPreservesConfigWhenCredentialReappears(
	t *testing.T,
) {
	paths := setupServerCommandTest(
		t,
		fullPurgeCredentialConfig,
	)
	original := validCredential(222)
	if err := secretSet(
		deviceCredentialService,
		original,
	); err != nil {
		t.Fatal(err)
	}
	stubServerRevoke(t)
	replacement := validCredential(223)
	err := finalizeLocalUninstallWithProgress(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		func(step string) error {
			if step != "config_cleanup" {
				return nil
			}
			return secretSet(
				deviceCredentialService,
				replacement,
			)
		},
		false,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"reappeared before config cleanup",
		) {
		t.Fatalf("full uninstall error = %v", err)
	}
	if _, statErr := os.Stat(paths.ConfigFile); statErr != nil {
		t.Fatalf("config inventory was removed: %v", statErr)
	}
	actual, readErr := secretGet(deviceCredentialService)
	if readErr != nil || actual != replacement {
		t.Fatalf(
			"replacement=%q err=%v",
			actual,
			readErr,
		)
	}
}

func TestFullUninstallPreservesChangedConfigBeforeFinalCleanup(
	t *testing.T,
) {
	paths := setupServerCommandTest(
		t,
		fullPurgeEmptyCredentialConfig,
	)
	stubServerRevoke(t)
	changed := `{
		"schema_version":3,
		"default_server":"default",
		"servers":{
			"default":{
				"profile_id":"profile-default",
				"route_policy":"local"
			},
			"cabin":{
				"profile_id":"profile-cabin",
				"route_policy":"local"
			}
		}
	}`
	err := finalizeLocalUninstallWithProgress(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		func(step string) error {
			if step != "config_cleanup" {
				return nil
			}
			return os.WriteFile(
				paths.ConfigFile,
				[]byte(changed),
				0o600,
			)
		},
		false,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"configuration changed before config cleanup",
		) {
		t.Fatalf("full uninstall error = %v", err)
	}
	data, readErr := os.ReadFile(paths.ConfigFile)
	if readErr != nil ||
		!strings.Contains(string(data), `"cabin"`) {
		t.Fatalf(
			"changed config was not preserved: %q err=%v",
			data,
			readErr,
		)
	}
}

func TestFullUninstallAtomicCleanupPreservesLastMomentConfigChange(
	t *testing.T,
) {
	paths := setupServerCommandTest(
		t,
		fullPurgeEmptyCredentialConfig,
	)
	expected, expectedExists, err := readOptionalFile(paths.ConfigFile)
	if err != nil || !expectedExists {
		t.Fatalf("config snapshot exists=%v error=%v", expectedExists, err)
	}
	changed := []byte(`{
		"schema_version":3,
		"default_server":"default",
		"servers":{
			"default":{
				"profile_id":"profile-default",
				"route_policy":"local"
			},
			"cabin":{
				"profile_id":"profile-cabin",
				"route_policy":"local"
			}
		}
	}`)
	previousHook := beforeUninstallConfigSnapshotRemoval
	beforeUninstallConfigSnapshotRemoval = func(path string) error {
		return os.WriteFile(path, changed, 0o600)
	}
	t.Cleanup(func() {
		beforeUninstallConfigSnapshotRemoval = previousHook
	})

	err = removeManagedConfigArtifactsAtSnapshot(
		paths,
		&uninstallReport{},
		expected,
		true,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"configuration changed before config cleanup",
		) {
		t.Fatalf("atomic cleanup error = %v", err)
	}
	actual, readErr := os.ReadFile(paths.ConfigFile)
	if readErr != nil || string(actual) != string(changed) {
		t.Fatalf(
			"last-moment config=%q error=%v",
			actual,
			readErr,
		)
	}
}

func TestFullUninstallDeviceRelockResetsCloudStorageProof(
	t *testing.T,
) {
	paths := setupServerCommandTest(
		t,
		fullPurgeEmptyCredentialConfig,
	)
	snapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg := snapshot.Config
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
	store, err := NewOAuthSecretStore(
		newMemoryOAuthSecretBackend(),
		cfg.ProfileID,
	)
	if err != nil {
		t.Fatal(err)
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
	previousHook := profilePurgeFinalProofHook
	profilePurgeFinalProofHook = func() error {
		return errDesktopKeyringLocked
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = previousStore
		profilePurgeFinalProofHook = previousHook
	})

	err = finalizeLocalUninstallWithProgress(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		nil,
		false,
	)
	if err == nil ||
		!strings.Contains(err.Error(), errDesktopKeyringLocked.Error()) {
		t.Fatalf("full uninstall error = %v", err)
	}
	saved, loadErr := loadCloudRecoverySnapshotUnchecked(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.Config.Cloud == nil ||
		saved.Config.Cloud.RecoveryHold == nil ||
		saved.Config.Cloud.RecoveryHold.StorageVerified {
		t.Fatalf(
			"purge relock kept storage proof: %+v",
			saved.Config.Cloud,
		)
	}
}

func TestFullUninstallPurgeRemovesUnreadableCredentialSlots(
	t *testing.T,
) {
	for _, slot := range []string{"current", "pending"} {
		t.Run(slot, func(t *testing.T) {
			paths := setupServerCommandTest(
				t,
				fullPurgeCredentialConfig,
			)
			if slot == "pending" {
				if err := secretSet(
					deviceCredentialService,
					validCredential(224),
				); err != nil {
					t.Fatal(err)
				}
			}
			service := deviceCredentialService
			if slot == "pending" {
				service = deviceCredentialPendingService
			}
			if err := secretSet(service, "malformed"); err != nil {
				t.Fatal(err)
			}
			stubServerRevoke(t)
			report := &uninstallReport{}
			if err := finalizeLocalUninstallWithProgress(
				paths,
				installState{},
				report,
				uninstallModePurge,
				nil,
				false,
			); err != nil {
				t.Fatalf("full uninstall: %v", err)
			}
			if _, err := os.Stat(paths.ConfigFile); !os.IsNotExist(err) {
				t.Fatalf("config remains after purge: %v", err)
			}
			if _, err := secretGet(service); err != errSecretNotFound {
				t.Fatalf(
					"malformed %s slot remains: %v",
					slot,
					err,
				)
			}
			if notes := strings.Join(report.notes, "\n"); !strings.Contains(
				notes,
				"unreadable and was removed without revoking",
			) {
				t.Fatalf("missing honest report: %q", notes)
			}
		})
	}
}
