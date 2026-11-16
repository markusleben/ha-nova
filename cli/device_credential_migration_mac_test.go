package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMacKeepsKeychainKeysInsteadOfCopyingThemToFiles(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	deviceKeyNeverCopiedToFile = true
	t.Cleanup(func() {
		deviceKeyNeverCopiedToFile, deviceKeyStaysInKeychain = false, false
		setActiveServerProfile(defaultServerProfileName)
	})
	saved := gateWindowOffForFileCopy
	t.Cleanup(func() { gateWindowOffForFileCopy = saved })
	gateWindowOffForFileCopy = func(string, string, bool) error {
		t.Error("nothing is copied on macOS, so no window_off is sent")
		return nil
	}
	defaultKey, cabinKey := generateTestDeviceCredential(t), generateTestDeviceCredential(t)
	if err := writeDeviceCredential(defaultKey); err != nil {
		t.Fatal(err)
	}
	setActiveServerProfile("cabin") // a new profile: the other profile's Keychain key decides
	if err := writePendingDeviceCredential(cabinKey); err != nil {
		t.Fatal(err)
	}

	migrated, err := migrateKeyringDeviceCredentialToFile()
	if !errors.Is(err, errDeviceKeyStaysInKeychain) || migrated {
		t.Fatalf("migrated=%v err=%v, want the Keychain kept", migrated, err)
	}
	if !deviceKeyStaysInKeychain || deviceCredentialFileModeForced || deviceFileBackendMarkerExists() {
		t.Fatal("the install must stay on the Keychain, so no other profile's key is hidden")
	}
	if got, ok, err := readPendingDeviceCredential(); err != nil || !ok || got != cabinKey {
		t.Fatalf("this profile's pending Keychain key must stay readable: ok=%v err=%v", ok, err)
	}
	setActiveServerProfile(defaultServerProfileName)
	if got, ok, err := readDeviceCredential(); err != nil || !ok || got != defaultKey {
		t.Fatalf("the other profile's Keychain key must stay readable: ok=%v err=%v", ok, err)
	}
}

func TestMacUnreadableKeychainSlotKeepsTheKeychain(t *testing.T) {
	for _, broken := range []string{"cabin", "zulu"} { // sorted before and after the healthy default
		withDeviceStorageTestHome(t)
		resetKeyringDeviceSlots(t)
		deviceKeyNeverCopiedToFile = true
		if err := writeDeviceCredential(generateTestDeviceCredential(t)); err != nil {
			t.Fatal(err)
		}
		if err := keyring.Set(deviceCredentialServiceForProfile(broken), secretUser(), "not-a-key"); err != nil {
			t.Fatal(err)
		}
		setActiveServerProfile(broken)
		migrated, err := migrateKeyringDeviceCredentialToFile()
		if !errors.Is(err, errDeviceKeyStaysInKeychain) || migrated || !deviceKeyStaysInKeychain {
			t.Errorf("%s: migrated=%v err=%v keychain=%v, want the Keychain kept", broken, migrated, err, deviceKeyStaysInKeychain)
		}
		_ = keyring.Delete(deviceCredentialServiceForProfile(broken), secretUser())
		setActiveServerProfile(defaultServerProfileName)
		deviceKeyNeverCopiedToFile, deviceKeyStaysInKeychain = false, false
	}
}

func TestMacPairToFileRefusesWithoutUsingTheCode(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	deviceKeyNeverCopiedToFile = true
	t.Cleanup(func() { deviceKeyNeverCopiedToFile, deviceKeyStaysInKeychain = false, false })
	if err := writeDeviceCredential(generateTestDeviceCredential(t)); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(t.TempDir(), "config.json")
	paths := runtimePaths{ConfigDir: filepath.Dir(configFile), ConfigFile: configFile}
	if err := saveConfig(paths, runtimeConfig{RelayBaseURL: "http://ha:8791"}); err != nil {
		t.Fatal(err)
	}
	orig := runSecurePairingForPairCmd
	runSecurePairingForPairCmd = func(_, _ string, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		t.Error("the one-time code was used")
		return "", nil
	}
	defer func() { runSecurePairingForPairCmd = orig }()
	if rc := runPairCommand(paths, []string{"--code", "123456", "--credential-store=file"}); rc != 1 {
		t.Fatalf("rc=%d, want a refusal", rc)
	}
	if deviceFileBackendMarkerExists() || deviceCredentialFileModeForced {
		t.Fatal("a refused file pairing must leave the Keychain in charge")
	}
}

func TestMacWithoutAKeychainKeyMayStoreItsFirstKeyInAFile(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	deviceKeyNeverCopiedToFile = true
	t.Cleanup(func() { deviceKeyNeverCopiedToFile, deviceKeyStaysInKeychain = false, false })
	if migrated, err := migrateKeyringDeviceCredentialToFile(); err != nil || migrated || deviceKeyStaysInKeychain {
		t.Fatalf("migrated=%v err=%v keychain=%v", migrated, err, deviceKeyStaysInKeychain)
	}
}
