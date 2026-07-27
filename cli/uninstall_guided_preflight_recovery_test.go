package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestGuidedPreflightRejectsMissingCloudDeviceBeforeHomeAssistantRemoval(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg, store := readyCloudPurgeProfile(
		t,
		"profile-cloud-missing-device",
		"relay-cloud-missing-device",
	)
	cfg.HAURL = "http://ha.local:8123"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	oldStore := newCloudSecretStoreForCLI
	newCloudSecretStoreForCLI = func(profileID string) (OAuthSecretStore, error) {
		if profileID != cfg.ProfileID {
			t.Fatalf("unexpected profile id %q", profileID)
		}
		return store, nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = oldStore
	})

	err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "current Cloud device credential") ||
		!strings.Contains(err.Error(), "before Home Assistant removal") {
		t.Fatalf("missing Cloud device preflight error = %v", err)
	}
}

func TestGuidedPreflightRejectsIncompletePendingDeviceEndpoint(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	if err := saveConfig(paths, runtimeConfig{
		ProfileID:            "profile-incomplete-pending",
		HAURL:                "http://ha.local:8123",
		RelayBaseURL:         "http://relay:8791",
		RoutePolicy:          routePolicyLocal,
		PendingSecureBaseURL: "https://relay:8792",
	}); err != nil {
		t.Fatal(err)
	}
	if err := writePendingDeviceCredential(validCredential(143)); err != nil {
		t.Fatal(err)
	}

	err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "pending device endpoint") ||
		!strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete pending endpoint preflight error = %v", err)
	}
}

func TestGuidedPreflightRejectsConflictingMigrationCleanupBeforeRemoval(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	if err := saveConfig(paths, runtimeConfig{
		ProfileID:    "profile-migration-conflict",
		HAURL:        "http://ha.local:8123",
		RelayBaseURL: "http://relay:8791",
		RoutePolicy:  routePolicyLocal,
	}); err != nil {
		t.Fatal(err)
	}
	service := deviceCredentialServiceForProfile(defaultServerProfileName)
	if err := deviceSecretFileSet(service, validCredential(144)); err != nil {
		t.Fatal(err)
	}
	if err := secretKeyringSet(
		service,
		secretUser(),
		validCredential(145),
	); err != nil {
		t.Fatal(err)
	}
	if err := writeKeyringDeviceCredentialCleanup([]string{service}); err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceFileBackendMarker(); err != nil {
		t.Fatal(err)
	}

	err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "differs from its file copy") ||
		!strings.Contains(err.Error(), "before Home Assistant removal") {
		t.Fatalf("migration conflict preflight error = %v", err)
	}
	if _, exists, readErr := readKeyringDeviceCredentialCleanup(); readErr != nil ||
		!exists {
		t.Fatalf(
			"migration checkpoint changed: exists=%v err=%v",
			exists,
			readErr,
		)
	}
}

func TestRetirementPrecleanRelockFailsClosedAndKeepsRetryConfig(t *testing.T) {
	paths, _, _ := pendingRetirementUninstallFixture(t)
	oldRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		return nil
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForRetire = oldRevoke
	})
	err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err != nil {
		t.Fatal(err)
	}
	deviceCredentialPreflight = func() error {
		return errors.New("keyring relocked after retirement preclean")
	}
	if err := finalizeLocalUninstallWithProgressUnlocked(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		nil,
		nil,
	); err == nil ||
		!strings.Contains(err.Error(), "keyring relocked") {
		t.Fatalf("relocked purge error = %v", err)
	}
	if _, err := os.Stat(paths.ConfigFile); err != nil {
		t.Fatalf("retry config was removed after keyring failure: %v", err)
	}
}

func TestCredentialRecreatedAfterPrecleanIsPurged(t *testing.T) {
	paths, _, _ := pendingRetirementUninstallFixture(t)
	oldRetireRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		return nil
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForRetire = oldRetireRevoke
	})
	if err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	); err != nil {
		t.Fatal(err)
	}
	recreated := validCredential(142)
	if err := writeDeviceCredential(recreated); err != nil {
		t.Fatal(err)
	}
	if err := finalizeLocalUninstallWithProgressUnlocked(
		paths,
		installState{},
		&uninstallReport{},
		uninstallModePurge,
		nil,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := readDeviceCredential(); err != nil || exists {
		t.Fatalf(
			"recreated credential survived purge: exists=%v err=%v",
			exists,
			err,
		)
	}
}
