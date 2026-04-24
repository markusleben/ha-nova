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
	return readSecretWithService(relayAuthTokenServiceName())
}

func writeRelayAuthToken(token string) error {
	if overridden, err := writeRelayAuthTokenOverride(token); overridden {
		if err != nil {
			return fmt.Errorf("cannot write relay auth token: %w", err)
		}
		return nil
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
