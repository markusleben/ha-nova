package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRelayAuthTokenOverrideRoundTrip(t *testing.T) {
	overridePath := filepath.Join(t.TempDir(), ".test-relay-auth-token")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", overridePath)

	if overridden, err := writeRelayAuthTokenOverride("secret-token"); !overridden || err != nil {
		t.Fatalf("write override failed: overridden=%v err=%v", overridden, err)
	}
	token, overridden, err := readRelayAuthTokenOverride()
	if !overridden || err != nil {
		t.Fatalf("read override failed: overridden=%v err=%v", overridden, err)
	}
	if token != "secret-token" {
		t.Fatalf("unexpected token %q", token)
	}
	if overridden, err := deleteRelayAuthTokenOverride(); !overridden || err != nil {
		t.Fatalf("delete override failed: overridden=%v err=%v", overridden, err)
	}
	if _, err := os.Stat(overridePath); !isNotExist(err) {
		t.Fatalf("expected override file deleted, err=%v", err)
	}
}

func TestRelayAuthTokenOverrideRequiresExplicitOptIn(t *testing.T) {
	overridePath := filepath.Join(t.TempDir(), ".test-relay-auth-token")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", overridePath)

	if path := relayAuthTokenTestFile(); path != "" {
		t.Fatalf("expected override disabled without opt-in, got %q", path)
	}
}

func TestRelayAuthTokenProblemMessageDifferentiatesMissingAndUnavailable(t *testing.T) {
	if got := relayAuthTokenProblemMessage(missingRelayAuthTokenError("ha-nova.relay-auth-token")); got != "relay auth token missing; run: ha-nova setup" {
		t.Fatalf("missing-token message = %q", got)
	}
	if got := relayAuthTokenProblemMessage(errors.New("dbus: couldn't determine address of session bus")); got != "secure storage unavailable in this Linux session; run HA NOVA from a terminal inside the Linux desktop session on this machine, and then run: ha-nova setup" {
		t.Fatalf("secret-service-session-unavailable message = %q", got)
	}
	// Windows network logon sessions (issue #213): the raw Credential
	// Manager text must classify with local-session guidance.
	if got := relayAuthTokenProblemMessage(errors.New("cannot save relay token: A specified logon session does not exist. It may already have been terminated.")); got != "secure storage unavailable in this Windows session (network logon, for example SSH); run HA NOVA from a local interactive session (console or RDP) on this machine, and then run: ha-nova setup" {
		t.Fatalf("windows-network-logon message = %q", got)
	}
	if got := relayAuthTokenProblemMessage(errors.New("exec: \"dbus-launch\": executable file not found in $PATH")); got != "secure storage unavailable in this Linux session; run HA NOVA from a terminal inside the Linux desktop session on this machine, and then run: ha-nova setup" {
		t.Fatalf("secret-service-dbus-launch-missing message = %q", got)
	}
	if got := relayAuthTokenProblemMessage(errors.New("The name org.freedesktop.secrets was not provided by any .service files")); got != "secure storage unavailable on this Linux machine; start a Secret Service provider (for example GNOME Keyring or KWallet Secrets) and then run: ha-nova setup" {
		t.Fatalf("secret-service-unavailable message = %q", got)
	}
	if got := relayAuthTokenProblemMessage(errors.New("Secret Service preflight timed out")); got != "secure storage unavailable on this Linux machine; start a Secret Service provider (for example GNOME Keyring or KWallet Secrets) and then run: ha-nova setup" {
		t.Fatalf("secret-service-timeout message = %q", got)
	}
	if got := relayAuthTokenProblemMessage(errors.New("org.freedesktop.DBus.Error.ServiceUnknown: The name org.freedesktop.secrets was not provided by any .service files")); got != "secure storage unavailable on this Linux machine; start a Secret Service provider (for example GNOME Keyring or KWallet Secrets) and then run: ha-nova setup" {
		t.Fatalf("secret-service-serviceunknown message = %q", got)
	}
	lockedWant := "secure storage is present but locked on this Linux machine; unlock the default keyring and then run: ha-nova setup — or, if no one ever unlocks a desktop session on this machine, run: ha-nova setup --service <client>"
	if got := relayAuthTokenProblemMessage(desktopKeyringLockedError("default Secret Service collection is locked")); got != lockedWant {
		t.Fatalf("secret-service-locked message = %q", got)
	}
	initWant := "secure storage is present but not initialized on this Linux machine; initialize the default keyring and then run: ha-nova setup"
	if got := relayAuthTokenProblemMessage(errors.New("Object does not exist at path \"/org/freedesktop/secrets/collection/login\"")); got != initWant {
		t.Fatalf("secret-service-login-missing message = %q", got)
	}
	if got := relayAuthTokenProblemMessage(errors.New("no such secret collection")); got != initWant {
		t.Fatalf("secret-service-no-collection message = %q", got)
	}
	if got := relayAuthTokenProblemMessage(errors.New("no default Secret Service collection configured")); got != initWant {
		t.Fatalf("secret-service-no-default message = %q", got)
	}
	if got := relayAuthTokenProblemMessage(errors.New("keychain locked")); got != "relay auth token unavailable: keychain locked" {
		t.Fatalf("unavailable-token message = %q", got)
	}
}

func TestRelayAuthTokenSetupPreflightSkipsPlatformCheckForInsecureTestOverride(t *testing.T) {
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(t.TempDir(), ".test-relay-auth-token"))

	originalPlatformPreflight := relayAuthTokenPlatformSetupPreflight
	defer func() {
		relayAuthTokenPlatformSetupPreflight = originalPlatformPreflight
	}()
	relayAuthTokenPlatformSetupPreflight = func() error {
		t.Fatal("did not expect platform keyring preflight when insecure test override is enabled")
		return nil
	}

	if err := relayAuthTokenSetupPreflight(); err != nil {
		t.Fatalf("relayAuthTokenSetupPreflight() error = %v", err)
	}
}

func TestRelayAuthTokenSetupSaveErrorExplainsWindowsNetworkLogonSession(t *testing.T) {
	// Issue #213: Credential Manager writes fail in network logon sessions
	// (PowerShell over OpenSSH) with this raw OS text.
	err := relayAuthTokenSetupSaveError(errors.New("A specified logon session does not exist. It may already have been terminated."))
	want := "cannot save relay token: secure storage unavailable in this Windows session (network logon, for example SSH). Run HA NOVA from a local interactive session (console or RDP) on this machine, and rerun `ha-nova setup`"
	if err == nil || err.Error() != want {
		t.Fatalf("relayAuthTokenSetupSaveError() = %v, want %q", err, want)
	}
}

func TestRelayAuthTokenSetupSaveErrorExplainsDesktopKeyringUnavailable(t *testing.T) {
	err := relayAuthTokenSetupSaveError(errors.New("The name org.freedesktop.secrets was not provided by any .service files"))
	want := "cannot save relay token: secure storage unavailable on this Linux machine. Start a Secret Service provider (for example GNOME Keyring or KWallet Secrets) and rerun `ha-nova setup`"
	if err == nil || err.Error() != want {
		t.Fatalf("relayAuthTokenSetupSaveError() = %v, want %q", err, want)
	}
}

func TestRelayAuthTokenSetupSaveErrorExplainsLinuxSessionBusMissing(t *testing.T) {
	err := relayAuthTokenSetupSaveError(errors.New("dbus: couldn't determine address of session bus"))
	want := "cannot save relay token: secure storage unavailable in this Linux session. Run HA NOVA from a terminal inside the Linux desktop session on this machine, and rerun `ha-nova setup`"
	if err == nil || err.Error() != want {
		t.Fatalf("relayAuthTokenSetupSaveError() = %v, want %q", err, want)
	}
}

func TestRelayAuthTokenSetupSaveErrorExplainsLinuxKeyringInitialization(t *testing.T) {
	err := relayAuthTokenSetupSaveError(errors.New("no default Secret Service collection configured"))
	want := "cannot save relay token: secure storage is present but not initialized on this Linux machine. Initialize the default keyring and rerun `ha-nova setup`"
	if err == nil || err.Error() != want {
		t.Fatalf("relayAuthTokenSetupSaveError() = %v, want %q", err, want)
	}
}

func TestRelayAuthTokenSetupSaveErrorExplainsLinuxKeyringLocked(t *testing.T) {
	err := relayAuthTokenSetupSaveError(desktopKeyringLockedError("default Secret Service collection is locked"))
	want := "cannot save relay token: secure storage is present but locked on this Linux machine. Unlock the default keyring and rerun `ha-nova setup`"
	if err == nil || err.Error() != want {
		t.Fatalf("relayAuthTokenSetupSaveError() = %v, want %q", err, want)
	}
}

func TestRelayAuthTokenFileRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	path := filepath.Join(t.TempDir(), "ha-nova", "relay-token")

	if err := writeRelayAuthTokenFile(path, "secret-token"); err != nil {
		t.Fatalf("writeRelayAuthTokenFile() error = %v", err)
	}
	token, err := readRelayAuthTokenFile(path)
	if err != nil {
		t.Fatalf("readRelayAuthTokenFile() error = %v", err)
	}
	if token != "secret-token" {
		t.Fatalf("token = %q, want secret-token", token)
	}
}

func TestRelayAuthTokenUsesConfiguredServiceFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	cfg := runtimeConfig{
		HAHost:         "192.168.1.5",
		HAURL:          "http://192.168.1.5:8123",
		RelayBaseURL:   "http://192.168.1.5:8791",
		RelayTokenFile: defaultRelayAuthTokenFile(paths),
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	if err := writeRelayAuthToken("service-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error = %v", err)
	}
	token, err := readRelayAuthToken()
	if err != nil {
		t.Fatalf("readRelayAuthToken() error = %v", err)
	}
	if token != "service-token" {
		t.Fatalf("token = %q, want service-token", token)
	}
	if _, err := os.Stat(cfg.RelayTokenFile); err != nil {
		t.Fatalf("expected service token file: %v", err)
	}
}

func TestRelayAuthTokenFileRejectsUnsafePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "relay-token")
	if err := os.WriteFile(path, []byte("secret-token"), 0o644); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	_, err := readRelayAuthTokenFile(path)
	if err == nil || !strings.Contains(err.Error(), "permissions must be 0600 or stricter") {
		t.Fatalf("expected permission error, got %v", err)
	}
	if err := writeRelayAuthTokenFile(path, "new-secret-token"); err == nil || !strings.Contains(err.Error(), "permissions must be 0600 or stricter") {
		t.Fatalf("expected write to reject unsafe existing file, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.Contains(string(data), "new-secret-token") {
		t.Fatalf("write leaked token into unsafe existing file")
	}
}

func TestRelayAuthTokenFileWriteHardensUnsafeOwnedParentPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	dir := filepath.Join(t.TempDir(), "ha-nova")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("mkdir token dir: %v", err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod token dir: %v", err)
	}

	path := filepath.Join(dir, "relay-token")
	if err := writeRelayAuthTokenFile(path, "secret-token"); err != nil {
		t.Fatalf("writeRelayAuthTokenFile() error = %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat token dir: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("expected parent directory to be hardened, mode=%o", info.Mode().Perm())
	}
	token, err := readRelayAuthTokenFile(path)
	if err != nil {
		t.Fatalf("readRelayAuthTokenFile() error = %v", err)
	}
	if token != "secret-token" {
		t.Fatalf("token = %q, want secret-token", token)
	}
}

func TestRelayAuthTokenFileRejectsNativeWindowsBeforeWrite(t *testing.T) {
	originalPlatform := relayAuthTokenFilePlatformOS
	defer func() {
		relayAuthTokenFilePlatformOS = originalPlatform
	}()
	relayAuthTokenFilePlatformOS = "windows"

	path := filepath.Join(t.TempDir(), "relay-token")
	err := writeRelayAuthTokenFile(path, "secret-token")
	if err == nil || !strings.Contains(err.Error(), "service token files are not supported on native Windows") {
		t.Fatalf("expected native Windows rejection, got %v", err)
	}
	if _, err := os.Stat(path); !isNotExist(err) {
		t.Fatalf("expected token file not to be written on native Windows, err=%v", err)
	}
}

func TestRelayAuthTokenFileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target-token")
	link := filepath.Join(dir, "relay-token")
	if err := os.WriteFile(target, []byte("secret-token"), 0o600); err != nil {
		t.Fatalf("write target token: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := readRelayAuthTokenFile(link)
	if err == nil || !strings.Contains(err.Error(), "symlink not allowed") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestRelayAuthTokenFileRejectsEmptyTokenWithoutLeakingSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	path := filepath.Join(t.TempDir(), "relay-token")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	_, err := readRelayAuthTokenFile(path)
	if err == nil || !strings.Contains(err.Error(), "empty file") {
		t.Fatalf("expected empty-token error, got %v", err)
	}
	if strings.Contains(relayAuthTokenProblemMessage(err), "secret-token") {
		t.Fatalf("problem message leaked token: %s", relayAuthTokenProblemMessage(err))
	}
}

func TestRelayTokenStorageReadsFailLoudWhenConfigUnreadable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}

	// Reads must fail loud instead of silently falling back to the OS
	// keyring (issue #200: that fallback can hang on headless Linux).
	_, overridden, err := readRelayAuthTokenFileOverride()
	if !overridden {
		t.Fatalf("expected unreadable config to be handled by the token-file path, got overridden=false")
	}
	if !errors.Is(err, errRelayTokenStorageConfigUnreadable) {
		t.Fatalf("expected config-unreadable error, got %v", err)
	}

	// Writes and deletes fall back to the platform keystore so that
	// `ha-nova setup` can still repair an unreadable config.
	if handled, err := writeRelayAuthTokenFileOverride("token"); handled || err != nil {
		t.Fatalf("expected write to skip the token-file path, got handled=%v err=%v", handled, err)
	}
	if handled, err := deleteRelayAuthTokenFileOverride(); handled || err != nil {
		t.Fatalf("expected delete to skip the token-file path, got handled=%v err=%v", handled, err)
	}

	// A missing config keeps the normal keyring fallback for reads.
	if err := os.Remove(paths.ConfigFile); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	if _, overridden, err := readRelayAuthTokenFileOverride(); overridden || err != nil {
		t.Fatalf("expected missing config to fall back to keyring, got overridden=%v err=%v", overridden, err)
	}
}

func TestRelayTokenStorageHonorsTokenFileWithoutRelayBaseURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	tokenPath := filepath.Join(paths.ConfigDir, "relay-token")
	if err := writeRelayAuthTokenFile(tokenPath, "configured-token"); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"relay_token_file":"relay-token"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	token, overridden, err := readRelayAuthTokenFileOverride()
	if !overridden || err != nil {
		t.Fatalf("expected token file to be honored, got overridden=%v err=%v", overridden, err)
	}
	if token != "configured-token" {
		t.Fatalf("unexpected token %q", token)
	}
}

func TestRelayAuthTokenFileSuppressionRoutesToKeyring(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"relay_token_file":"relay-token"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, ok, err := relayAuthTokenFilePathFromConfig(); !ok || err != nil {
		t.Fatalf("expected token file to be configured, got ok=%v err=%v", ok, err)
	}

	restore := withRelayAuthTokenFileSuppressed()
	defer restore()
	if path, ok, err := relayAuthTokenFilePathFromConfig(); ok || err != nil || path != "" {
		t.Fatalf("expected suppression to report no token file, got path=%q ok=%v err=%v", path, ok, err)
	}
}
