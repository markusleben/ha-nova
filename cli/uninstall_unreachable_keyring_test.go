package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// unreachableKeyringPurgeFixture: lockedKeyringPurgeFixture's keyring-backed
// pairing, but this terminal can't reach the keyring at all (follow-up to
// #775: over SSH the purge removed part of HA NOVA, then failed there).
// Setting *locked to false makes the keyring reachable again.
func unreachableKeyringPurgeFixture(t *testing.T, keyringErr error) (runtimePaths, *bool) {
	t.Helper()
	paths, locked := lockedKeyringPurgeFixture(t)
	deviceCredentialPreflight = func() error {
		if *locked {
			return keyringErr
		}
		return nil
	}
	return paths, locked
}

var sshNoSessionBus = desktopKeyringSessionUnavailableError("local secure storage is unavailable in this Linux session")

func TestPurgeOverSSHWithoutKeyringStopsBeforeAnyChange(t *testing.T) {
	paths, reachable := unreachableKeyringPurgeFixture(t, sshNoSessionBus)
	stubKeyringUnlockWindow(t, false, "", func() { t.Fatal("no unlock window for an unreachable keyring") })
	t.Setenv("SSH_CONNECTION", "10.0.0.2 50000 10.0.0.3 22")

	for _, args := range [][]string{{"--yes", "--purge"}, {"--purge"}} {
		exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, args) })
		if exitCode != 1 {
			t.Fatalf("%v: exit = %d\n%s", args, exitCode, output)
		}
		for _, want := range []string{
			"HA NOVA can't reach this computer's keyring from this terminal (for example over SSH), so it can't remove its saved key. Nothing was changed.",
			"Next step: sign in to the desktop of the computer HA NOVA is installed on",
			"Then run: " + uninstallRerunCommand(args[0] == "--yes", true),
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("%v: output missing %q:\n%s", args, want, output)
			}
		}
		for _, unwanted := range []string{"Removed", "desktop keyring session unavailable", "Secret Service", "ha-nova setup"} {
			if strings.Contains(output, unwanted) {
				t.Fatalf("%v: output has %q:\n%s", args, unwanted, output)
			}
		}
		if !fileExists(paths.ConfigFile) {
			t.Fatalf("%v: refused purge removed the config", args)
		}
	}
	// The guided menu disables Remove instead.
	if got := uninstallPurgeBlocked(paths); got != "needs this computer's own desktop session" {
		t.Fatalf("menu reason = %q", got)
	}
	requireNothingRemoved(t, paths, reachable)
}

// No session bus at all (a console login) reads the same; a desktop terminal
// whose keyring isn't running gets the log-out step.
func TestPurgeWithUnreachableKeyringNamesTheRightStep(t *testing.T) {
	for name, tc := range map[string]struct {
		err        error
		want, menu string
	}{
		"no session bus": {err: sshNoSessionBus,
			want: "HA NOVA can't reach this computer's keyring from this terminal",
			menu: "needs this computer's own desktop session"},
		"bus socket gone": {err: errors.New("dial unix /run/user/1000/bus: connect: no such file or directory"),
			want: "HA NOVA can't reach this computer's keyring from this terminal",
			menu: "needs this computer's own desktop session"},
		"not running": {err: desktopKeyringUnavailableError("Secret Service preflight failed"),
			want: "This computer's keyring isn't running, so HA NOVA can't remove its saved key. Nothing was changed.\n[ha-nova] WARNING: Next step: log out of the desktop and back in. Then run: ha-nova uninstall --purge",
			menu: "the keyring isn't running; log out and back in, then run ha-nova uninstall again"},
	} {
		t.Run(name, func(t *testing.T) {
			paths, reachable := unreachableKeyringPurgeFixture(t, tc.err)
			stubKeyringUnlockWindow(t, true, "", func() { t.Fatal("no unlock window for an unreachable keyring") })
			var refused bool
			_, output := captureCommandOutput(t, func() int {
				refused = purgeRefusedOutsideDesktop(paths, uninstallModePurge, false, "ha-nova uninstall --purge")
				return 0
			})
			if !refused || !strings.Contains(output, tc.want) || strings.Contains(output, "/run/user") {
				t.Fatalf("refused = %v\n%s", refused, output)
			}
			if got := uninstallPurgeBlocked(paths); got != tc.menu {
				t.Fatalf("menu reason = %q", got)
			}
			requireNothingRemoved(t, paths, reachable)
		})
	}
}

// Only reads the purge would fail on stop it: keeping the pairing, macOS, a
// pairing in files, a locked keyring (its own path), and a relay token whose
// keyring isn't running (the purge skips it with a note) go on.
func TestPurgeUnreachableKeyringGateScope(t *testing.T) {
	paths, _ := unreachableKeyringPurgeFixture(t, sshNoSessionBus)
	stubKeyringUnlockWindow(t, false, "", func() {})
	if purgeRefusedOutsideDesktop(paths, uninstallModeStandard, false, "ha-nova uninstall") {
		t.Fatal("keeping the pairing needs no keyring")
	}
	if err := purgeKeyringOutOfReachError(paths); err == nil {
		t.Fatal("unreachable keyring not found")
	}
	onSecureStoragePlatform(t, "darwin")
	if err := purgeKeyringOutOfReachError(paths); err != nil {
		t.Fatalf("macOS keeps its own checks: %v", err)
	}
	onSecureStoragePlatform(t, "linux")
	deviceCredentialPreflight = func() error {
		return desktopKeyringLockedError("default Secret Service collection is locked")
	}
	if err := purgeKeyringOutOfReachError(paths); err != nil {
		t.Fatalf("a locked keyring has its own path: %v", err)
	}
	deviceCredentialPreflight = func() error { return sshNoSessionBus }
	deviceCredentialFileModeForced = true
	if err := purgeKeyringOutOfReachError(paths); err != nil {
		t.Fatalf("file-backed pairing reads no keyring: %v", err)
	}
	original := purgeRelayTokenRead
	t.Cleanup(func() { purgeRelayTokenRead = original })
	purgeRelayTokenRead = func() (string, error) {
		return "", relayAuthTokenReadError("ha-nova", desktopKeyringUnavailableError("Secret Service preflight failed"))
	}
	if err := purgeKeyringOutOfReachError(paths); err != nil {
		t.Fatalf("the purge skips a relay token whose keyring isn't running: %v", err)
	}
	purgeRelayTokenRead = func() (string, error) { return "", relayAuthTokenReadError("ha-nova", sshNoSessionBus) }
	if err := purgeKeyringOutOfReachError(paths); err == nil {
		t.Fatal("relay token out of reach not found")
	}
	// The Link store wraps the keyring state in its own error.
	purgeRelayTokenRead = original
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(t.TempDir(), "no-relay-token"))
	if err := purgeKeyringOutOfReachError(paths); err != nil {
		t.Fatalf("nothing left to read: %v", err)
	}
}

func TestKeyringErrorTextIsPlain(t *testing.T) {
	onSecureStoragePlatform(t, "linux")
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"locked":      {desktopKeyringLockedError("default Secret Service collection is locked"), keyringLockedMessage},
		"no bus":      {sshNoSessionBus, keyringSessionMessage},
		"not running": {desktopKeyringUnavailableError("Secret Service preflight failed"), keyringMissingMessage},
		"not set up":  {desktopKeyringInitializationRequiredError("no default collection"), keyringNotSetUpMessage},
		"Link locked": {newCloudError(CloudErrSecretStoreLocked, "unlock Secret Service item", nil), keyringLockedMessage},
		"Link no bus": {newCloudError(CloudErrSecretStore, "access OAuth secret store", sshNoSessionBus), keyringSessionMessage},
		"other":       {errors.New("boom"), "boom"},
	} {
		if got := keyringErrorText(tc.err); got != tc.want {
			t.Fatalf("%s: got %q, want %q", name, got, tc.want)
		}
	}
	onSecureStoragePlatform(t, "darwin")
	if got := keyringErrorText(newCloudError(CloudErrSecretStoreLocked, "access native secure storage", nil)); strings.Contains(got, "Passwords and Keys") {
		t.Fatalf("macOS got the Linux text: %q", got)
	}
}

func TestCloudKeyringProblemIsPlain(t *testing.T) {
	onSecureStoragePlatform(t, "linux")
	problem := cloudProblemForError(newCloudError(CloudErrSecretStore, "access OAuth secret store", sshNoSessionBus))
	if got := problem.humanText(); got != "Home Assistant Link: "+keyringSessionMessage+" (code cloud_secure_storage; next: unlock_secure_storage)" {
		t.Fatalf("got %q", got)
	}
	other := cloudProblemForError(newCloudError(CloudErrForbidden, "connect", nil))
	if other.humanText() != other.Error() {
		t.Fatalf("non-keyring problem changed: %q", other.humanText())
	}
}

func TestPairWithLockedKeyringPrintsPlainText(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	onSecureStoragePlatform(t, "linux")
	stubStorageCanaries(t, desktopKeyringLockedError("default Secret Service collection is locked"))
	dir := t.TempDir()
	paths := runtimePaths{ConfigDir: dir, ConfigFile: filepath.Join(dir, "config.json")}
	if err := saveConfig(paths, runtimeConfig{RelayBaseURL: "http://ha:8791"}); err != nil {
		t.Fatal(err)
	}
	exitCode, output := captureCommandOutput(t, func() int {
		return runPairCommand(paths, []string{"--code", "123456"})
	})
	if exitCode != 1 {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	for _, want := range []string{
		"This computer can't store the device key yet. " + keyringLockedMessage,
		"No one unlocks this computer's desktop (a VM or server)? Run: ha-nova pair --credential-store=file",
		"Nothing was paired",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Secret Service") || strings.Contains(output, "desktop keyring locked") {
		t.Fatalf("raw keyring text:\n%s", output)
	}
}

// Same relock contract as the locked-keyring stop: a verified Link storage
// hold is reset, so the next Link step is unlock, not cleanup.
func TestPurgeUnreachableKeyringStopResetsLinkStorageProof(t *testing.T) {
	paths, _ := unreachableKeyringPurgeFixture(t, sshNoSessionBus)
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
	if exitCode != 1 || !strings.Contains(output, "can't reach this computer's keyring") {
		t.Fatalf("exit = %d\n%s", exitCode, output)
	}
	saved, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil || saved.Cloud == nil || saved.Cloud.RecoveryHold == nil || saved.Cloud.RecoveryHold.StorageVerified {
		t.Fatalf("storage proof kept: %+v (err %v)", saved.Cloud, err)
	}
}

// The purge reads a legacy relay token late, after client integrations and
// PATH are gone: any read error it won't skip stops the run up front.
func TestPurgeStopsUpFrontOnUnreadableRelayToken(t *testing.T) {
	paths, _ := unreachableKeyringPurgeFixture(t, nil)
	stubKeyringUnlockWindow(t, false, "", func() {})
	original := purgeRelayTokenRead
	t.Cleanup(func() { purgeRelayTokenRead = original })
	purgeRelayTokenRead = func() (string, error) {
		return "", relayAuthTokenReadError("ha-nova", desktopKeyringInitializationRequiredError("no default Secret Service collection configured"))
	}
	var refused bool
	_, output := captureCommandOutput(t, func() int {
		refused = purgeRefusedOutsideDesktop(paths, uninstallModePurge, true, "ha-nova uninstall --yes --purge")
		return 0
	})
	for _, want := range []string{
		"HA NOVA can't read its saved key in this computer's keyring, so it can't remove it. Nothing was changed.",
		"Next step: " + keyringNotSetUpMessage + " Then run: ha-nova uninstall --yes --purge",
	} {
		if !refused || !strings.Contains(output, want) {
			t.Fatalf("refused = %v, missing %q\n%s", refused, want, output)
		}
	}
	// A configured token file is removed, not read: no stop.
	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelayTokenFile = defaultRelayAuthTokenFile(paths)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if purgeRefusedForUnreadableRelayToken(paths, "ha-nova uninstall --purge") {
		t.Fatal("token file install refused")
	}
}

func TestLinkKeyringErrorsArePlain(t *testing.T) {
	onSecureStoragePlatform(t, "linux")
	busGone := newCloudError(CloudErrSecretStore, "access OAuth secret store",
		errors.New("dial unix /run/user/1000/bus: connect: no such file or directory"))
	if !isLinuxKeyringOutOfReach(busGone) || !strings.HasPrefix(keyringErrorText(busGone), keyringSessionMessage) {
		t.Fatalf("godbus text inside a CloudError: %q", keyringErrorText(busGone))
	}
	original := deviceCredentialPreflight
	t.Cleanup(func() { deviceCredentialPreflight = original })
	deviceCredentialPreflight = func() error { return desktopKeyringLockedError("default Secret Service collection is locked") }
	forbidden := newCloudError(CloudErrSecretUIForbidden, "access OAuth secret store", nil)
	if got := keyringErrorText(forbidden); got != keyringLockedMessage {
		t.Fatalf("UI-forbidden on a locked keyring: %q", got)
	}
	deviceCredentialPreflight = func() error { return nil }
	if got := keyringErrorText(forbidden); got == keyringLockedMessage {
		t.Fatal("UI-forbidden on an unlocked keyring read as locked")
	}
	// A write that may have landed keeps its safety text.
	unknown := &cloudProblem{Code: cloudProblemSecureStorage, Remediation: cloudRemediationVerifyState,
		Detail: "a secure-storage update may have completed", Cause: desktopKeyringLockedError("x")}
	if unknown.humanText() != unknown.Error() {
		t.Fatalf("verify-state problem rewritten: %q", unknown.humanText())
	}
	// Link recovery guidance offers cloud unlock only where it can help.
	if cloudProblemNeedsStorageUnlock(cloudProblemForError(desktopKeyringUnavailableError("x"))) ||
		!cloudProblemNeedsStorageUnlock(cloudProblemForError(desktopKeyringLockedError("x"))) {
		t.Fatal("cloud unlock offered for a keyring that isn't running, or not for a locked one")
	}
	// cloud: one next step after the plain text.
	for name, tc := range map[string]struct {
		cause      error
		want, none string
	}{
		"locked":      {desktopKeyringLockedError("x"), "Next step: in a terminal on this computer's desktop, run: ha-nova cloud unlock", "non-elevated"},
		"not running": {desktopKeyringUnavailableError("x"), keyringMissingMessage, "cloud unlock"},
	} {
		_, output := captureCommandOutput(t, func() int {
			printCloudCommandProblem(newCloudError(CloudErrSecretStore, "access OAuth secret store", tc.cause))
			return 0
		})
		if !strings.Contains(output, tc.want) || strings.Contains(output, tc.none) {
			t.Fatalf("%s:\n%s", name, output)
		}
	}
}

func TestPairFileHintKeepsTheServer(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	resetServerProfileSelection(t)
	onSecureStoragePlatform(t, "linux")
	stubStorageCanaries(t, desktopKeyringLockedError("default Secret Service collection is locked"))
	dir := t.TempDir()
	paths := runtimePaths{ConfigDir: dir, ConfigFile: filepath.Join(dir, "config.json")}
	_, output := captureCommandOutput(t, func() int {
		return runPairCommand(paths, []string{"--server", "cabin", "--relay-url", "http://192.0.2.9:8791", "--code", "123456"})
	})
	if !strings.Contains(output, "Run: ha-nova pair --server cabin --relay-url http://192.0.2.9:8791 --credential-store=file") {
		t.Fatalf("output:\n%s", output)
	}
}
