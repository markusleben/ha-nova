package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestPersistInteractiveSetupStateReturnsActionableLinuxKeyringError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalWrite := writeRelayAuthTokenForSetupPersistence
	defer func() {
		writeRelayAuthTokenForSetupPersistence = originalWrite
	}()
	writeRelayAuthTokenForSetupPersistence = func(string) error {
		return errors.New("The name org.freedesktop.secrets was not provided by any .service files")
	}

	nextCfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}
	nextState := installState{
		SchemaVersion:      stateSchemaVersion,
		InstalledClients:   []string{"hermes"},
		ClientInstallModes: map[string]string{},
	}

	err = persistInteractiveSetupState(paths, nextCfg, &nextState, "", false, "new-token")
	want := "cannot save relay token: secure storage unavailable on this Linux machine. Start a Secret Service provider (for example GNOME Keyring or KWallet Secrets) and rerun `ha-nova setup`"
	if err == nil || err.Error() != want {
		t.Fatalf("persistInteractiveSetupState() error = %v, want %q", err, want)
	}
}

func TestPersistInteractiveSetupStateReturnsActionableLinuxSessionBusError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalWrite := writeRelayAuthTokenForSetupPersistence
	defer func() {
		writeRelayAuthTokenForSetupPersistence = originalWrite
	}()
	writeRelayAuthTokenForSetupPersistence = func(string) error {
		return errors.New("dbus: couldn't determine address of session bus")
	}

	nextCfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}
	nextState := installState{
		SchemaVersion:      stateSchemaVersion,
		InstalledClients:   []string{"hermes"},
		ClientInstallModes: map[string]string{},
	}

	err = persistInteractiveSetupState(paths, nextCfg, &nextState, "", false, "new-token")
	want := "cannot save relay token: secure storage unavailable in this Linux session. Run HA NOVA from a terminal inside the Linux desktop session on this machine, and rerun `ha-nova setup`"
	if err == nil || err.Error() != want {
		t.Fatalf("persistInteractiveSetupState() error = %v, want %q", err, want)
	}
}

func TestPersistInteractiveSetupStateReturnsActionableLinuxKeyringInitializationError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalWrite := writeRelayAuthTokenForSetupPersistence
	defer func() {
		writeRelayAuthTokenForSetupPersistence = originalWrite
	}()
	writeRelayAuthTokenForSetupPersistence = func(string) error {
		return desktopKeyringLockedError("default Secret Service collection is locked")
	}

	nextCfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}
	nextState := installState{
		SchemaVersion:      stateSchemaVersion,
		InstalledClients:   []string{"hermes"},
		ClientInstallModes: map[string]string{},
	}

	err = persistInteractiveSetupState(paths, nextCfg, &nextState, "", false, "new-token")
	want := "cannot save relay token: secure storage is present but locked on this Linux machine. Unlock the default keyring and rerun `ha-nova setup`"
	if err == nil || err.Error() != want {
		t.Fatalf("persistInteractiveSetupState() error = %v, want %q", err, want)
	}
}

func TestPersistInteractiveSetupStateWithRecoveryRetriesOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalWrite := writeRelayAuthTokenForSetupPersistence
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		writeRelayAuthTokenForSetupPersistence = originalWrite
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	writeCalls := 0
	writeRelayAuthTokenForSetupPersistence = func(token string) error {
		writeCalls++
		if writeCalls == 1 {
			return desktopKeyringLockedError("default Secret Service collection is locked")
		}
		return originalWrite(token)
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}
	recoveryCalls := 0
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction, secret []byte) error {
		recoveryCalls++
		if action != platformSecureStorageRecoveryUnlock {
			t.Fatalf("unexpected recovery action %q", action)
		}
		if string(secret) != "linux-local-keyring" {
			t.Fatalf("unexpected recovery secret %q", string(secret))
		}
		return nil
	}
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
		return []byte("linux-local-keyring"), nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}
	state := installState{
		SchemaVersion:      stateSchemaVersion,
		InstalledClients:   []string{"hermes"},
		ClientInstallModes: map[string]string{},
	}
	recovery := setupSecureStorageRecoveryState{}

	if err := persistInteractiveSetupStateWithRecovery(bufio.NewReader(strings.NewReader("\n")), io.Discard, paths, cfg, &state, "", false, "new-token", &recovery); err != nil {
		t.Fatalf("persistInteractiveSetupStateWithRecovery() error = %v", err)
	}
	if recoveryCalls != 1 {
		t.Fatalf("expected one recovery call, got %d", recoveryCalls)
	}
	if writeCalls != 2 {
		t.Fatalf("expected one retry after recovery, got %d write calls", writeCalls)
	}
}

func TestPersistInteractiveSetupStateWithRecoveryDoesNotRepromptAfterAttempt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalWrite := writeRelayAuthTokenForSetupPersistence
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	defer func() {
		writeRelayAuthTokenForSetupPersistence = originalWrite
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
	}()

	writeRelayAuthTokenForSetupPersistence = func(string) error {
		return desktopKeyringLockedError("default Secret Service collection is locked")
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction, _ []byte) error {
		if action != platformSecureStorageRecoveryUnlock {
			t.Fatalf("unexpected recovery action %q", action)
		}
		t.Fatal("did not expect recovery once the run already attempted it")
		return nil
	}

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}
	state := installState{
		SchemaVersion:      stateSchemaVersion,
		InstalledClients:   []string{"hermes"},
		ClientInstallModes: map[string]string{},
	}
	recovery := setupSecureStorageRecoveryState{saveRetryAttempted: true}

	err = persistInteractiveSetupStateWithRecovery(bufio.NewReader(strings.NewReader("\n")), io.Discard, paths, cfg, &state, "", false, "new-token", &recovery)
	if err == nil {
		t.Fatal("expected setup persistence to fail without a second recovery prompt")
	}
}

func TestPersistInteractiveSetupStateWithRecoveryRetriesInitializationOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalWrite := writeRelayAuthTokenForSetupPersistence
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		writeRelayAuthTokenForSetupPersistence = originalWrite
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	writeCalls := 0
	writeRelayAuthTokenForSetupPersistence = func(token string) error {
		writeCalls++
		if writeCalls == 1 {
			return desktopKeyringInitializationRequiredError("no default Secret Service collection configured")
		}
		return originalWrite(token)
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}
	recoveryCalls := 0
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction, secret []byte) error {
		recoveryCalls++
		if action != platformSecureStorageRecoveryInitialize {
			t.Fatalf("unexpected recovery action %q", action)
		}
		if string(secret) != "linux-local-keyring" {
			t.Fatalf("unexpected recovery secret %q", string(secret))
		}
		return nil
	}
	reads := 0
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
		reads++
		return []byte("linux-local-keyring"), nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}
	state := installState{
		SchemaVersion:      stateSchemaVersion,
		InstalledClients:   []string{"hermes"},
		ClientInstallModes: map[string]string{},
	}
	recovery := setupSecureStorageRecoveryState{}

	if err := persistInteractiveSetupStateWithRecovery(bufio.NewReader(strings.NewReader("\n")), io.Discard, paths, cfg, &state, "", false, "new-token", &recovery); err != nil {
		t.Fatalf("persistInteractiveSetupStateWithRecovery() error = %v", err)
	}
	if recoveryCalls != 1 {
		t.Fatalf("expected one initialization recovery call, got %d", recoveryCalls)
	}
	if writeCalls != 2 {
		t.Fatalf("expected one failed save plus one retry, got %d writes", writeCalls)
	}
	if reads != 2 {
		t.Fatalf("expected create-password prompt plus confirmation, got %d reads", reads)
	}
}
