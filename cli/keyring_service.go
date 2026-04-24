package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errRelayAuthTokenMissing = errors.New("relay auth token missing")
var errDesktopKeyringSessionUnavailable = errors.New("desktop keyring session unavailable")
var errDesktopKeyringUnavailable = errors.New("desktop keyring unavailable")
var errDesktopKeyringSetupRequired = errors.New("desktop keyring setup required")
var errDesktopKeyringLocked = errors.New("desktop keyring locked")
var errDesktopKeyringInitializationRequired = errors.New("desktop keyring initialization required")

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
		return "secure storage is present but locked on this Linux machine; unlock the default keyring and then run: ha-nova setup"
	}
	if isDesktopKeyringSetupRequiredError(err) {
		return "secure storage is present but not ready on this Linux machine; rerun `ha-nova setup` interactively to finish local secure storage setup"
	}
	return fmt.Sprintf("relay auth token unavailable: %s", err)
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
