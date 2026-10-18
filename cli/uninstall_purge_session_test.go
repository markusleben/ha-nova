package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func failSecureStorageReads(t *testing.T, err error) {
	t.Helper()
	original := secretKeyringGetWithPolicy
	secretKeyringGetWithPolicy = func(context.Context, string, string, SecretStoreUIPolicy) (string, error) {
		return "", err
	}
	t.Cleanup(func() { secretKeyringGetWithPolicy = original })
}

// rc2 lane N1: a purge over SSH, whose secure storage is out of reach, stops
// in the terminal with the desktop next step, before changing anything.
func TestPurgeOverSSHRefusesBeforeChangingAnything(t *testing.T) {
	withDeviceStorageTestHome(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{HAURL: "http://192.0.2.5:8123"}); err != nil {
		t.Fatal(err)
	}
	onSecureStoragePlatform(t, "windows")
	installSafeNativePromptContext(t)
	t.Setenv("SSH_CONNECTION", "10.0.0.2 50000 10.0.0.3 22")
	failSecureStorageReads(t, newCloudError(CloudErrSecretStore, "access native secure storage", nil))

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes", "--purge"})
	})
	if exitCode != 1 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	for _, want := range []string{
		"can't reach secure storage from this terminal",
		"Nothing changed.",
		"Next step: sign in to the desktop of the computer HA NOVA is installed on",
		"then run: ha-nova uninstall --yes --purge",
		"Details: access native secure storage: SECRET_STORE_UNAVAILABLE",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if !fileExists(paths.ConfigFile) {
		t.Fatal("refused purge removed the config")
	}
	if _, err := os.Stat(paths.UninstallStatusFile); !os.IsNotExist(err) {
		t.Fatalf("refused purge left an uninstall status behind: %v", err)
	}
}

// Only a session that can't reach the store refuses: a desktop terminal and a
// missing credential go on to the purge, which keeps its own checks.
func TestPurgeSecureStorageSessionErrorOnlyForUnreachableSessions(t *testing.T) {
	withDeviceStorageTestHome(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	installSafeNativePromptContext(t)
	storeErr := newCloudError(CloudErrSecretStore, "access native secure storage", nil)

	onSecureStoragePlatform(t, "windows")
	if err := purgeSecureStorageSessionError(paths); err != nil {
		t.Fatalf("no credential stored: %v", err)
	}
	failSecureStorageReads(t, storeErr)
	if err := purgeSecureStorageSessionError(paths); err != nil {
		t.Fatalf("desktop terminal refused: %v", err)
	}
	t.Setenv("SSH_CONNECTION", "10.0.0.2 50000 10.0.0.3 22")
	if err := purgeSecureStorageSessionError(paths); err == nil {
		t.Fatal("SSH session with unreachable storage not refused")
	}
	onSecureStoragePlatform(t, "linux")
	if err := purgeSecureStorageSessionError(paths); err != nil {
		t.Fatalf("linux keeps its keyring guidance, got refusal: %v", err)
	}
}

// The checks the detached Windows helper would fail on run in the terminal.
func TestValidatePurgeBeforeWindowsHandoffReportsHelperFailures(t *testing.T) {
	withDeviceStorageTestHome(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePurgeBeforeWindowsHandoff(paths); err != nil {
		t.Fatalf("clean install: %v", err)
	}
	// An active pairing endpoint without its credential can't be revoked.
	if err := saveConfig(paths, runtimeConfig{
		RelaySecureBaseURL: "https://192.0.2.5:8792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatal(err)
	}
	err = validatePurgeBeforeWindowsHandoff(paths)
	if err == nil || !strings.Contains(err.Error(), "current credential is missing") {
		t.Fatalf("err = %v", err)
	}
}
