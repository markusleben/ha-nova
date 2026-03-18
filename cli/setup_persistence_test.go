package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistInteractiveSetupStateRollsBackConfigAndTokenWhenStateSaveFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalCfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}
	if err := saveConfig(paths, originalCfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	originalState := installState{
		SchemaVersion:    stateSchemaVersion,
		Version:          "0.1.11",
		InstallSource:    "bundle",
		InstalledClients: []string{"claude"},
	}
	if err := saveState(paths, originalState); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}
	if err := writeRelayAuthToken("previous-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	originalSaveState := saveStateForSetupPersistence
	defer func() {
		saveStateForSetupPersistence = originalSaveState
	}()
	saveStateForSetupPersistence = func(paths runtimePaths, state installState) error {
		return errors.New("disk full")
	}

	nextCfg := runtimeConfig{
		HAHost:       "ha-box.local",
		HAURL:        "https://ha-box.local:9443/custom",
		RelayBaseURL: "http://ha-box.local:8791",
	}
	nextState := installState{
		SchemaVersion:      stateSchemaVersion,
		InstalledClients:   []string{"gemini"},
		ClientInstallModes: map[string]string{},
	}

	err = persistInteractiveSetupState(paths, nextCfg, &nextState, "previous-token", true, "new-token")
	if err == nil || err.Error() != "cannot save state: disk full" {
		t.Fatalf("expected state save failure, got %v", err)
	}

	restoredCfg, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error after rollback: %v", err)
	}
	if restoredCfg.HAHost != originalCfg.HAHost || restoredCfg.HAURL != originalCfg.HAURL || restoredCfg.RelayBaseURL != originalCfg.RelayBaseURL {
		t.Fatalf("config not restored after rollback: got %+v want %+v", restoredCfg, originalCfg)
	}

	restoredState, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error after rollback: %v", err)
	}
	if restoredState.Version != originalState.Version || len(restoredState.InstalledClients) != 1 || restoredState.InstalledClients[0] != "claude" {
		t.Fatalf("state not restored after rollback: %+v", restoredState)
	}

	restoredToken, err := readRelayAuthToken()
	if err != nil {
		t.Fatalf("readRelayAuthToken() error after rollback: %v", err)
	}
	if restoredToken != "previous-token" {
		t.Fatalf("token not restored after rollback: got %q", restoredToken)
	}

	configData, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("ReadFile(config) error: %v", err)
	}
	if len(configData) == 0 {
		t.Fatal("expected restored config file to remain non-empty")
	}
}

func TestPersistInteractiveSetupStateRestoresSnapshotWhenConfigSaveFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalCfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}
	if err := saveConfig(paths, originalCfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("previous-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	originalSaveConfig := saveConfigForSetupPersistence
	defer func() {
		saveConfigForSetupPersistence = originalSaveConfig
	}()
	saveConfigForSetupPersistence = func(paths runtimePaths, cfg runtimeConfig) error {
		_ = os.WriteFile(paths.ConfigFile, []byte("{"), 0o600)
		return errors.New("disk full")
	}

	nextCfg := runtimeConfig{
		HAHost:       "ha-box.local",
		HAURL:        "https://ha-box.local:9443/custom",
		RelayBaseURL: "http://ha-box.local:8791",
	}
	nextState := installState{
		SchemaVersion:      stateSchemaVersion,
		InstalledClients:   []string{"gemini"},
		ClientInstallModes: map[string]string{},
	}

	err = persistInteractiveSetupState(paths, nextCfg, &nextState, "previous-token", true, "new-token")
	if err == nil || err.Error() != "cannot save config: disk full" {
		t.Fatalf("expected config save failure, got %v", err)
	}

	restoredCfg, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error after rollback: %v", err)
	}
	if restoredCfg.HAHost != originalCfg.HAHost || restoredCfg.HAURL != originalCfg.HAURL || restoredCfg.RelayBaseURL != originalCfg.RelayBaseURL {
		t.Fatalf("config not restored after config-save rollback: got %+v want %+v", restoredCfg, originalCfg)
	}

	restoredToken, err := readRelayAuthToken()
	if err != nil {
		t.Fatalf("readRelayAuthToken() error after rollback: %v", err)
	}
	if restoredToken != "previous-token" {
		t.Fatalf("token not restored after config-save rollback: got %q", restoredToken)
	}
}
