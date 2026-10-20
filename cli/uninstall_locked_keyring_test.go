package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// lockedKeyringPurgeFixture: a keyring-backed pairing on Linux whose keyring
// is locked (0.27 lane G4: GDM autologin locks it after every boot). Setting
// *locked to false is the unlock.
func lockedKeyringPurgeFixture(t *testing.T) (runtimePaths, *bool) {
	t.Helper()
	home := withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	// No relay token stored; keeps the read off a real Secret Service.
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", home+"/no-relay-token")
	onSecureStoragePlatform(t, "linux")
	installSafeNativePromptContext(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceCredential(validCredential(7)); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAURL:              "http://192.0.2.5:8123",
		RelayBaseURL:       "http://192.0.2.5:8791",
		RelaySecureBaseURL: "https://192.0.2.5:8792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatal(err)
	}
	locked := true
	deviceCredentialPreflight = func() error {
		if locked {
			return desktopKeyringLockedError("default Secret Service collection is locked")
		}
		return nil
	}
	return paths, &locked
}

// stubKeyringUnlockWindow makes this terminal able to show the unlock window
// and counts how often it opens; unlock runs when it opens.
func stubKeyringUnlockWindow(t *testing.T, interactive bool, stdin string, unlock func()) *int {
	t.Helper()
	allowNativeSecureStoragePromptForTest(t)
	oldSupport := detectPlatformSecureStorageRecoverySupportForSetup
	oldRun := runPlatformSecureStorageRecoveryForSetup
	oldTTY, oldInputTTY, oldInteractive := writerSupportsTTYForSetup, uiInputSupportsTTY, uninstallInteractive
	oldStdin := os.Stdin
	t.Cleanup(func() {
		detectPlatformSecureStorageRecoverySupportForSetup = oldSupport
		runPlatformSecureStorageRecoveryForSetup = oldRun
		writerSupportsTTYForSetup, uiInputSupportsTTY, uninstallInteractive = oldTTY, oldInputTTY, oldInteractive
		os.Stdin = oldStdin
	})
	opened := 0
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) { return true, nil }
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction) error {
		if action != platformSecureStorageRecoveryUnlock {
			t.Fatalf("action = %q, want unlock", action)
		}
		opened++
		unlock()
		return nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return interactive }
	uiInputSupportsTTY = func() bool { return interactive }
	uninstallInteractive = func() bool { return interactive }
	in, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.WriteString(stdin); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	os.Stdin = in
	return &opened
}

func requireNothingRemoved(t *testing.T, paths runtimePaths, locked *bool) {
	t.Helper()
	if !fileExists(paths.ConfigFile) {
		t.Fatal("refused purge removed the config")
	}
	*locked = false
	if credential, ok, err := readDeviceCredential(); err != nil || !ok || credential != validCredential(7) {
		t.Fatalf("refused purge touched the device credential: ok=%v err=%v", ok, err)
	}
}

// Without a way to show the unlock window (--yes, SSH, no desktop), the purge
// stops before it revokes or removes anything and names one next step.
func TestPurgeWithLockedKeyringStopsBeforeAnyChange(t *testing.T) {
	paths, locked := lockedKeyringPurgeFixture(t)
	opened := stubKeyringUnlockWindow(t, false, "", func() { t.Fatal("no unlock window without a terminal") })

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes", "--purge"})
	})
	if exitCode != 1 || *opened != 0 {
		t.Fatalf("exit = %d, windows = %d\n%s", exitCode, *opened, output)
	}
	for _, want := range []string{
		"This computer's keyring is locked, so HA NOVA can't remove its saved key. Nothing was changed.",
		"Next step: unlock the keyring (log in to the desktop, or open Passwords and Keys), then run: ha-nova uninstall --yes --purge",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{"ha-nova setup", "pending device credential", "Removed"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("output has %q:\n%s", unwanted, output)
		}
	}
	requireNothingRemoved(t, paths, locked)
}

// In a desktop terminal the purge opens setup's unlock window and goes on once
// the keyring is unlocked.
func TestPurgeWithLockedKeyringOpensTheUnlockWindow(t *testing.T) {
	paths, locked := lockedKeyringPurgeFixture(t)
	opened := stubKeyringUnlockWindow(t, true, "\n", func() { *locked = false })

	var refused bool
	_, output := captureCommandOutput(t, func() int {
		refused = purgeRefusedOutsideDesktop(paths, uninstallModePurge, false)
		return 0
	})
	if refused || *opened != 1 {
		t.Fatalf("refused = %v, windows = %d\n%s", refused, *opened, output)
	}
	for _, want := range []string{
		"This computer's keyring is locked",
		"Type your password only in that window; HA NOVA never sees it.",
		"Open the window to unlock the keyring now",
		"The keyring is unlocked.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

// Declining the window, or a keyring that stays locked, stops the purge.
func TestPurgeWithLockedKeyringStopsWhenItStaysLocked(t *testing.T) {
	for name, tc := range map[string]struct {
		stdin       string
		wantWindows int
	}{
		"declined":     {stdin: "n\n", wantWindows: 0},
		"still locked": {stdin: "\nn\n", wantWindows: 1},
	} {
		t.Run(name, func(t *testing.T) {
			paths, locked := lockedKeyringPurgeFixture(t)
			opened := stubKeyringUnlockWindow(t, true, tc.stdin, func() {})

			var refused bool
			_, output := captureCommandOutput(t, func() int {
				refused = purgeRefusedOutsideDesktop(paths, uninstallModePurge, false)
				return 0
			})
			if !refused || *opened != tc.wantWindows ||
				!strings.Contains(output, "Nothing was changed.") ||
				!strings.Contains(output, "then run: ha-nova uninstall --purge") {
				t.Fatalf("refused = %v, windows = %d\n%s", refused, *opened, output)
			}
			requireNothingRemoved(t, paths, locked)
		})
	}
}

// Only a purge that must read the locked keyring stops: keeping the pairing,
// another platform, or a pairing kept in a file goes on.
func TestPurgeLockedKeyringGateScope(t *testing.T) {
	paths, _ := lockedKeyringPurgeFixture(t)
	stubKeyringUnlockWindow(t, false, "", func() {})
	if purgeRefusedOutsideDesktop(paths, uninstallModeStandard, false) {
		t.Fatal("keeping the pairing needs no keyring")
	}
	onSecureStoragePlatform(t, "darwin")
	if err := purgeKeyringLockedError(paths); err != nil {
		t.Fatalf("macOS keeps its own checks: %v", err)
	}
	onSecureStoragePlatform(t, "linux")
	if err := purgeKeyringLockedError(paths); err == nil {
		t.Fatal("locked keyring not found")
	}
	deviceCredentialFileModeForced = true
	if err := purgeKeyringLockedError(paths); err != nil {
		t.Fatalf("file-backed pairing reads no keyring: %v", err)
	}
}

func TestKeyringRemovalErrorIsPlain(t *testing.T) {
	locked := keyringRemovalError("inspect device credential", desktopKeyringLockedError("default Secret Service collection is locked"))
	if !strings.Contains(locked.Error(), "This computer's keyring is locked. Unlock it") ||
		strings.Contains(locked.Error(), "ha-nova setup") || !isDesktopKeyringLockedError(locked) {
		t.Fatalf("locked = %v", locked)
	}
	session := keyringRemovalError("inspect device credential", desktopKeyringSessionUnavailableError("no session bus"))
	if !strings.Contains(session.Error(), "Open a terminal on this computer's desktop") || strings.Contains(session.Error(), "ha-nova setup") {
		t.Fatalf("session = %v", session)
	}
	notSetUp := keyringRemovalError("inspect device credential", desktopKeyringInitializationRequiredError("no default collection"))
	if !isDesktopKeyringInitializationRequiredError(notSetUp) || !strings.Contains(notSetUp.Error(), "ha-nova setup") {
		t.Fatalf("not set up = %v", notSetUp)
	}
	other := keyringRemovalError("inspect device credential", errors.New("boom"))
	if other.Error() != "cannot inspect device credential: boom" {
		t.Fatalf("other = %v", other)
	}
}
