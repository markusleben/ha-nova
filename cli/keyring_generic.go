package main

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// Generic OS-keyring access by service name, used for the device-credential
// slots (current + pending). It is additive and does not touch the existing
// relay-auth-token functions. For hermetic tests, HA_NOVA_TEST_SECRET_DIR
// points at a directory where each service is one 0600 file — the same escape
// hatch shape the relay token uses, kept isolated per service.

var errSecretNotFound = errors.New("secret not found")

func secretUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "ha-nova"
}

func testSecretDir() (string, bool) {
	dir := strings.TrimSpace(os.Getenv("HA_NOVA_TEST_SECRET_DIR"))
	return dir, dir != ""
}

func testSecretPath(dir, service string) string {
	// Service names contain dots; keep them as-is but strip any path separators.
	safe := strings.ReplaceAll(strings.ReplaceAll(service, "/", "_"), string(os.PathSeparator), "_")
	return filepath.Join(dir, safe)
}

func secretGet(service string) (string, error) {
	if dir, ok := testSecretDir(); ok {
		data, err := os.ReadFile(testSecretPath(dir, service))
		if err != nil {
			if os.IsNotExist(err) {
				return "", errSecretNotFound
			}
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	value, err := keyring.Get(service, secretUser())
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", errSecretNotFound
		}
		return "", err
	}
	return value, nil
}

func secretSet(service, value string) error {
	if dir, ok := testSecretDir(); ok {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(testSecretPath(dir, service), []byte(value), 0o600)
	}
	return keyring.Set(service, secretUser(), value)
}

func secretDelete(service string) error {
	if dir, ok := testSecretDir(); ok {
		err := os.Remove(testSecretPath(dir, service))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	err := keyring.Delete(service, secretUser())
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}
