package main

import (
	"os"
	"strings"
	"testing"
)

func TestDeviceRetirementCheckpointRejectsReplacementCredential(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	oldCredential := validCredential(88)
	if err := writeDeviceCredential(oldCredential); err != nil {
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
	replacement := validCredential(89)
	if err := writeDeviceCredential(replacement); err != nil {
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
	if err == nil ||
		!strings.Contains(err.Error(), "credential changed") ||
		resumed ||
		revokeCalls != 0 {
		t.Fatalf(
			"replacement binding: resumed=%v calls=%d err=%v",
			resumed,
			revokeCalls,
			err,
		)
	}
	got, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || got != replacement {
		t.Fatalf(
			"replacement credential changed: got=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}
}

func TestDeviceRetirementCheckpointRejectsMissingExpectedCredential(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	credential := validCredential(90)
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
	if err == nil ||
		!strings.Contains(err.Error(), "presence changed") ||
		resumed ||
		revokeCalls != 0 {
		t.Fatalf(
			"missing credential binding: resumed=%v calls=%d err=%v",
			resumed,
			revokeCalls,
			err,
		)
	}
	if _, exists, readErr :=
		readDeviceCredentialRetirementCheckpoint(paths); readErr != nil ||
		!exists {
		t.Fatalf(
			"checkpoint was not preserved: exists=%v err=%v",
			exists,
			readErr,
		)
	}
}

func TestDeviceRetirementCheckpointRejectsNewCloudState(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	credential := generateTestDeviceCredential(t)
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
	revokeCalls := 0
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })

	resumed, err := resumeDeviceCredentialRetirementCheckpoint(
		paths,
		runtimeConfig{
			ProfileID: previous.ProfileID,
			Cloud: &cloudLifecycleMetadata{
				State: cloudStateReady,
			},
		},
	)
	if err == nil ||
		!strings.Contains(err.Error(), "Cloud state appeared") ||
		resumed ||
		revokeCalls != 0 {
		t.Fatalf(
			"Cloud identity guard: resumed=%v calls=%d err=%v",
			resumed,
			revokeCalls,
			err,
		)
	}
}

func TestSetupFailsClosedOnUnreadableConfigWithRetirementCheckpoint(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	credential := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	previous := runtimeConfig{
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	if err := writeDeviceCredentialRetirementCheckpoint(
		paths,
		previous,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	revokeCalls := 0
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })

	exitCode, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{"--non-interactive"})
	})
	if exitCode == 0 ||
		!strings.Contains(output, "retirement is pending") ||
		revokeCalls != 0 {
		t.Fatalf(
			"corrupt-config guard: exit=%d calls=%d output=%q",
			exitCode,
			revokeCalls,
			output,
		)
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
