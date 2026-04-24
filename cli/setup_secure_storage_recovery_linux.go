//go:build linux

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	dbusServiceName            = "org.freedesktop.DBus"
	dbusServicePath            = "/org/freedesktop/DBus"
	dbusServiceInterface       = "org.freedesktop.DBus"
	gnomeKeyringComm           = "gnome-keyring-daemon"
	secureStorageUnlockTimeout = 3 * time.Second
)

var secureStorageRecoveryOwnerProcess = detectSecretServiceOwnerProcessForRecovery
var secureStorageRecoveryProbe = probeLinuxKeyringWritable
var secureStorageRecoverySessionBusWithTimeout = relayAuthTokenPreflightSessionBusWithTimeout
var inspectLinuxSecureStorageStateForRecovery = inspectLinuxSecureStorageState
var inspectLinuxSecureStorageStateWithConnForRecovery = inspectLinuxSecureStorageStateWithConn
var initializeLinuxSecureStorageForRecovery = initializeLinuxSecureStorage
var unlockLinuxSecureStorageForRecovery = unlockLinuxSecureStorage

func detectPlatformSecureStorageRecoverySupport() (bool, error) {
	owner, err := secureStorageRecoveryOwnerProcess()
	if err != nil {
		return false, err
	}
	if !owner.supportsGNOMEKeyringRecovery() {
		return false, nil
	}
	conn, err := secureStorageRecoverySessionBusWithTimeout(relayAuthTokenPreflightTimeout)
	if err != nil {
		return false, err
	}
	return secureStorageRecoverySupportsGNOMEMethods(conn)
}

func inferPlatformSecureStorageRecoveryAction(err error) (platformSecureStorageRecoveryAction, error) {
	switch {
	case isDesktopKeyringInitializationRequiredError(err):
		return platformSecureStorageRecoveryInitialize, nil
	case isDesktopKeyringLockedError(err):
		return platformSecureStorageRecoveryUnlock, nil
	}

	state, inspectErr := inspectLinuxSecureStorageStateForRecovery()
	if inspectErr != nil {
		return "", inspectErr
	}
	switch state.kind {
	case linuxSecureStorageStateNeedsInit:
		return platformSecureStorageRecoveryInitialize, nil
	case linuxSecureStorageStateLocked:
		return platformSecureStorageRecoveryUnlock, nil
	default:
		return "", desktopKeyringSetupRequiredError("local secure storage does not need interactive recovery")
	}
}

func runPlatformSecureStorageRecovery(action platformSecureStorageRecoveryAction, secret []byte) error {
	if len(secret) == 0 {
		return localSecureStorageRecoveryError(desktopKeyringSetupRequiredError("local secure storage password missing"))
	}

	owner, err := secureStorageRecoveryOwnerProcess()
	if err != nil {
		return localSecureStorageRecoveryError(err)
	}
	if !owner.supportsGNOMEKeyringRecovery() {
		return localSecureStorageRecoveryError(desktopKeyringUnavailableError("GNOME Keyring recovery is unavailable"))
	}

	conn, err := secureStorageRecoverySessionBusWithTimeout(relayAuthTokenPreflightTimeout)
	if err != nil {
		return localSecureStorageRecoveryError(err)
	}

	switch action {
	case platformSecureStorageRecoveryInitialize:
		if err := initializeLinuxSecureStorageForRecovery(conn, secret); err != nil {
			return localSecureStorageRecoveryError(err)
		}
	default:
		state, err := inspectLinuxSecureStorageStateWithConnForRecovery(conn)
		if err != nil {
			return localSecureStorageRecoveryError(err)
		}
		if state.kind == linuxSecureStorageStateNeedsInit {
			return localSecureStorageRecoveryError(desktopKeyringInitializationRequiredError("no default Secret Service collection configured"))
		}
		if state.kind == linuxSecureStorageStateLocked {
			if err := unlockLinuxSecureStorageForRecovery(conn, state.defaultCollection, secret); err != nil {
				return localSecureStorageRecoveryError(err)
			}
		}
		if err := secureStorageRecoveryProbe(); err != nil {
			return localSecureStorageRecoveryError(err)
		}
		return nil
	}

	if err := secureStorageRecoveryProbe(); err != nil {
		return err
	}
	return nil
}

func probeLinuxKeyringWritable() error {
	serviceSuffix := make([]byte, 8)
	if _, err := rand.Read(serviceSuffix); err != nil {
		return fmt.Errorf("cannot verify local secure storage: %w", err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("cannot verify local secure storage: %w", err)
	}
	service := fmt.Sprintf("%s.recovery-probe.%s", relayAuthTokenServiceName(), hex.EncodeToString(serviceSuffix))
	probeSecret := hex.EncodeToString(secret)

	if err := writeSecretWithService(service, probeSecret); err != nil {
		return localSecureStorageRecoveryError(err)
	}
	readBack, err := readSecretWithService(service)
	if err != nil {
		deleteErr := deleteSecretWithService(service)
		if deleteErr != nil {
			return localSecureStorageRecoveryError(deleteErr)
		}
		return localSecureStorageRecoveryError(err)
	}
	if readBack != probeSecret {
		deleteErr := deleteSecretWithService(service)
		if deleteErr != nil {
			return localSecureStorageRecoveryError(deleteErr)
		}
		return fmt.Errorf("cannot verify local secure storage: saved verification secret did not match")
	}
	if err := deleteSecretWithService(service); err != nil {
		return localSecureStorageRecoveryError(err)
	}
	return nil
}
