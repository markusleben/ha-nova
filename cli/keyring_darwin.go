//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"
)

func readRelayAuthToken() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}

	out, err := exec.Command(
		"security", "find-generic-password",
		"-a", u.Username,
		"-s", keyringServiceName,
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("missing relay auth token (%s)", keyringServiceName)
	}
	return strings.TrimSpace(string(out)), nil
}

func writeRelayAuthToken(token string) error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}

	cmd := exec.Command(
		"security", "add-generic-password",
		"-U",
		"-a", u.Username,
		"-s", keyringServiceName,
		"-w", token,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot write relay auth token: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func deleteRelayAuthToken() error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("cannot determine current user: %w", err)
	}

	cmd := exec.Command(
		"security", "delete-generic-password",
		"-a", u.Username,
		"-s", keyringServiceName,
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
