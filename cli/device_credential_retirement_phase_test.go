package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRevokedRetirementCheckpointResumesAfterCredentialDeletion(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	credential := validCredential(91)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	previous := runtimeConfig{
		ProfileID:          "profile-1",
		RelayInstanceID:    "relay-1",
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	if err := writeDeviceCredentialRetirementCheckpoint(
		paths,
		previous,
	); err != nil {
		t.Fatal(err)
	}
	checkpoint, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths)
	if err != nil || !exists {
		t.Fatalf("read checkpoint: exists=%v err=%v", exists, err)
	}
	checkpoint, err = markDeviceCredentialRetirementRevoked(
		paths,
		checkpoint,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := deleteDeviceCredential(); err != nil {
		t.Fatal(err)
	}
	revokeCalls := 0
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })

	resumed, err := resumeDeviceCredentialRetirementCheckpoint(
		paths,
		runtimeConfig{ProfileID: previous.ProfileID},
	)
	if err != nil || !resumed || revokeCalls != 0 {
		t.Fatalf(
			"resume revoked phase: resumed=%v calls=%d err=%v",
			resumed,
			revokeCalls,
			err,
		)
	}
	if _, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths); err != nil ||
		exists {
		t.Fatalf("checkpoint remains: exists=%v err=%v", exists, err)
	}
}

func TestRevokedRetirementCheckpointRejectsReplacementCredential(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceCredential(validCredential(92)); err != nil {
		t.Fatal(err)
	}
	previous := runtimeConfig{
		ProfileID:          "profile-1",
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	if err := writeDeviceCredentialRetirementCheckpoint(
		paths,
		previous,
	); err != nil {
		t.Fatal(err)
	}
	checkpoint, _, err := readDeviceCredentialRetirementCheckpoint(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = markDeviceCredentialRetirementRevoked(
		paths,
		checkpoint,
	); err != nil {
		t.Fatal(err)
	}
	replacement := validCredential(93)
	if err := writeDeviceCredential(replacement); err != nil {
		t.Fatal(err)
	}

	resumed, err := resumeDeviceCredentialRetirementCheckpoint(
		paths,
		runtimeConfig{ProfileID: previous.ProfileID},
	)
	if err == nil || !strings.Contains(err.Error(), "credential changed") ||
		resumed {
		t.Fatalf("replacement accepted: resumed=%v err=%v", resumed, err)
	}
	got, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || got != replacement {
		t.Fatalf(
			"replacement changed: got=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}
}

func TestRevokedRetirementCheckpointRejectsRestoredEndpoint(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	credential := validCredential(100)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	previous := runtimeConfig{
		ProfileID:          "profile-1",
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	if err := writeDeviceCredentialRetirementCheckpoint(
		paths,
		previous,
	); err != nil {
		t.Fatal(err)
	}
	checkpoint, _, err := readDeviceCredentialRetirementCheckpoint(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markDeviceCredentialRetirementRevoked(
		paths,
		checkpoint,
	); err != nil {
		t.Fatal(err)
	}

	resumed, err := resumeDeviceCredentialRetirementCheckpoint(
		paths,
		previous,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "endpoint was restored") ||
		resumed {
		t.Fatalf("restored endpoint accepted: resumed=%v err=%v", resumed, err)
	}
	if _, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths); err != nil ||
		!exists {
		t.Fatalf("checkpoint lost: exists=%v err=%v", exists, err)
	}
	got, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || got != credential {
		t.Fatalf(
			"credential changed: got=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}
}

func TestLegacyProfileGetsIdentityBeforeRetirementCheckpoint(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		paths.ConfigFile,
		[]byte(`{"schema_version":1,"relay_base_url":"http://relay:8791","relay_secure_base_url":"https://relay:8792","relay_spki_pin":"pin"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	credential := validCredential(94)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	previous := runtimeConfig{
		RelayBaseURL:       "http://relay:8791",
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		return errors.New("offline")
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })
	saveCalls := 0
	save := func(paths runtimePaths, cfg runtimeConfig) error {
		saveCalls++
		if saveCalls == 1 {
			return saveConfig(paths, cfg)
		}
		return errors.New("restore disk full")
	}

	if _, err := saveConfigBeforeDeviceRetirement(
		paths,
		previous,
		save,
	); err == nil {
		t.Fatal("retirement failure was not returned")
	}
	current, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths)
	if err != nil || !exists {
		t.Fatalf("checkpoint: exists=%v err=%v", exists, err)
	}
	if current.ProfileID == "" ||
		checkpoint.ProfileID != current.ProfileID {
		t.Fatalf(
			"profile identities diverged: config=%q checkpoint=%q",
			current.ProfileID,
			checkpoint.ProfileID,
		)
	}
}
