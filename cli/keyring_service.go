package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var errRelayAuthTokenMissing = errors.New("relay auth token missing")
var errDesktopKeyringSessionUnavailable = errors.New("desktop keyring session unavailable")
var errDesktopKeyringUnavailable = errors.New("desktop keyring unavailable")
var errDesktopKeyringSetupRequired = errors.New("desktop keyring setup required")
var errDesktopKeyringLocked = errors.New("desktop keyring locked")
var errDesktopKeyringInitializationRequired = errors.New("desktop keyring initialization required")
var errRelayAuthTokenFileInvalid = errors.New("relay token file invalid")
var errRelayTokenStorageConfigUnreadable = errors.New("relay token storage config unreadable")

var relayAuthTokenFilePathOverride string
var relayAuthTokenFileSuppressed bool
var relayAuthTokenFilePlatformOS = runtime.GOOS

// withRelayAuthTokenFileSuppressed routes token reads/deletes to the OS
// credential store even when the config references a token file. Purge uses
// it after the token file was already removed so a leftover desktop-mode
// keyring entry still gets cleaned up.
func withRelayAuthTokenFileSuppressed() func() {
	previous := relayAuthTokenFileSuppressed
	relayAuthTokenFileSuppressed = true
	return func() { relayAuthTokenFileSuppressed = previous }
}

func relayAuthTokenServiceName() string {
	if override := strings.TrimSpace(os.Getenv("HA_NOVA_KEYRING_SERVICE")); override != "" {
		return override
	}
	return keyringServiceName
}

func relayAuthTokenTestFile() string {
	if os.Getenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING") != "1" {
		return ""
	}
	if override := strings.TrimSpace(os.Getenv("HA_NOVA_TEST_KEYRING_FILE")); override != "" {
		return override
	}
	return ""
}

func readRelayAuthTokenOverride() (string, bool, error) {
	path := relayAuthTokenTestFile()
	if path == "" {
		return "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return "", true, os.ErrNotExist
		}
		return "", true, err
	}
	return strings.TrimSpace(string(data)), true, nil
}

func relayAuthTokenFileServiceName(path string) string {
	return "service token file " + path
}

func defaultRelayAuthTokenFile(paths runtimePaths) string {
	return filepath.Join(paths.ConfigDir, "relay-token")
}

func relayAuthTokenFilePathFromConfig() (string, bool, error) {
	if relayAuthTokenFilePathOverride != "" {
		path := relayAuthTokenFilePathOverride
		if !filepath.IsAbs(path) {
			paths, err := detectPaths()
			if err != nil {
				return "", false, err
			}
			path = filepath.Join(paths.ConfigDir, path)
		}
		return filepath.Clean(path), true, nil
	}
	if relayAuthTokenFileSuppressed {
		return "", false, nil
	}
	paths, err := detectPaths()
	if err != nil {
		return "", false, err
	}
	// Read the raw config instead of loadConfig: token storage must not
	// depend on relay_base_url being set, and an unreadable config must
	// fail loud instead of silently falling back to the OS keyring — on
	// headless Linux that fallback can hang in Secret Service unlock
	// prompts even though a token file is configured (issue #200).
	cfg, err := loadJSONConfig(paths.ConfigFile)
	if err != nil {
		if isNotExist(err) {
			return "", false, nil
		}
		return "", true, fmt.Errorf("%w: %v (fix or remove the config file, or rerun: ha-nova setup)", errRelayTokenStorageConfigUnreadable, err)
	}
	path := strings.TrimSpace(cfg.RelayTokenFile)
	if path == "" {
		return "", false, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(paths.ConfigDir, path)
	}
	return filepath.Clean(path), true, nil
}

func readRelayAuthTokenFile(path string) (string, error) {
	if err := validateRelayAuthTokenFile(path); err != nil {
		if isNotExist(err) {
			return "", missingRelayAuthTokenError(relayAuthTokenFileServiceName(path))
		}
		return "", relayAuthTokenReadError(relayAuthTokenFileServiceName(path), err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", relayAuthTokenReadError(relayAuthTokenFileServiceName(path), err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", relayAuthTokenReadError(relayAuthTokenFileServiceName(path), fmt.Errorf("%w: empty file", errRelayAuthTokenFileInvalid))
	}
	return token, nil
}

func writeRelayAuthTokenFile(path, token string) error {
	if relayAuthTokenFilePlatformOS == "windows" {
		return fmt.Errorf("cannot write relay auth token: %w: service token files are not supported on native Windows", errRelayAuthTokenFileInvalid)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("cannot write relay auth token: %w: empty token", errRelayAuthTokenFileInvalid)
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("cannot write relay auth token: %w", err)
	}
	if err := hardenRelayAuthTokenFileParent(parent); err != nil {
		return fmt.Errorf("cannot write relay auth token: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot write relay auth token: %w: symlink not allowed", errRelayAuthTokenFileInvalid)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot write relay auth token: %w: not a regular file", errRelayAuthTokenFileInvalid)
		}
		if err := validateRelayAuthTokenFile(path); err != nil {
			return fmt.Errorf("cannot write relay auth token: %w", err)
		}
	} else if !isNotExist(err) {
		return fmt.Errorf("cannot write relay auth token: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("cannot write relay auth token: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("cannot write relay auth token: %w", err)
	}
	return nil
}

func deleteRelayAuthTokenFile(path string) error {
	if err := os.Remove(path); err != nil && !isNotExist(err) {
		return fmt.Errorf("cannot delete relay auth token: %w", err)
	}
	return nil
}

func validateRelayAuthTokenFile(path string) error {
	if relayAuthTokenFilePlatformOS == "windows" {
		return fmt.Errorf("%w: service token files are not supported on native Windows", errRelayAuthTokenFileInvalid)
	}
	parent := filepath.Dir(path)
	if err := validateRelayAuthTokenFileParent(parent); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: symlink not allowed", errRelayAuthTokenFileInvalid)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file", errRelayAuthTokenFileInvalid)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: permissions must be 0600 or stricter", errRelayAuthTokenFileInvalid)
	}
	if err := validateRelayAuthTokenFileOwner(info); err != nil {
		return err
	}
	return nil
}

func hardenRelayAuthTokenFileParent(parent string) error {
	info, err := os.Stat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: parent is not a directory", errRelayAuthTokenFileInvalid)
	}
	if err := validateRelayAuthTokenFileOwner(info); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		if err := os.Chmod(parent, 0o700); err != nil {
			return err
		}
	}
	return validateRelayAuthTokenFileParent(parent)
}

func validateRelayAuthTokenFileParent(parent string) error {
	info, err := os.Stat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: parent is not a directory", errRelayAuthTokenFileInvalid)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: parent directory must not be group/world writable", errRelayAuthTokenFileInvalid)
	}
	if err := validateRelayAuthTokenFileOwner(info); err != nil {
		return err
	}
	return nil
}

func relayAuthTokenFileEnabled() bool {
	_, ok, err := relayAuthTokenFilePathFromConfig()
	return ok && err == nil
}

func missingRelayAuthTokenError(service string) error {
	return fmt.Errorf("%w (%s)", errRelayAuthTokenMissing, service)
}

func relayAuthTokenReadError(service string, err error) error {
	return fmt.Errorf("cannot read relay auth token (%s): %w", service, err)
}

func wrapDesktopKeyringError(kind error, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return kind
	}
	return fmt.Errorf("%w: %s", kind, detail)
}

func desktopKeyringSessionUnavailableError(detail string) error {
	return wrapDesktopKeyringError(errDesktopKeyringSessionUnavailable, detail)
}

func desktopKeyringUnavailableError(detail string) error {
	return wrapDesktopKeyringError(errDesktopKeyringUnavailable, detail)
}

func desktopKeyringSetupRequiredError(detail string) error {
	return wrapDesktopKeyringError(errDesktopKeyringSetupRequired, detail)
}

func desktopKeyringLockedError(detail string) error {
	return wrapDesktopKeyringError(errDesktopKeyringLocked, detail)
}

func desktopKeyringInitializationRequiredError(detail string) error {
	return wrapDesktopKeyringError(errDesktopKeyringInitializationRequired, detail)
}

func isMissingRelayAuthTokenError(err error) bool {
	return errors.Is(err, errRelayAuthTokenMissing)
}

func relayAuthTokenProblemMessage(err error) string {
	if err == nil {
		return ""
	}
	if isMissingRelayAuthTokenError(err) {
		return "relay auth token missing; run: ha-nova setup"
	}
	if isWindowsNetworkLogonSessionError(err) {
		return "secure storage unavailable in this Windows session (network logon, for example SSH); run HA NOVA from a local interactive session (console or RDP) on this machine, and then run: ha-nova setup"
	}
	if isDesktopKeyringSessionUnavailableError(err) {
		return "secure storage unavailable in this Linux session; run HA NOVA from a terminal inside the Linux desktop session on this machine, and then run: ha-nova setup"
	}
	if isDesktopKeyringUnavailableError(err) {
		return "secure storage unavailable on this Linux machine; start a Secret Service provider (for example GNOME Keyring or KWallet Secrets) and then run: ha-nova setup"
	}
	if isDesktopKeyringInitializationRequiredError(err) {
		return "secure storage is present but not initialized on this Linux machine; initialize the default keyring and then run: ha-nova setup"
	}
	if isDesktopKeyringLockedError(err) {
		return "secure storage is present but locked on this Linux machine; unlock the default keyring and then run: ha-nova setup — or, if no one ever unlocks a desktop session on this machine, run: ha-nova setup --service <client>"
	}
	if isDesktopKeyringSetupRequiredError(err) {
		return "secure storage is present but not ready on this Linux machine; rerun `ha-nova setup` interactively to finish local secure storage setup"
	}
	if errors.Is(err, errRelayAuthTokenFileInvalid) {
		return fmt.Sprintf("service token file is not usable: %s", err)
	}
	return fmt.Sprintf("relay auth token unavailable: %s", err)
}

// isWindowsNetworkLogonSessionError matches the Credential Manager failure
// in Windows network logon sessions (for example PowerShell over OpenSSH):
// there is no interactive logon session to hold the credential store. Local
// console, Windows Terminal, and RDP sessions are unaffected. String-matched
// here (not build-tagged) so every platform classifies forwarded errors and
// tests run everywhere.
func isWindowsNetworkLogonSessionError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "a specified logon session does not exist")
}

func isDesktopKeyringUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errDesktopKeyringUnavailable) {
		return true
	}
	message := strings.ToLower(err.Error())
	return (strings.Contains(message, "org.freedesktop.secrets") &&
		(strings.Contains(message, "not provided") || strings.Contains(message, "serviceunknown"))) ||
		strings.Contains(message, "secret service preflight timed out")
}

func isDesktopKeyringSessionUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errDesktopKeyringSessionUnavailable) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "couldn't determine address of session bus") ||
		(strings.Contains(message, "dbus-launch") && strings.Contains(message, "not found"))
}

func isDesktopKeyringSetupRequiredError(err error) bool {
	if err == nil {
		return false
	}
	if isDesktopKeyringLockedError(err) || isDesktopKeyringInitializationRequiredError(err) {
		return true
	}
	if errors.Is(err, errDesktopKeyringSetupRequired) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "failed to unlock correct collection") ||
		strings.Contains(message, "secure storage is present but not ready on this linux machine") ||
		(strings.Contains(message, "object does not exist at path") &&
			(strings.Contains(message, "/org/freedesktop/secrets/collection/login") ||
				strings.Contains(message, "/org/freedesktop/secrets/aliases/default"))) ||
		strings.Contains(message, "no such secret collection")
}

func isDesktopKeyringLockedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errDesktopKeyringLocked) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "default secret service collection is locked") ||
		strings.Contains(message, "secure storage is present but locked on this linux machine") ||
		strings.Contains(message, "local secure storage is still locked on this linux machine") ||
		strings.Contains(message, "password was invalid")
}

func isDesktopKeyringInitializationRequiredError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errDesktopKeyringInitializationRequired) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no default secret service collection configured") ||
		strings.Contains(message, "secure storage is present but not initialized on this linux machine") ||
		strings.Contains(message, "local secure storage still needs one-time setup on this linux machine") ||
		(strings.Contains(message, "object does not exist at path") &&
			(strings.Contains(message, "/org/freedesktop/secrets/collection/login") ||
				strings.Contains(message, "/org/freedesktop/secrets/aliases/default"))) ||
		strings.Contains(message, "no such secret collection")
}

func errorsIsContextDeadlineExceeded(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded")
}

func relayAuthTokenSetupSaveError(err error) error {
	return relayAuthTokenSetupOperationError("save relay token", err)
}

func relayAuthTokenSetupReadError(err error) error {
	return relayAuthTokenSetupOperationError("access saved relay token", err)
}

func relayAuthTokenSetupOperationError(action string, err error) error {
	if isWindowsNetworkLogonSessionError(err) {
		return fmt.Errorf("cannot %s: secure storage unavailable in this Windows session (network logon, for example SSH). Run HA NOVA from a local interactive session (console or RDP) on this machine, and rerun `ha-nova setup`", action)
	}
	if isDesktopKeyringSessionUnavailableError(err) {
		return fmt.Errorf("cannot %s: secure storage unavailable in this Linux session. Run HA NOVA from a terminal inside the Linux desktop session on this machine, and rerun `ha-nova setup`", action)
	}
	if isDesktopKeyringUnavailableError(err) {
		return fmt.Errorf("cannot %s: secure storage unavailable on this Linux machine. Start a Secret Service provider (for example GNOME Keyring or KWallet Secrets) and rerun `ha-nova setup`", action)
	}
	if isDesktopKeyringInitializationRequiredError(err) {
		return fmt.Errorf("cannot %s: secure storage is present but not initialized on this Linux machine. Initialize the default keyring and rerun `ha-nova setup`", action)
	}
	if isDesktopKeyringLockedError(err) {
		return fmt.Errorf("cannot %s: secure storage is present but locked on this Linux machine. Unlock the default keyring and rerun `ha-nova setup`", action)
	}
	if isDesktopKeyringSetupRequiredError(err) {
		return fmt.Errorf("cannot %s: secure storage is present but not ready on this Linux machine. Rerun `ha-nova setup` interactively to finish local secure storage setup", action)
	}
	return fmt.Errorf("cannot %s: %s", action, err)
}

func localSecureStorageRecoveryError(err error) error {
	if errors.Is(err, errLocalSecureStoragePasswordRejected) {
		return err
	}
	err = normalizeLinuxKeyringError(err)
	if isDesktopKeyringSessionUnavailableError(err) {
		return desktopKeyringSessionUnavailableError("local secure storage is unavailable in this Linux session")
	}
	if isDesktopKeyringUnavailableError(err) {
		return desktopKeyringUnavailableError("local secure storage backend is unavailable on this Linux machine")
	}
	if isDesktopKeyringInitializationRequiredError(err) {
		return desktopKeyringInitializationRequiredError("local secure storage still needs one-time setup on this Linux machine")
	}
	if isDesktopKeyringLockedError(err) {
		return desktopKeyringLockedError("local secure storage is still locked on this Linux machine")
	}
	if isDesktopKeyringSetupRequiredError(err) {
		return desktopKeyringSetupRequiredError("local secure storage is still not ready on this Linux machine")
	}
	return fmt.Errorf("local secure storage verification failed: %s", err)
}

func normalizeLinuxKeyringError(err error) error {
	if err == nil {
		return nil
	}
	if classified := classifyAmbiguousDesktopKeyringSetupError(err); classified != nil {
		return classified
	}
	return normalizeLinuxKeyringErrorWithoutAmbiguousClassification(err)
}

func normalizeLinuxKeyringErrorWithoutAmbiguousClassification(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case isDesktopKeyringSessionUnavailableError(err):
		return desktopKeyringSessionUnavailableError(err.Error())
	case isDesktopKeyringUnavailableError(err):
		return desktopKeyringUnavailableError(err.Error())
	case isDesktopKeyringInitializationRequiredError(err):
		return desktopKeyringInitializationRequiredError(err.Error())
	case isDesktopKeyringLockedError(err):
		return desktopKeyringLockedError(err.Error())
	case isDesktopKeyringSetupRequiredError(err):
		return desktopKeyringSetupRequiredError(err.Error())
	default:
		return err
	}
}

func writeRelayAuthTokenOverride(token string) (bool, error) {
	path := relayAuthTokenTestFile()
	if path == "" {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return true, err
	}
	return true, os.WriteFile(path, []byte(token), 0o600)
}

func readRelayAuthTokenFileOverride() (string, bool, error) {
	path, ok, err := relayAuthTokenFilePathFromConfig()
	if err != nil || !ok {
		return "", ok, err
	}
	token, err := readRelayAuthTokenFile(path)
	return token, true, err
}

func writeRelayAuthTokenFileOverride(token string) (bool, error) {
	path, ok, err := relayAuthTokenFilePathFromConfig()
	if errors.Is(err, errRelayTokenStorageConfigUnreadable) {
		// Setup rewrites the config anyway; an unreadable config must not
		// block the repair path, only reads fail loud.
		return false, nil
	}
	if err != nil || !ok {
		return ok, err
	}
	return true, writeRelayAuthTokenFile(path, token)
}

func deleteRelayAuthTokenFileOverride() (bool, error) {
	path, ok, err := relayAuthTokenFilePathFromConfig()
	if errors.Is(err, errRelayTokenStorageConfigUnreadable) {
		return false, nil
	}
	if err != nil || !ok {
		return ok, err
	}
	return true, deleteRelayAuthTokenFile(path)
}

func relayAuthTokenStorageLabel() string {
	if path, ok, err := relayAuthTokenFilePathFromConfig(); err == nil && ok {
		return relayAuthTokenFileServiceName(path)
	}
	return "secure storage"
}

func deleteRelayAuthTokenOverride() (bool, error) {
	path := relayAuthTokenTestFile()
	if path == "" {
		return false, nil
	}
	if err := os.Remove(path); err != nil && !isNotExist(err) {
		return true, err
	}
	return true, nil
}
