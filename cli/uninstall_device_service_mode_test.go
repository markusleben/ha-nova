package main

import (
	"os"
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
	if _, err := runTestPurge(paths); err != nil {
		t.Fatalf("purge error = %v", err)
	}
	if deviceSecretFileBacked() {
		t.Fatal("file-backend marker or routing survived purge")
	}
	if _, err := os.Stat(paths.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("config survived purge: %v", err)
	}
}

func TestPurgeFileBackedInstallWithReachableKeyringStillProvesKeyringEmpty(t *testing.T) {
	// A keyring credential stranded by a keyring-to-file switch made while the
	// keyring was locked must still fail the final proof when it is reachable.
	paths := setupHeadlessPurgeTest(t, runtimeConfig{}, true)
	deviceCredentialPreflight = func() error { return nil }
	if err := writeDeviceFileBackendMarker(); err != nil {
		t.Fatal(err)
	}
	if err := secretKeyringSet(deviceCredentialService, secretUser(), generateTestDeviceCredential(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secretKeyringDelete(deviceCredentialService, secretUser()) })
	if _, err := runTestPurge(paths); err == nil ||
		!strings.Contains(err.Error(), "reappeared") {
		t.Fatalf("purge error = %v, want the stranded keyring credential to fail the final proof", err)
	}
}
