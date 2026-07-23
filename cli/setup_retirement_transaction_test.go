package main

import (
	"errors"
	"path/filepath"
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
