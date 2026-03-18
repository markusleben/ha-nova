//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"
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

	cmd := exec.Command(
		"security", "find-generic-password",
		"-a", u.Username,
		"-s", service,
		"-w",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		lower := strings.ToLower(text)
		if strings.Contains(lower, "could not be found") || strings.Contains(lower, "item could not be found") {
			return "", missingRelayAuthTokenError(service)
		}
		return "", relayAuthTokenReadError(service, fmt.Errorf("%w (%s)", err, text))
	}
	return strings.TrimSpace(string(out)), nil
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
	service := relayAuthTokenServiceName()

	cmd := exec.Command(
		"security", "add-generic-password",
		"-U",
		"-a", u.Username,
		"-s", service,
		"-w", token,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot write relay auth token: %w (%s)", err, strings.TrimSpace(string(output)))
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
