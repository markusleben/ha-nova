package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
		"Next step: on this computer's desktop, open Passwords and Keys (or KDE Wallet) and unlock your login keyring, or log out and back in with your password. Then run: ha-nova uninstall --yes --purge",
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
		refused = purgeRefusedOutsideDesktop(paths, uninstallModePurge, false, "ha-nova uninstall")
		return 0
	})
	if refused || *opened != 1 {
		t.Fatalf("refused = %v, windows = %d\n%s", refused, *opened, output)
	}
	for _, want := range []string{
		"This computer's keyring is locked",
		"Type your keyring password (usually your login password) only in that window; HA NOVA never sees it.",
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
		stdin, want, unwanted string
		wantWindows           int
	}{
		"declined": {stdin: "n\n", wantWindows: 0,
			want:     "Next step: run ha-nova uninstall again, answer Y, and type your keyring password (usually your login password) in the window that opens.",
			unwanted: "The keyring opened"},
		"still locked": {stdin: "\nn\n", wantWindows: 1,
			want:     "The keyring opened, but HA NOVA's key is still locked. Nothing was changed.",
			unwanted: "This computer's keyring is locked, so"},
	} {
		t.Run(name, func(t *testing.T) {
			paths, locked := lockedKeyringPurgeFixture(t)
			opened := stubKeyringUnlockWindow(t, true, tc.stdin, func() {})

			var refused bool
			_, output := captureCommandOutput(t, func() int {
				refused = purgeRefusedOutsideDesktop(paths, uninstallModePurge, false, "ha-nova uninstall")
				return 0
			})
			if !refused || *opened != tc.wantWindows ||
				!strings.Contains(output, "Nothing was changed.") ||
				!strings.Contains(output, tc.want) || strings.Contains(output, tc.unwanted) {
				t.Fatalf("refused = %v, windows = %d\n%s", refused, *opened, output)
			}
			requireNothingRemoved(t, paths, locked)
		})
	}
}

// Only a purge that must read the locked keyring stops: keeping the pairing,
// another platform, a pairing and token kept in files (setup --service; the
// fixture's token sits in a file), or a keyring that isn't set up goes on.
func TestPurgeLockedKeyringGateScope(t *testing.T) {
	paths, _ := lockedKeyringPurgeFixture(t)
	stubKeyringUnlockWindow(t, false, "", func() {})
	if purgeRefusedOutsideDesktop(paths, uninstallModeStandard, false, "ha-nova uninstall") {
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
	// The Link store reports "not set up" with the locked code.
	original := purgeRelayTokenRead
	t.Cleanup(func() { purgeRelayTokenRead = original })
	purgeRelayTokenRead = func() (string, error) {
		return "", newCloudError(CloudErrSecretStoreLocked, "access OAuth secret store",
			desktopKeyringInitializationRequiredError("no default Secret Service collection configured"))
	}
	if err := purgeKeyringLockedError(paths); err != nil {
		t.Fatalf("a keyring that isn't set up is not locked: %v", err)
	}
	purgeRelayTokenRead = func() (string, error) {
		return "", newCloudError(CloudErrSecretStoreLocked, "access OAuth secret store",
			desktopKeyringLockedError("default Secret Service collection is locked"))
	}
	if err := purgeKeyringLockedError(paths); err == nil {
		t.Fatal("locked Link store not found")
	}
	// UI-forbidden: the keyring itself says whether it is locked.
	purgeRelayTokenRead = func() (string, error) {
		return "", newCloudError(CloudErrSecretUIForbidden, "access OAuth secret store", nil)
	}
	if err := purgeKeyringLockedError(paths); err == nil {
		t.Fatal("UI-forbidden read on a locked keyring not found")
	}
	deviceCredentialPreflight = func() error {
		return desktopKeyringInitializationRequiredError("no default Secret Service collection configured")
	}
	if err := purgeKeyringLockedError(paths); err != nil {
		t.Fatalf("UI-forbidden read on a keyring that isn't set up: %v", err)
	}
}

// The guided menu unlocks first, so the Relay can still be asked about the App.
func TestGuidedUninstallUnlocksBeforeTheMenu(t *testing.T) {
	paths, locked := lockedKeyringPurgeFixture(t)
	opened := stubKeyringUnlockWindow(t, true, "\n", func() { *locked = false })
	var unlocked bool
	captureCommandOutput(t, func() int { unlocked = offerKeyringUnlockForPurge(paths); return 0 })
	if !unlocked || *opened != 1 || *locked {
		t.Fatalf("unlocked = %v, windows = %d", unlocked, *opened)
	}
	// Unlocked already: no second window.
	captureCommandOutput(t, func() int { unlocked = offerKeyringUnlockForPurge(paths); return 0 })
	if unlocked || *opened != 1 {
		t.Fatalf("second offer: unlocked = %v, windows = %d", unlocked, *opened)
	}
}

// Uninstall's window only unlocks: a keyring that turns out not set up never
// gets the "create a keyring" page.
func TestUninstallUnlockNeverOffersToCreateAKeyring(t *testing.T) {
	stubKeyringUnlockWindow(t, true, "\n\n", func() {})
	runPlatformSecureStorageRecoveryForSetup = func(platformSecureStorageRecoveryAction) error {
		return desktopKeyringInitializationRequiredError("no default Secret Service collection configured")
	}
	state := setupSecureStorageRecoveryState{unlockOnly: true}
	var out strings.Builder
	_, err := runSetupSecureStorageRecoveryFlow(bufio.NewReader(strings.NewReader("\n\n")), &out, keyringUnlockTrigger, &state, setupSecureStorageRecoveryInitialAttempt)
	if err == nil || strings.Contains(out.String(), "isn't set up yet") || strings.Count(out.String(), "Open the window") != 1 {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
}

func TestKeyringRemovalErrorIsPlain(t *testing.T) {
	locked := keyringRemovalError("inspect device credential", desktopKeyringLockedError("default Secret Service collection is locked"))
	if !strings.HasPrefix(locked.Error(), "HA NOVA can't remove its saved key. This computer's keyring is locked. Open Passwords and Keys") ||
		strings.Contains(locked.Error(), "ha-nova setup") || !isDesktopKeyringLockedError(locked) {
		t.Fatalf("locked = %v", locked)
	}
	if got := uninstallErrorText(fmt.Errorf("failed to validate device credential cleanup: %w", locked)); got != locked.Error() {
		t.Fatalf("printed = %q", got)
	}
	joined := errors.Join(fmt.Errorf("failed to validate: %w", locked), errors.New("reset Cloud secure-storage recovery proof: boom"))
	if got := uninstallErrorText(joined); got != locked.Error()+"\nreset Cloud secure-storage recovery proof: boom" {
		t.Fatalf("joined printed = %q", got)
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

// server remove words a locked keyring the same plain way and stops first.
func TestServerRemoveWithLockedKeyringIsPlain(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	previousGet := secretGetForServerRemove
	secretGetForServerRemove = func(string) (string, error) {
		return "", desktopKeyringLockedError("default Secret Service collection is locked")
	}
	t.Cleanup(func() { secretGetForServerRemove = previousGet })
	revokedAt := stubServerRevoke(t)
	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"remove", "cabin"})
	})
	if exit != 1 || len(*revokedAt) != 0 ||
		!strings.Contains(output, "This computer's keyring is locked. Open Passwords and Keys") ||
		!strings.Contains(output, "Nothing was removed.") || strings.Contains(output, "Secret Service") {
		t.Fatalf("exit=%d revoked=%v\n%s", exit, *revokedAt, output)
	}
}

// --yes asks nothing; from a desktop terminal the step is to drop --yes.
func TestPurgeYesWithLockedKeyringNamesTheWindowRun(t *testing.T) {
	paths, locked := lockedKeyringPurgeFixture(t)
	opened := stubKeyringUnlockWindow(t, true, "", func() { t.Fatal("--yes opened the window") })
	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes", "--purge"})
	})
	if exitCode != 1 || *opened != 0 ||
		!strings.Contains(output, "Next step: run ha-nova uninstall --purge (without --yes) and answer Y.") {
		t.Fatalf("exit = %d, windows = %d\n%s", exitCode, *opened, output)
	}
	requireNothingRemoved(t, paths, locked)
}

// Guided uninstall unlocks before the menu, so the Relay can still be asked
// whether the App may go too; a declined window disables Remove instead of
// asking again after the menu.
func TestGuidedUninstallUnlocksBeforeTheAppOffer(t *testing.T) {
	for name, tc := range map[string]struct {
		stdin       string
		wantApp     bool
		wantWindows int
		wantDefault string
	}{
		"unlocked": {stdin: "\n", wantApp: true, wantWindows: 1, wantDefault: "purge"},
		// The user just said no: Enter in the menu must remove nothing.
		"declined": {stdin: "n\n", wantApp: false, wantWindows: 0, wantDefault: "cancel"},
	} {
		t.Run(name, func(t *testing.T) {
			paths, locked := lockedKeyringPurgeFixture(t)
			stubAppRemoval(t, newFakeSupervisor(t), nil, func() (devicePeers, error) {
				if *locked {
					return devicePeers{}, errors.New("keyring locked")
				}
				return peersOf(0, false, false), nil
			})
			menu := stubUninstallMenu(t, "cancel")
			opened := stubKeyringUnlockWindow(t, true, tc.stdin, func() { *locked = false })
			exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, nil) })
			hasApp := slices.ContainsFunc(menu.spec.Options, func(o setupMenuOption) bool { return o.Value == "app" })
			if exitCode != 0 || *opened != tc.wantWindows || hasApp != tc.wantApp ||
				menu.spec.Options[0].Disabled == tc.wantApp || menu.spec.DefaultValue != tc.wantDefault {
				t.Fatalf("exit=%d windows=%d spec=%+v\n%s", exitCode, *opened, menu.spec, output)
			}
			if !tc.wantApp && !strings.Contains(menu.spec.Options[0].DisabledReason, "keyring is locked") {
				t.Fatalf("reason = %q", menu.spec.Options[0].DisabledReason)
			}
		})
	}
}

// The early stop keeps the purge relock contract: a verified Link storage
// hold is reset, so the next Link step is unlock, not cleanup.
func TestPurgeLockedKeyringStopResetsLinkStorageProof(t *testing.T) {
	paths, _ := lockedKeyringPurgeFixture(t)
	stubKeyringUnlockWindow(t, false, "", func() {})
	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
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
	store, err := NewOAuthSecretStore(newMemoryOAuthSecretBackend(), cfg.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	previousStore := newCloudSecretStoreForCLI
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) { return store, nil }
	t.Cleanup(func() { newCloudSecretStoreForCLI = previousStore })

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes", "--purge"})
	})
	if exitCode != 1 || !strings.Contains(output, "Nothing was changed.") {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	saved, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil || saved.Cloud == nil || saved.Cloud.RecoveryHold == nil || saved.Cloud.RecoveryHold.StorageVerified {
		t.Fatalf("storage proof kept: %+v (err %v)", saved.Cloud, err)
	}
}

// An interrupted keyring-to-file move leaves keyring copies the purge reads
// from the keyring: a locked keyring there gets the unlock window too.
func TestPurgeLockedKeyringGateSeesMigrationCleanupSlots(t *testing.T) {
	paths, _ := lockedKeyringPurgeFixture(t)
	deviceCredentialFileModeForced = true // the pairing itself now lives in a file
	if err := purgeKeyringLockedError(paths); err != nil {
		t.Fatalf("no checkpoint, no keyring read: %v", err)
	}
	cleanupPath, err := keyringDeviceCredentialCleanupPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cleanupPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeKeyringDeviceCredentialCleanup([]string{deviceCredentialService}); err != nil {
		t.Fatal(err)
	}
	if err := purgeKeyringLockedError(paths); err == nil {
		t.Fatal("locked migration-cleanup slot not found")
	}
}
