package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Headless service-mode purge (client matrix linux-B): `setup --service`
// against a standalone relay never pairs a device, so no `.file-backend`
// marker exists and the keyring is unreachable.
func setupHeadlessPurgeTest(t *testing.T, cfg runtimeConfig, serviceFile bool) runtimePaths {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	previous := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return errDesktopKeyringSessionUnavailable }
	t.Cleanup(func() { deviceCredentialPreflight = previous })
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg.HAHost = "192.0.2.5"
	cfg.HAURL = "http://192.0.2.5:8123"
	cfg.RelayBaseURL = "http://192.0.2.5:8791"
	if serviceFile {
		cfg.RelayTokenFile = defaultRelayAuthTokenFile(paths)
		if err := writeRelayAuthTokenFile(cfg.RelayTokenFile, "service-token"); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths
}

func runTestPurge(paths runtimePaths) (*uninstallReport, error) {
	report := &uninstallReport{}
	err := finalizeLocalUninstallWithProgress(
		paths, installState{}, report, uninstallModePurge, nil, false,
	)
	return report, err
}

func TestPurgeUnpairedServiceInstallWithoutKeyringRemovesEverything(t *testing.T) {
	paths := setupHeadlessPurgeTest(t, runtimeConfig{}, true)
	report, err := runTestPurge(paths)
	if err != nil {
		t.Fatalf("purge error = %v", err)
	}
	if deviceSecretFileBacked() {
		t.Fatal("purge leaked process-wide file routing")
	}
	for _, path := range []string{paths.ConfigFile, defaultRelayAuthTokenFile(paths)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived purge: %v", path, err)
		}
	}
	if !strings.Contains(strings.Join(report.notes, "\n"), "keyring was not checked for leftover HA NOVA device credentials") {
		t.Fatalf("missing unchecked-keyring note: %q", report.notes)
	}
}

func TestPurgeStillFailsClosedWhenKeyringCredentialMayExist(t *testing.T) {
	cases := map[string]struct {
		cfg         runtimeConfig
		serviceFile bool
	}{
		"paired service install": {
			cfg: runtimeConfig{
				RelaySecureBaseURL: "https://192.0.2.5:8792",
				RelaySpkiPin:       "sha256/pin",
			},
			serviceFile: true,
		},
		"interrupted pairing": {
			cfg: runtimeConfig{
				PendingSecureBaseURL: "https://192.0.2.5:8792",
				PendingSpkiPin:       "sha256/pin",
			},
			serviceFile: true,
		},
		"desktop install without service choice": {},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			paths := setupHeadlessPurgeTest(t, tc.cfg, tc.serviceFile)
			if _, err := runTestPurge(paths); err == nil ||
				!strings.Contains(err.Error(), "device credential") {
				t.Fatalf("purge error = %v, want fail-closed device credential error", err)
			}
			if _, err := os.Stat(paths.ConfigFile); err != nil {
				t.Fatalf("config must survive a fail-closed purge: %v", err)
			}
		})
	}
}

func TestPurgePairedFileBackedInstallWithoutKeyringRemovesEverything(t *testing.T) {
	paths := setupHeadlessPurgeTest(t, runtimeConfig{
		RelaySecureBaseURL: "https://192.0.2.5:8792",
		RelaySpkiPin:       "sha256/pin",
	}, true)
	if err := deviceSecretFileSet(deviceCredentialService, generateTestDeviceCredential(t)); err != nil {
		t.Fatal(err)
	}
	previous := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(_, _, _ string) error { return nil }
	t.Cleanup(func() { revokeSelfDeviceV1ForUninstall = previous })
	report, err := runTestPurge(paths)
	if err != nil {
		t.Fatalf("purge error = %v", err)
	}
	if deviceSecretFileBacked() {
		t.Fatal("file-backend marker or routing survived purge")
	}
	if !strings.Contains(strings.Join(report.notes, "\n"), "keyring was not checked") {
		t.Fatalf("missing unchecked-keyring note: %q", report.notes)
	}
	if _, err := os.Stat(paths.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("config survived purge: %v", err)
	}
}

func TestPurgeFileBackedInstallNeverSwitchesToKeyringOnRetry(t *testing.T) {
	// A keyring credential this file-backed install never owned (e.g. the
	// real login keychain under a throwaway HOME, or one stranded by a
	// keyring-to-file switch made while the keyring was locked) must fail the
	// purge read-only — and a retry must not re-decide to the keyring and
	// delete it.
	paths := setupHeadlessPurgeTest(t, runtimeConfig{}, true)
	deviceCredentialPreflight = func() error { return nil }
	if err := writeDeviceFileBackendMarker(); err != nil {
		t.Fatal(err)
	}
	foreign := generateTestDeviceCredential(t)
	if err := secretKeyringSet(deviceCredentialService, secretUser(), foreign); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secretKeyringDelete(deviceCredentialService, secretUser()) })
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := runTestPurge(paths); err == nil ||
			!strings.Contains(err.Error(), "still holds a device credential") {
			t.Fatalf("attempt %d: purge error = %v, want the read-only keyring refusal", attempt, err)
		}
		if !deviceFileBackendMarkerExists() {
			t.Fatalf("attempt %d: the backend marker must survive a failed purge", attempt)
		}
		if got, err := secretKeyringGet(deviceCredentialService, secretUser()); err != nil || got != foreign {
			t.Fatalf("attempt %d: keyring credential = %q, %v; the purge must never delete it", attempt, got, err)
		}
	}
}

func TestGuidedPurgePreflightRoutesLikePurge(t *testing.T) {
	// Interactive `uninstall` → Full purge runs this preflight BEFORE the
	// guided teardown and before finalize; it must select the same backend.
	paths := setupHeadlessPurgeTest(t, runtimeConfig{}, true)
	if err := prepareUninstallBeforeGuidedTeardown(paths, uninstallModePurge); err != nil {
		t.Fatalf("guided preflight error = %v", err)
	}
	if deviceSecretFileBacked() {
		t.Fatal("guided preflight leaked process-wide file routing")
	}

	paired := setupHeadlessPurgeTest(t, runtimeConfig{
		RelaySecureBaseURL: "https://192.0.2.5:8792",
		RelaySpkiPin:       "sha256/pin",
	}, true)
	if err := prepareUninstallBeforeGuidedTeardown(paired, uninstallModePurge); err == nil ||
		!strings.Contains(err.Error(), "device credential") {
		t.Fatalf("guided preflight error = %v, want fail-closed device credential error", err)
	}
}

func TestPurgeRefusesForeignTokenFileBeforeAnyCleanup(t *testing.T) {
	paths := setupHeadlessPurgeTest(t, runtimeConfig{}, false)
	external := filepath.Join(t.TempDir(), "relay-token")
	if err := writeRelayAuthTokenFile(external, "user-managed"); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:         "192.0.2.5",
		HAURL:          "http://192.0.2.5:8123",
		RelayBaseURL:   "http://192.0.2.5:8791",
		RelayTokenFile: external,
	}); err != nil {
		t.Fatal(err)
	}
	if err := deviceSecretFileSet(deviceCredentialService, generateTestDeviceCredential(t)); err != nil {
		t.Fatal(err)
	}
	zshrc := filepath.Join(paths.Home, ".zshrc")
	writeTestFile(t, zshrc, pathBlockHeader+"\n"+pathExportLine+"\n")
	if label := uninstallTokenLineLabel(paths); label != "" {
		t.Fatalf("a foreign token file must not be listed as removed: %q", label)
	}
	if warning := uninstallForeignTokenFileWarning(paths); !strings.Contains(warning, "Full purge will stop") ||
		!strings.Contains(warning, paths.ConfigFile) {
		t.Fatalf("warning = %q", warning)
	}
	if _, err := runTestPurge(paths); err == nil ||
		!strings.Contains(err.Error(), "is not the managed service-token path") {
		t.Fatalf("purge error = %v", err)
	}
	if !deviceSecretFileExists(deviceCredentialService) {
		t.Fatal("device credential removed before the token-file refusal")
	}
	if got, _ := os.ReadFile(zshrc); !strings.Contains(string(got), pathBlockHeader) {
		t.Fatal("PATH entry removed before the token-file refusal")
	}
	if token, err := readRelayAuthTokenFile(external); err != nil || token != "user-managed" {
		t.Fatalf("external token = %q, %v", token, err)
	}
}
