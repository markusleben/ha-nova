//go:build linux

package main

import (
	"fmt"
	"os/user"

	"github.com/zalando/go-keyring"
)

func readRelayAuthToken() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}

	token, err := keyring.Get(keyringServiceName, u.Username)
	if err != nil {
		return "", fmt.Errorf("missing relay auth token (%s)", keyringServiceName)
	}
	return token, nil
}

func writeRelayAuthToken(token string) error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}
	return keyring.Set(keyringServiceName, u.Username, token)
}

func deleteRelayAuthToken() error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}
	if err := keyring.Delete(keyringServiceName, u.Username); err != nil && err != keyring.ErrNotFound {
		return err
	}
	return nil
}
