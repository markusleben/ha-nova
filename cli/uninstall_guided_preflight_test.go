package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestGuidedPreflightInspectsLaterCloudSlotAfterRetiringExists(
	t *testing.T,
) {
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg, store := readyCloudPurgeProfile(
		t,
		"profile-cloud-later-slot",
		"relay-cloud-later-slot",
	)
	backend, ok := store.backend.(*memoryOAuthSecretBackend)
	if !ok {
		t.Fatalf("unexpected OAuth backend %T", store.backend)
	}
	pendingEnvelope := testOAuthEnvelope(
		"new-cloud-refresh",
		"token-new-cloud-refresh",
		"user-1",
		cfg.RelayInstanceID,
	)
	pending, err := store.CreatePending(
		context.Background(),
		pendingEnvelope,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(current.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, current)
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &metadata,
	}
	backend.values[oauthSecretCurrentService+"\x00"+cfg.ProfileID] = "{broken"
	backend.operations = nil
	oldStore := newCloudSecretStoreForCLI
	newCloudSecretStoreForCLI = func(profileID string) (OAuthSecretStore, error) {
		if profileID != cfg.ProfileID {
			t.Fatalf("unexpected profile id %q", profileID)
		}
		return store, nil
	}
	t.Cleanup(func() { newCloudSecretStoreForCLI = oldStore })

	err = validateUninstallSecureStorageBeforeGuidedTeardown(
		paths,
		[]cloudPurgeTarget{{
			profileName: "default",
			profileID:   cfg.ProfileID,
			config:      cfg,
		}},
		nil,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "missing or inconsistent") {
		t.Fatalf("guided Cloud preflight error = %v", err)
	}
	for _, operation := range backend.operations {
		if operation != "get" {
			t.Fatalf(
				"guided preflight mutated before later-slot validation: %v",
				backend.operations,
			)
		}
	}
}

func TestGuidedPreflightBlocksStandardRetirementBeforeRevoke(t *testing.T) {
	paths, _, _ := pendingRetirementUninstallFixture(t)
	revokeCalls := 0
	oldRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForRetire = oldRevoke
	})

	err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModeStandard,
	)
	if err == nil || !strings.Contains(err.Error(), "choose Full purge") {
		t.Fatalf("standard preflight error = %v", err)
	}
	if revokeCalls != 0 {
		t.Fatalf("standard preflight revokes=%d", revokeCalls)
	}
	if _, exists, readErr :=
		readDeviceCredentialRetirementCheckpoint(paths); readErr != nil ||
		!exists {
		t.Fatalf("checkpoint changed: exists=%v err=%v", exists, readErr)
	}
}

func TestGuidedPreflightValidatesCorruptSiblingBeforeAnyRevoke(t *testing.T) {
	paths, _, _ := pendingRetirementUninstallFixture(t)
	raw := `{
		"schema_version":3,
		"default_server":"default",
		"servers":{
			"default":{
				"profile_id":"profile-1",
				"ha_url":"http://ha.local:8123",
				"relay_base_url":"http://relay:8791",
				"route_policy":"local"
			},
			"zeta":{
				"profile_id":"profile-zeta",
				"relay_base_url":"http://zeta:8791",
				"route_policy":"local"
			}
		}
	}`
	if err := os.WriteFile(paths.ConfigFile, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := deviceCredentialRetirementCheckpointPathForProfile(
		paths,
		"zeta",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	revokeCalls := 0
	oldRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForRetire = oldRevoke
	})

	err = prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err == nil ||
		!strings.Contains(err.Error(), `server "zeta"`) {
		t.Fatalf("corrupt sibling preflight error = %v", err)
	}
	if revokeCalls != 0 {
		t.Fatalf("valid default retired before corrupt sibling: %d", revokeCalls)
	}
}

func TestGuidedPreflightRejectsDuplicateCloudIdentityBeforeAnyRevoke(
	t *testing.T,
) {
	paths, _, _ := pendingRetirementUninstallFixture(t)
	raw := `{
		"schema_version":3,
		"default_server":"default",
		"servers":{
			"default":{
				"profile_id":"profile-shared",
				"ha_url":"http://ha.local:8123",
				"relay_base_url":"http://relay:8791",
				"route_policy":"local",
				"cloud":{"state":"authorizing"}
			},
			"cabin":{
				"profile_id":"profile-shared",
				"relay_base_url":"http://cabin:8791",
				"route_policy":"local",
				"cloud":{"state":"authorizing"}
			}
		}
	}`
	if err := os.WriteFile(paths.ConfigFile, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	revokeCalls := 0
	oldRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForRetire = oldRevoke
	})

	err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err == nil || !strings.Contains(err.Error(), "share profile_id") {
		t.Fatalf("duplicate Cloud identity preflight error = %v", err)
	}
	if revokeCalls != 0 {
		t.Fatalf("retirement ran before Cloud identity validation: %d", revokeCalls)
	}
}

func TestGuidedPreflightPreservesExactLockedKeyringError(t *testing.T) {
	paths, _, _ := pendingRetirementUninstallFixture(t)
	locked := desktopKeyringLockedError(
		"test native secure storage is locked",
	)
	deviceCredentialPreflight = func() error {
		return locked
	}
	revokeCalls := 0
	oldRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForRetire = oldRevoke
	})

	err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err == nil || !strings.Contains(err.Error(), locked.Error()) {
		if err == nil ||
			!strings.Contains(err.Error(), "Unlock the default keyring") {
			t.Fatalf("locked-keyring preflight error = %v", err)
		}
	}
	if strings.Contains(err.Error(), "does not match") {
		t.Fatalf("locked keyring was mislabeled as checkpoint mismatch: %v", err)
	}
	if revokeCalls != 0 {
		t.Fatalf("locked keyring reached revoke: %d", revokeCalls)
	}
}

func TestGuidedPreflightRejectsOrdinaryDeviceKeyringLock(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceCredential(validCredential(141)); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{
		ProfileID:            "profile-device-lock",
		RelayInstanceID:      "relay-device-lock",
		HAURL:                "http://ha.local:8123",
		RelayBaseURL:         "http://relay:8791",
		RelaySecureBaseURL:   "https://relay:8792",
		RelaySpkiPin:         "pin",
		RoutePolicy:          routePolicyLocal,
		ClientInstallID:      "install-device-lock",
		PendingSecureBaseURL: "",
		PendingSpkiPin:       "",
	}); err != nil {
		t.Fatal(err)
	}
	deviceCredentialPreflight = func() error {
		return desktopKeyringLockedError("ordinary device keyring relocked")
	}

	err = prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "Unlock the default keyring") {
		t.Fatalf("ordinary device keyring preflight error = %v", err)
	}
}

func TestGuidedPreflightRejectsCloudKeyringLock(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg, store := readyCloudPurgeProfile(
		t,
		"profile-cloud-lock",
		"relay-cloud-lock",
	)
	cfg.HAURL = "http://ha.local:8123"
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
				"test Cloud keyring lock",
				nil,
			)
		}
		return nil
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
		!strings.Contains(err.Error(), "native secure storage is locked") {
		t.Fatalf("Cloud keyring preflight error = %v", err)
	}
}
