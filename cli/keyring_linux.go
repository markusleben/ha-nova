//go:build linux

package main

import (
	"fmt"
	"os/user"

	"github.com/zalando/go-keyring"
)

func readKeychainToken() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}

	token, err := keyring.Get("ha-nova.relay-auth-token", u.Username)
	if err != nil {
		return "", fmt.Errorf("missing relay auth token (ha-nova.relay-auth-token)")
	}
	return token, nil
}
