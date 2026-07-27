package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestInteractiveRetirementPreservesPairingWhenPersistenceFails(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	credential := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatalf("seed device credential: %v", err)
	}
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detect paths: %v", err)
	}
	cfg := runtimeConfig{
		RelayBaseURL:       "http://relay:8791",
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	originalSave := saveConfigForSetupPersistence
	saveConfigForSetupPersistence = func(runtimePaths, runtimeConfig) error {
		return errors.New("disk full")
	}
	t.Cleanup(func() { saveConfigForSetupPersistence = originalSave })
	revokeCalls := 0
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })

	state := defaultInstallState()
	err = persistInteractiveSetupStateWithMode(paths, cfg, &state, "", false, "", true)
	if err == nil {
		t.Fatal("persistence failure was not returned")
	}
	if revokeCalls != 0 {
		t.Fatalf("device was revoked before persistence committed: %d calls", revokeCalls)
	}
	if got, ok, readErr := readDeviceCredential(); readErr != nil || !ok || got != credential {
		t.Fatalf("device credential changed: got=%q ok=%v err=%v", got, ok, readErr)
	}
	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("load restored config: %v", err)
	}
	if saved.RelaySecureBaseURL != cfg.RelaySecureBaseURL || saved.RelaySpkiPin != cfg.RelaySpkiPin {
		t.Fatalf("paired config changed after failed persistence: %+v", saved)
	}
}

func TestNonInteractiveRetirementPreservesPairingWhenConfigSaveFails(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	credential := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatalf("seed device credential: %v", err)
	}
	paths := runtimePaths{
		ConfigDir:  t.TempDir(),
		ConfigFile: filepath.Join(t.TempDir(), "config.json"),
	}
	cfg := runtimeConfig{
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	revokeCalls := 0
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })

	_, err := saveConfigBeforeDeviceRetirement(paths, cfg, func(runtimePaths, runtimeConfig) error {
		return errors.New("disk full")
	})
	if err == nil {
		t.Fatal("config save failure was not returned")
	}
	if revokeCalls != 0 {
		t.Fatalf("device was revoked before config persistence committed: %d calls", revokeCalls)
	}
	if got, ok, readErr := readDeviceCredential(); readErr != nil || !ok || got != credential {
		t.Fatalf("device credential changed: got=%q ok=%v err=%v", got, ok, readErr)
	}
}

func TestDeviceRetirementPreservesCurrentCredentialWhenRevokeFails(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	credential := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatalf("seed device credential: %v", err)
	}
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		return errors.New("connection refused")
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })

	err := finalizeDeviceCredentialRetirement(runtimeConfig{
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	})
	if err == nil {
		t.Fatal("unconfirmed device revocation was accepted")
	}
	got, exists, readErr := readDeviceCredential()
	if readErr != nil || !exists || got != credential {
		t.Fatalf(
			"credential was not preserved: got=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}
}

func TestDeviceRetirementPreservesActivatedPendingWhenRevokeFails(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	credential := generateTestDeviceCredential(t)
	if err := writePendingDeviceCredential(credential); err != nil {
		t.Fatalf("seed pending device credential: %v", err)
	}
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(base, pin, gotCredential string) error {
		if base != "https://pending-relay:8792" ||
			pin != "pending-pin" ||
			gotCredential != credential {
			t.Fatalf(
				"pending revoke = %q %q %q",
				base,
				pin,
				gotCredential,
			)
		}
		return errors.New("connection refused")
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })

	err := finalizeDeviceCredentialRetirement(runtimeConfig{
		PendingSecureBaseURL: "https://pending-relay:8792",
		PendingSpkiPin:       "pending-pin",
	})
	if err == nil {
		t.Fatal("unconfirmed pending-device revocation was accepted")
	}
	got, exists, readErr := readPendingDeviceCredential()
	if readErr != nil || !exists || got != credential {
		t.Fatalf(
			"pending credential was not preserved: got=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}
}

func TestDeviceRetirementRevokeAttemptKeepsDurableRetryCheckpoint(
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
		t.Fatalf("seed device credential: %v", err)
	}
	previous := runtimeConfig{
		RelayBaseURL:       "http://relay:8791",
		ProfileID:          "profile-1",
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	if err := saveConfig(paths, previous); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	originalRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		return errors.New("connection refused")
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = originalRevoke })
	saveCalls := 0
	save := func(paths runtimePaths, cfg runtimeConfig) error {
		saveCalls++
		return saveConfig(paths, cfg)
	}

	_, err = saveConfigBeforeDeviceRetirement(paths, previous, save)
	if err == nil ||
		!strings.Contains(err.Error(), "cannot finish") ||
		saveCalls != 1 {
		t.Fatalf(
			"revoke attempt result saves=%d err=%v",
			saveCalls,
			err,
		)
	}
	current, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatalf("read cleared config: %v", err)
	}
	if !deviceRetirementEndpointsCleared(current) {
		t.Fatalf("test requires stranded cleared config: %+v", current)
	}
	checkpoint, exists, readErr :=
		readDeviceCredentialRetirementCheckpoint(paths)
	if readErr != nil ||
		!exists {
		t.Fatalf(
			"durable retirement checkpoint missing: exists=%v err=%v",
			exists,
			readErr,
		)
	}
	if checkpoint.Phase != deviceCredentialRetirementPrepared {
		t.Fatalf("checkpoint phase = %q", checkpoint.Phase)
	}
	if got, exists, readErr := readDeviceCredential(); readErr != nil ||
		!exists ||
		got != credential {
		t.Fatalf(
			"credential was not preserved: got=%q exists=%v err=%v",
			got,
			exists,
			readErr,
		)
	}

	revokeSelfDeviceV1ForRetire = func(base, pin, got string) error {
		if base != previous.RelaySecureBaseURL ||
			pin != previous.RelaySpkiPin ||
			got != credential {
			t.Fatalf("retry revoke = %q %q %q", base, pin, got)
		}
		return nil
	}
	resumed, err := resumeDeviceCredentialRetirementCheckpoint(paths, current)
	if err != nil || !resumed {
		t.Fatalf("resume retirement: resumed=%v err=%v", resumed, err)
	}
	if _, exists, readErr := readDeviceCredential(); readErr != nil || exists {
		t.Fatalf(
			"retired credential remains: exists=%v err=%v",
			exists,
			readErr,
		)
	}
	if _, exists, readErr :=
		readDeviceCredentialRetirementCheckpoint(paths); readErr != nil ||
		exists {
		t.Fatalf(
			"retirement checkpoint remains: exists=%v err=%v",
			exists,
			readErr,
		)
	}
}

func TestDeviceRetirementClearsStaleRelayIdentityTransactionally(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	cfg := runtimeConfig{
		RelayInstanceID:    "relay-before-token-switch",
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
	}
	var persisted runtimeConfig
	paths := runtimePaths{
		ConfigDir:  t.TempDir(),
		ConfigFile: filepath.Join(t.TempDir(), "config.json"),
	}
	updated, err := saveConfigBeforeDeviceRetirement(
		paths,
		cfg,
		func(_ runtimePaths, value runtimeConfig) error {
			persisted = value
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if updated.RelayInstanceID != "" ||
		persisted.RelayInstanceID != "" ||
		updated.RelaySecureBaseURL != "" ||
		persisted.RelaySpkiPin != "" {
		t.Fatalf(
			"retired device config updated=%+v persisted=%+v",
			updated,
			persisted,
		)
	}
}

func TestDeviceRetirementRejectsConfiguredCloudBeforeSave(t *testing.T) {
	cfg := runtimeConfig{
		RelayInstanceID:    "relay-cloud",
		RelaySecureBaseURL: "https://relay:8792",
		RelaySpkiPin:       "pin",
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateReady,
		},
	}
	saveCalls := 0
	paths := runtimePaths{
		ConfigDir:  t.TempDir(),
		ConfigFile: filepath.Join(t.TempDir(), "config.json"),
	}
	updated, err := saveConfigBeforeDeviceRetirement(
		paths,
		cfg,
		func(runtimePaths, runtimeConfig) error {
			saveCalls++
			return nil
		},
	)
	if err == nil || saveCalls != 0 {
		t.Fatalf(
			"Cloud retirement error=%v saves=%d updated=%+v",
			err,
			saveCalls,
			updated,
		)
	}
	if updated.RelayInstanceID != "relay-cloud" ||
		updated.RelaySecureBaseURL != "https://relay:8792" {
		t.Fatalf("blocked retirement changed config: %+v", updated)
	}
}
