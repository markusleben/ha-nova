//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"
)

func readKeychainToken() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}

	out, err := exec.Command(
		"security", "find-generic-password",
		"-a", u.Username,
		"-s", "ha-nova.relay-auth-token",
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("missing relay auth token (ha-nova.relay-auth-token)")
	}
	return strings.TrimSpace(string(out)), nil
}
