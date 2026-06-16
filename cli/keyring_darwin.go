//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"

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
	if token, overridden, err := readRelayAuthTokenFileOverride(); overridden {
		return token, err
	}
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}
	service := relayAuthTokenServiceName()

	// Read through go-keyring so the base64 envelope its writer (keyring.Set)
	// adds is decoded back to the raw token. Reading the item with raw
	// `security ... -w` returns the encoded `go-keyring-base64:...` value, which
	// would authenticate every relay call with the wrong bearer token.
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
	if overridden, err := writeRelayAuthTokenFileOverride(token); overridden {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}
	service := relayAuthTokenServiceName()

	// Write via go-keyring, whose macOS backend pipes the command through
	// `security -i` (stdin) instead of passing the secret as a CLI argument — so
	// the token never appears in the process argv (visible to `ps`). go-keyring
	// base64-wraps the stored value, so the read path MUST use keyring.Get
	// (above) to decode it; delete matches by `-s <service> -a <user>` and needs
	// no value, so it carries no argv exposure.
	if err := keyring.Set(service, u.Username, token); err != nil {
		return fmt.Errorf("cannot write relay auth token: %w", err)
	}
	return nil
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
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}
	service := relayAuthTokenServiceName()

	cmd := exec.Command(
		"security", "delete-generic-password",
		"-a", u.Username,
		"-s", service,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		text := strings.TrimSpace(string(output))
		if strings.Contains(text, "could not be found") {
			return nil
		}
		return fmt.Errorf("cannot delete relay auth token: %w (%s)", err, text)
	}
	return nil
}
