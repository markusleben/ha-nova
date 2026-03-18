//go:build linux

package main

import (
	"fmt"
	"os/user"

	"github.com/zalando/go-keyring"
)

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
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}
	service := relayAuthTokenServiceName()

	token, err := keyring.Get(service, u.Username)
	if err != nil {
		if err == keyring.ErrNotFound {
			return "", missingRelayAuthTokenError(service)
		}
		return "", relayAuthTokenReadError(service, err)
	}
	return token, nil
}

func writeRelayAuthToken(token string) error {
	if overridden, err := writeRelayAuthTokenOverride(token); overridden {
		if err != nil {
			return fmt.Errorf("cannot write relay auth token: %w", err)
		}
		return nil
	}
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}
	return keyring.Set(relayAuthTokenServiceName(), u.Username, token)
}

func deleteRelayAuthToken() error {
	if overridden, err := deleteRelayAuthTokenOverride(); overridden {
		if err != nil {
			return fmt.Errorf("cannot delete relay auth token: %w", err)
		}
		return nil
	}
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}
	if err := keyring.Delete(relayAuthTokenServiceName(), u.Username); err != nil && err != keyring.ErrNotFound {
		return err
	}
	return nil
}
