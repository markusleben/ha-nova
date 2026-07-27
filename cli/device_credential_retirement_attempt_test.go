package main

import (
	"errors"
	"testing"
)

func TestCurrentRevokeThenPendingFailureNeverRestoresConfig(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	currentCredential := validCredential(98)
	pendingCredential := validCredential(99)
	if err := writeDeviceCredential(currentCredential); err != nil {
		t.Fatal(err)
	}
	if err := writePendingDeviceCredential(pendingCredential); err != nil {
		t.Fatal(err)
	}
	previous := runtimeConfig{
		ProfileID:            "profile-1",
		RelayInstanceID:      "relay-1",
		RelayBaseURL:         "http://relay:8791",
		RelaySecureBaseURL:   "https://relay:8792",
		RelaySpkiPin:         "pin",
		PendingSecureBaseURL: "https://pending:8792",
		PendingSpkiPin:       "pending-pin",
	}
	if err := saveConfig(paths, previous); err != nil {
		t.Fatal(err)
	}
	revokeCalls := 0
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(
		_, _,
		credential string,
	) error {
		revokeCalls++
		if credential == currentCredential {
			return nil
		}
		return errors.New("pending offline")
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })
	saveCalls := 0
	save := func(paths runtimePaths, cfg runtimeConfig) error {
		saveCalls++
		return saveConfig(paths, cfg)
	}

	if _, err := saveConfigBeforeDeviceRetirement(
		paths,
		previous,
		save,
	); err == nil {
		t.Fatal("pending revoke failure was accepted")
	}
	if revokeCalls != 2 || saveCalls != 1 {
		t.Fatalf(
			"revokes=%d saves=%d",
			revokeCalls,
			saveCalls,
		)
	}
	current, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !deviceRetirementEndpointsCleared(current) {
		t.Fatalf("cleared config was restored: %+v", current)
	}
	checkpoint, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths)
	if err != nil || !exists ||
		checkpoint.Phase != deviceCredentialRetirementPrepared {
		t.Fatalf(
			"checkpoint=%+v exists=%v err=%v",
			checkpoint,
			exists,
			err,
		)
	}
	if got, exists, err := readDeviceCredential(); err != nil ||
		!exists ||
		got != currentCredential {
		t.Fatalf(
			"current slot changed: got=%q exists=%v err=%v",
			got,
			exists,
			err,
		)
	}
	if got, exists, err := readPendingDeviceCredential(); err != nil ||
		!exists ||
		got != pendingCredential {
		t.Fatalf(
			"pending slot changed: got=%q exists=%v err=%v",
			got,
			exists,
			err,
		)
	}
}
