package main

import "testing"

func TestMacKeychainKeyIsNeverCopiedToAFile(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	deviceKeyNeverCopiedToFile = true
	t.Cleanup(func() {
		deviceKeyNeverCopiedToFile = false
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
	setActiveServerProfile("cabin")
	if err := writeDeviceCredential(cabinKey); err != nil {
		t.Fatal(err)
	}

	migrated, err := migrateKeyringDeviceCredentialToFile()
	if err != nil || migrated {
		t.Fatalf("migrated=%v err=%v, want no copy and no error", migrated, err)
	}
	if !deviceCredentialFileModeForced || !deviceCredentialFileModeExplicit {
		t.Fatal("this computer must pair again into a file")
	}
	if deviceFileBackendMarkerExists() {
		t.Fatal("the file backend must not commit before the new pairing lands")
	}
	if got, ok, err := readKeyringSlotDirect(deviceCredentialServiceForProfile("cabin")); err != nil || !ok || got != cabinKey {
		t.Fatalf("the Keychain key must stay untouched: ok=%v err=%v", ok, err)
	}
	if got, ok, err := readKeyringSlotDirect(deviceCredentialServiceForProfile(defaultServerProfileName)); err != nil || !ok || got != defaultKey {
		t.Fatalf("the other profile's Keychain key must stay untouched: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := readDeviceCredential(); ok {
		t.Fatal("with the file backend forced, this profile reads as not paired, so setup pairs again")
	}
}

func TestMacNeverPairedComputerHasNothingToMove(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	deviceKeyNeverCopiedToFile = true
	t.Cleanup(func() { deviceKeyNeverCopiedToFile = false })
	if migrated, err := migrateKeyringDeviceCredentialToFile(); err != nil || migrated {
		t.Fatalf("migrated=%v err=%v", migrated, err)
	}
	if deviceCredentialFileModeForced {
		t.Fatal("an unpaired computer keeps its storage choice to the caller")
	}
}
