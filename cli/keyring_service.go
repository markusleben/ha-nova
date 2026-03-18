package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errRelayAuthTokenMissing = errors.New("relay auth token missing")

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
	return fmt.Sprintf("relay auth token unavailable: %s", err)
}

func isDesktopKeyringUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "org.freedesktop.secrets") &&
		(strings.Contains(message, "not provided") || strings.Contains(message, "serviceunknown"))
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
