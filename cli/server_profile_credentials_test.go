package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// Per-profile credential-slot coverage (multi-server support): slot naming,
// isolation between profiles, fail-closed legacy fallback, profile-aware
// purge/canary, and the all-profiles keyring→file migration.

const testProfileCredentialB = "hanova-dev-v1.CCCCCCCCCCCCCCCCCCCCCC.DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"

func TestDeviceCredentialSlotNamesPerProfile(t *testing.T) {
	if got := deviceCredentialServiceForProfile("default"); got != "ha-nova.device-credential" {
		t.Fatalf("default current slot = %q", got)
	}
	if got := deviceCredentialPendingServiceForProfile("default"); got != "ha-nova.device-credential.pending" {
		t.Fatalf("default pending slot = %q", got)
	}
	if got := deviceCredentialServiceForProfile("cabin"); got != "ha-nova.device-credential.cabin" {
		t.Fatalf("cabin current slot = %q", got)
	}
	if got := deviceCredentialPendingServiceForProfile("cabin"); got != "ha-nova.device-credential.pending.cabin" {
		t.Fatalf("cabin pending slot = %q", got)
	}

	currentCases := map[string]bool{
		"ha-nova.device-credential":               true,
		"ha-nova.device-credential.cabin":         true,
		"ha-nova.device-credential.pending":       false,
		"ha-nova.device-credential.pending.cabin": false,
		"ha-nova.device-credential.probe":         false,
		"ha-nova.relay-auth-token":                false,
	}
	for service, want := range currentCases {
		if got := isCurrentDeviceCredentialSlotService(service); got != want {
			t.Errorf("isCurrentDeviceCredentialSlotService(%q) = %v, want %v", service, got, want)
		}
	}
}

func TestCredentialSlotIsolationAcrossProfiles(t *testing.T) {
	resetServerProfileSelection(t)
	dir := t.TempDir()
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", dir)

	defaultCred := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(defaultCred); err != nil {
		t.Fatal(err)
	}
	setActiveServerProfile("cabin")
	if err := writeDeviceCredential(testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	if err := writePendingDeviceCredential(testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	// Default keeps its legacy slot name; cabin gets the suffixed one.
	if _, err := os.Stat(filepath.Join(dir, "ha-nova.device-credential")); err != nil {
		t.Fatalf("default slot file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ha-nova.device-credential.cabin")); err != nil {
		t.Fatalf("cabin slot file missing: %v", err)
	}
	if got, ok, err := readDeviceCredential(); err != nil || !ok || got != testProfileCredentialB {
		t.Fatalf("cabin read = %q ok=%v err=%v", got, ok, err)
	}
	setActiveServerProfile(defaultServerProfileName)
	if got, ok, err := readDeviceCredential(); err != nil || !ok || got != defaultCred {
		t.Fatalf("default read = %q ok=%v err=%v", got, ok, err)
	}

	// Purging cabin never touches default's slots.
	setActiveServerProfile("cabin")
	original := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(_, _, _ string) error { return nil }
	t.Cleanup(func() { revokeSelfDeviceV1ForUninstall = original })
	report := &uninstallReport{}
	purgeDeviceCredentialWithReport("https://cabin:18792", "pin", report, false)
	if _, ok, _ := readDeviceCredential(); ok {
		t.Fatal("cabin credential must be purged")
	}
	if !strings.Contains(strings.Join(report.removed, "\n"), `server "cabin"`) {
		t.Fatalf("purge report must name the profile, got: %v", report.removed)
	}
	setActiveServerProfile(defaultServerProfileName)
	if got, ok, err := readDeviceCredential(); err != nil || !ok || got != defaultCred {
		t.Fatalf("default credential lost by cabin purge: ok=%v err=%v", ok, err)
	}
}

func TestNonDefaultProfileFailsClosedWithoutDevicePairing(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	setActiveServerProfile("cabin")

	cfg := runtimeConfig{RelayBaseURL: "http://cabin:8791"}
	_, _, _, _, err := relayFunctionalTransport(cfg)
	if err == nil {
		t.Fatal("a non-default profile without device credentials must fail closed")
	}
	if !strings.Contains(err.Error(), `server profile "cabin"`) || !strings.Contains(err.Error(), "pair --server cabin") {
		t.Fatalf("fail-closed message must name the profile and the fix, got: %v", err)
	}

	// With a paired credential the same profile uses device mode normally.
	if err := writeDeviceCredential(testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	paired := runtimeConfig{RelayBaseURL: "http://cabin:8791", RelaySecureBaseURL: "https://cabin:18792", RelaySpkiPin: "PIN"}
	base, _, token, deviceMode, err := relayFunctionalTransport(paired)
	if err != nil || !deviceMode || base != "https://cabin:18792" || token != testProfileCredentialB {
		t.Fatalf("device mode on cabin failed: base=%q deviceMode=%v err=%v", base, deviceMode, err)
	}
}

func TestPurgeAllProfilesRevokesEachAgainstItsOwnEndpoint(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())

	if err := writeDeviceCredential(generateTestDeviceCredential(t)); err != nil {
		t.Fatal(err)
	}
	setActiveServerProfile("cabin")
	if err := writeDeviceCredential(testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	setActiveServerProfile(defaultServerProfileName)

	var revokedAt []string
	original := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(base, _, _ string) error {
		revokedAt = append(revokedAt, base)
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForUninstall = original })

	report := &uninstallReport{}
	purgeAllDeviceCredentialsWithReport([]profilePurgeTarget{
		{name: "default", secureBaseURL: "https://ha:18792", spkiPin: "pin"},
		{name: "cabin", secureBaseURL: "https://cabin:18792", spkiPin: "pin"},
	}, report, false)

	if len(revokedAt) != 2 || revokedAt[0] != "https://ha:18792" || revokedAt[1] != "https://cabin:18792" {
		t.Fatalf("revokes must go to each profile's own endpoint, got %v", revokedAt)
	}
	for _, profile := range []string{"default", "cabin"} {
		if _, ok, _ := readCredentialSlot(deviceCredentialServiceForProfile(profile)); ok {
			t.Fatalf("profile %q credential must be purged", profile)
		}
	}
}

func TestFileResidueMarkerRemovedOnlyWhenLastProfileSlotGone(t *testing.T) {
	withDeviceStorageTestHome(t)
	// First current-slot write commits the machine-wide marker.
	if err := deviceSecretFileSet(deviceCredentialServiceForProfile("default"), "cred-a"); err != nil {
		t.Fatal(err)
	}
	if err := deviceSecretFileSet(deviceCredentialServiceForProfile("cabin"), "cred-b"); err != nil {
		t.Fatal(err)
	}
	if !deviceFileBackendMarkerExists() {
		t.Fatal("precondition: marker must exist")
	}

	removeDeviceFileStorageResidueForProfile("cabin")
	if !deviceFileBackendMarkerExists() {
		t.Fatal("marker must survive while another profile's slot files remain")
	}
	if _, err := deviceSecretFileGet(deviceCredentialServiceForProfile("default")); err != nil {
		t.Fatalf("default slot must survive a cabin cleanup: %v", err)
	}

	removeDeviceFileStorageResidueForProfile("default")
	if deviceFileBackendMarkerExists() {
		t.Fatal("marker must go once the last profile's slots are gone")
	}
	if dir, err := deviceSecretFileDir(); err == nil {
		if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
			t.Fatalf("secrets dir must be removed when empty, stat err=%v", statErr)
		}
	}
}

func TestFileStorageCanaryChecksTargetProfileSlots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission semantics")
	}
	resetServerProfileSelection(t)
	withDeviceStorageTestHome(t)
	setActiveServerProfile("cabin")

	// An unwritable DEFAULT slot must not fail cabin's canary…
	if err := deviceSecretFileSet(deviceCredentialServiceForProfile("default"), "cred-a"); err != nil {
		t.Fatal(err)
	}
	defaultPath, err := deviceSecretFilePath(deviceCredentialServiceForProfile("default"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(defaultPath, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(defaultPath, 0o600) })
	if err := fileStorageCanary(); err != nil {
		t.Fatalf("cabin canary must ignore default's slots: %v", err)
	}

	// …but an unwritable CABIN slot must.
	if err := deviceSecretFileSet(deviceCredentialServiceForProfile("cabin"), "cred-b"); err != nil {
		t.Fatal(err)
	}
	cabinPath, err := deviceSecretFilePath(deviceCredentialServiceForProfile("cabin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cabinPath, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cabinPath, 0o600) })
	if err := fileStorageCanary(); err == nil {
		t.Fatal("cabin canary must fail on cabin's unwritable slot")
	}
}

func TestKeyringToFileMigrationMovesAllProfilesBeforeMarkerCommit(t *testing.T) {
	resetServerProfileSelection(t)
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	cabinService := deviceCredentialServiceForProfile("cabin")
	t.Cleanup(func() { _ = keyring.Delete(cabinService, secretUser()) })

	defaultCred := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(defaultCred); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(cabinService, secretUser(), testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	// The cabin profile is known via the active-profile seam.
	setActiveServerProfile("cabin")

	migrated, err := migrateKeyringDeviceCredentialToFile()
	if err != nil || !migrated {
		t.Fatalf("migration must run: migrated=%v err=%v", migrated, err)
	}
	if !deviceFileBackendMarkerExists() {
		t.Fatal("migration must commit the marker")
	}
	if got, err := deviceSecretFileGet(deviceCredentialServiceForProfile("default")); err != nil || strings.TrimSpace(got) != defaultCred {
		t.Fatalf("default credential file = %q err=%v", got, err)
	}
	if got, err := deviceSecretFileGet(cabinService); err != nil || strings.TrimSpace(got) != testProfileCredentialB {
		t.Fatalf("cabin credential file = %q err=%v", got, err)
	}
	if _, err := keyring.Get(deviceCredentialService, secretUser()); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("default keyring copy must be gone, err=%v", err)
	}
	if _, err := keyring.Get(cabinService, secretUser()); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("cabin keyring copy must be gone, err=%v", err)
	}
}

func TestPairServerNewProfileWithoutRelayURLIsHardError(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	dir := t.TempDir()
	paths := runtimePaths{ConfigDir: dir, ConfigFile: filepath.Join(dir, "config.json")}
	if err := saveConfig(paths, runtimeConfig{HAHost: "ha", HAURL: "http://ha:8123", RelayBaseURL: "http://ha:8791"}); err != nil {
		t.Fatal(err)
	}

	original := runSecurePairingForPairCmd
	runSecurePairingForPairCmd = func(_, _ string, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		t.Fatal("pairing must not start for a new profile without --relay-url")
		return "", nil
	}
	t.Cleanup(func() { runSecurePairingForPairCmd = original })

	// The saved default relay URL must NOT bootstrap the new profile.
	if rc := runPairCommand(paths, []string{"--server", "cabin", "--code", "123456"}); rc != 1 {
		t.Fatalf("rc = %d, want 1", rc)
	}
	// Invalid profile names are rejected at creation.
	if rc := runPairCommand(paths, []string{"--server", "Bad_Name", "--code", "123456"}); rc != 1 {
		t.Fatalf("invalid name rc = %d, want 1", rc)
	}
}

func TestPairServerCreatesProfileAndLeavesDefaultUntouched(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	dir := t.TempDir()
	paths := runtimePaths{ConfigDir: dir, ConfigFile: filepath.Join(dir, "config.json")}
	if err := saveConfig(paths, runtimeConfig{HAHost: "ha", HAURL: "http://ha:8123", RelayBaseURL: "http://ha:8791", ClientInstallID: "inst-abc"}); err != nil {
		t.Fatal(err)
	}

	original := runSecurePairingForPairCmd
	runSecurePairingForPairCmd = func(_, _ string, cfg *runtimeConfig, save func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		cfg.RelaySecureBaseURL = "https://cabin:18792"
		cfg.RelaySpkiPin = "PINB"
		return "dev-cabin", save(cfg)
	}
	t.Cleanup(func() { runSecurePairingForPairCmd = original })

	if rc := runPairCommand(paths, []string{"--server", "cabin", "--relay-url", "http://cabin:8791", "--code", "123456"}); rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}

	resetServerProfileSelection(t)
	cfgDefault, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("default profile after cabin pair: %v", err)
	}
	if cfgDefault.RelayBaseURL != "http://ha:8791" || cfgDefault.RelaySecureBaseURL != "" {
		t.Fatalf("default profile mutated by cabin pair: %+v", cfgDefault)
	}
	setServerSelectionOverride("cabin")
	cfgCabin, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("cabin profile: %v", err)
	}
	if cfgCabin.RelayBaseURL != "http://cabin:8791" || cfgCabin.RelaySecureBaseURL != "https://cabin:18792" {
		t.Fatalf("cabin profile = %+v", cfgCabin)
	}
	if cfgCabin.ClientInstallID != "inst-abc" {
		t.Fatalf("cabin must inherit the install-wide client_install_id, got %q", cfgCabin.ClientInstallID)
	}
}
