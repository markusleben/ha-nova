//go:build linux

package main

import (
	"fmt"
	"os/user"

	"github.com/zalando/go-keyring"
)

var keyringGetWithService = keyring.Get
var keyringSetWithService = keyring.Set
var keyringDeleteWithService = keyring.Delete
var inspectLinuxSecureStorageStateForKeyring = inspectLinuxSecureStorageState

// Device-credential slots share the relay token's Secret Service preflight, so a
// locked/uninitialized backend fails fast with a classified error instead of
// hanging go-keyring in an unlock prompt.
func init() {
	deviceCredentialPreflight = relayAuthTokenLinuxReadPreflight
}

func readRelayAuthToken() (string, error) {
	if token, overridden, err := readRelayAuthTokenOverride(); overridden {
		if err != nil {
			if isNotExist(err) {
				return "", missingRelayAuthTokenError(relayAuthTokenServiceName())
			}
			return "", relayAuthTokenReadError(relayAuthTokenServiceName(), err)
		}
		return token, nil
	}
	if token, overridden, err := readRelayAuthTokenFileOverride(); overridden {
		return token, err
	}
	// Fail fast with the local credential-store class instead of letting
	// go-keyring hang in a Secret Service unlock prompt (issue #200). The
	// recovery probe uses the low-level wrappers directly and stays exempt.
	if err := relayAuthTokenLinuxReadPreflight(); err != nil {
		return "", relayAuthTokenReadError(relayAuthTokenServiceName(), err)
	}
	return readSecretWithService(relayAuthTokenServiceName())
}

func writeRelayAuthToken(token string) error {
	if overridden, err := writeRelayAuthTokenOverride(token); overridden {
		if err != nil {
			return fmt.Errorf("cannot write relay auth token: %w", err)
		}
		return nil
	}
	if overridden, err := writeRelayAuthTokenFileOverride(token); overridden {
		return err
	}
	return writeSecretWithService(relayAuthTokenServiceName(), token)
}

func deleteRelayAuthToken() error {
	if overridden, err := deleteRelayAuthTokenOverride(); overridden {
		if err != nil {
			return fmt.Errorf("cannot delete relay auth token: %w", err)
		}
		return nil
	}
	if overridden, err := deleteRelayAuthTokenFileOverride(); overridden {
		return err
	}
	if err := relayAuthTokenLinuxReadPreflight(); err != nil {
		return err
	}
	return deleteSecretWithService(relayAuthTokenServiceName())
}

func currentKeyringUsername() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}
	return u.Username, nil
}

func readSecretWithService(service string) (string, error) {
	username, err := currentKeyringUsername()
	if err != nil {
		return "", err
	}

	token, err := keyringGetWithService(service, username)
	if err != nil {
		if err == keyring.ErrNotFound {
			return "", missingRelayAuthTokenError(service)
		}
		return "", relayAuthTokenReadError(service, normalizeLinuxKeyringError(err))
	}
	return token, nil
}

func writeSecretWithService(service, token string) error {
	username, err := currentKeyringUsername()
	if err != nil {
		return err
	}
	return normalizeLinuxKeyringError(keyringSetWithService(service, username, token))
}

func deleteSecretWithService(service string) error {
	username, err := currentKeyringUsername()
	if err != nil {
		return err
	}
	if err := keyringDeleteWithService(service, username); err != nil && err != keyring.ErrNotFound {
		return normalizeLinuxKeyringError(err)
	}
	return nil
}

func relayAuthTokenLinuxReadPreflight() error {
	state, err := inspectLinuxSecureStorageStateForKeyring()
	if err != nil {
		return err
	}
	switch state.kind {
	case linuxSecureStorageStateNeedsInit:
		return desktopKeyringInitializationRequiredError("no default Secret Service collection configured")
	case linuxSecureStorageStateLocked:
		return desktopKeyringLockedError("default Secret Service collection is locked")
	default:
		return nil
	}
}
