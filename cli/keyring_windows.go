//go:build windows

package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
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
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}
	service := relayAuthTokenServiceName()

	token, err := keyring.Get(service, u.Username)
	if err == nil {
		return token, nil
	}
	legacy, legacyErr := readLegacyWindowsRelayAuthToken(service)
	if legacyErr == nil && strings.TrimSpace(legacy) != "" {
		trimmed := strings.TrimSpace(legacy)
		if setErr := keyring.Set(service, u.Username, trimmed); setErr != nil {
			printHumanWarn("legacy Windows relay token migration failed: %s", setErr)
			return trimmed, nil
		}
		if deleteErr := deleteLegacyWindowsRelayAuthToken(service); deleteErr != nil {
			printHumanWarn("legacy Windows relay token cleanup failed after migration: %s", deleteErr)
		}
		return trimmed, nil
	}
	if err != keyring.ErrNotFound {
		return "", relayAuthTokenReadError(service, err)
	}
	if legacyErr != nil && !isNotExist(legacyErr) {
		return "", relayAuthTokenReadError(service, legacyErr)
	}
	return "", missingRelayAuthTokenError(service)
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
	if err := keyring.Set(relayAuthTokenServiceName(), u.Username, token); err != nil {
		return err
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
	if err := keyring.Delete(relayAuthTokenServiceName(), u.Username); err != nil && err != keyring.ErrNotFound {
		return err
	}
	if err := deleteLegacyWindowsRelayAuthToken(relayAuthTokenServiceName()); err != nil {
		printHumanWarn("legacy Windows relay token mirror cleanup failed: %s", err)
	}
	return nil
}

func legacyWindowsRelayAuthTokenPath(service string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_' || r == '-':
			return r
		default:
			return '_'
		}
	}, service)
	return filepath.Join(home, ".config", "ha-nova", "."+safe+".dpapi"), nil
}

func readLegacyWindowsRelayAuthToken(service string) (string, error) {
	path, err := legacyWindowsRelayAuthTokenPath(service)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	psPath := strings.ReplaceAll(path, `'`, `''`)
	command := "$blob = Get-Content -LiteralPath '" + psPath + `' -Raw; if ([string]::IsNullOrWhiteSpace($blob)) { exit 1 }; $secure = ConvertTo-SecureString -String $blob; $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure); try { [Console]::Out.Write([Runtime.InteropServices.Marshal]::PtrToStringAuto($bstr)) } finally { if ($bstr -ne [IntPtr]::Zero) { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr) } }`
	out, err := buildWindowsHiddenPowerShellCommand(command).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func deleteLegacyWindowsRelayAuthToken(service string) error {
	path, err := legacyWindowsRelayAuthTokenPath(service)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !isNotExist(err) {
		return err
	}
	return nil
}
