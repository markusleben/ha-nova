package main

import (
	"os"
	"strings"
	"testing"
)

// A headless server keeps its pairing in files and has no desktop to sign in
// to: over SSH, a full purge must finish and only note the relay token copy
// it can't reach, not stop at a step the user can't take.
func TestHeadlessFilePurgeSkipsUnreachableRelayToken(t *testing.T) {
	home := withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	// Keeps any unstubbed relay token read off a real Secret Service.
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", home+"/no-relay-token")
	onSecureStoragePlatform(t, "linux")
	installSafeNativePromptContext(t)
	stubKeyringUnlockWindow(t, false, "", func() { t.Fatal("no unlock window for an unreachable keyring") })
	stubServerRevoke(t)
	t.Setenv("SSH_CONNECTION", "10.0.0.2 50000 10.0.0.3 22")
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	// Only the on-disk marker says "files" during a real uninstall.
	if err := writeDeviceFileBackendMarker(); err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceCredential(validCredential(7)); err != nil {
		t.Fatal(err)
	}
	if !deviceSecretFileExists(deviceCredentialService) {
		t.Fatal("pairing not in files")
	}
	if err := saveConfig(paths, runtimeConfig{
		HAURL:              "http://192.0.2.5:8123",
		RelayBaseURL:       "http://192.0.2.5:8791",
		RelaySecureBaseURL: "https://192.0.2.5:8792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatal(err)
	}
	deviceCredentialPreflight = func() error { return sshNoSessionBus }
	unreachable := func() (string, error) { return "", relayAuthTokenReadError("ha-nova", sshNoSessionBus) }
	originalPurgeRead, originalRead, originalDelete := purgeRelayTokenRead, readRelayAuthTokenForUninstall, deleteRelayAuthTokenForUninstall
	t.Cleanup(func() {
		purgeRelayTokenRead, readRelayAuthTokenForUninstall, deleteRelayAuthTokenForUninstall = originalPurgeRead, originalRead, originalDelete
	})
	purgeRelayTokenRead, readRelayAuthTokenForUninstall = unreachable, unreachable
	deleteRelayAuthTokenForUninstall = func() error { t.Fatal("no delete without a read"); return nil }

	if got := uninstallPurgeBlocked(paths); got != "" {
		t.Fatalf("menu reason = %q", got)
	}
	exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, []string{"--yes", "--purge"}) })
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "HA NOVA couldn't check this computer's keyring for its old relay key from here.") ||
		strings.Contains(output, "sign in to the desktop") || strings.Contains(output, "Open a terminal on this computer's desktop") {
		t.Fatalf("output:\n%s", output)
	}
	if _, err := os.Stat(paths.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("config survived the purge: %v", err)
	}
	if deviceSecretFileExists(deviceCredentialService) {
		t.Fatal("device credential file survived the purge")
	}
}

// A desktop install can hold the marker too (first set up over SSH). A
// default server that still connects with the legacy token keeps the stop:
// skipping would leave a live token behind.
func TestFilePurgeKeepsStopWhenRelayTokenIsInUse(t *testing.T) {
	paths, _ := unreachableKeyringPurgeFixture(t, sshNoSessionBus)
	stubKeyringUnlockWindow(t, false, "", func() {})
	deviceCredentialFileModeForced = true
	deviceCredentialPreflight = func() error { return nil }
	original := purgeRelayTokenRead
	t.Cleanup(func() { purgeRelayTokenRead = original })
	purgeRelayTokenRead = func() (string, error) { return "", relayAuthTokenReadError("ha-nova", sshNoSessionBus) }
	if err := purgeKeyringOutOfReachError(paths); err != nil {
		t.Fatalf("paired default server: %v", err)
	}
	// A keyring that isn't set up holds nothing; a locked one keeps its
	// unlock step (it may hold a device key from before the switch to files).
	purgeRelayTokenRead = func() (string, error) {
		return "", relayAuthTokenReadError("ha-nova", desktopKeyringLockedError("default Secret Service collection is locked"))
	}
	if err := purgeKeyringLockedError(paths); err == nil {
		t.Fatal("locked keyring must keep its unlock step")
	}
	purgeRelayTokenRead = func() (string, error) {
		return "", relayAuthTokenReadError("ha-nova", desktopKeyringInitializationRequiredError("no default Secret Service collection configured"))
	}
	if purgeRefusedForUnreadableRelayToken(paths, "ha-nova uninstall --purge") {
		t.Fatal("not set up: refused")
	}
	purgeRelayTokenRead = func() (string, error) { return "", relayAuthTokenReadError("ha-nova", sshNoSessionBus) }
	if err := saveConfig(paths, runtimeConfig{HAURL: "http://192.0.2.5:8123", RelayBaseURL: "http://192.0.2.5:8791"}); err != nil {
		t.Fatal(err)
	}
	if err := purgeKeyringOutOfReachError(paths); err == nil {
		t.Fatal("a default server on the legacy token must stop the purge")
	}
}

// A standard uninstall keeps the pairing. A locked keyring can't show the
// slot, so the closing note goes by the paired config, not "relay token".
func TestKeepPairingUninstallWithLockedKeyringNamesThePairing(t *testing.T) {
	paths, _ := lockedKeyringPurgeFixture(t)
	stubKeyringUnlockWindow(t, false, "", func() {})
	exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, []string{"--yes"}) })
	if exitCode != 0 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Kept Home Assistant connection config and this device's pairing.") ||
		strings.Contains(output, "stored relay token") {
		t.Fatalf("output:\n%s", output)
	}
}
