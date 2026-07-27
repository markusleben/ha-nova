//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const secureStorageNativePromptTimeout = 2 * time.Minute

var secureStorageRecoverySessionBusWithTimeout = relayAuthTokenPreflightSessionBusWithTimeout
var inspectLinuxSecureStorageStateForRecovery = inspectLinuxSecureStorageState
var inspectLinuxSecureStorageStateWithConnForRecovery = inspectLinuxSecureStorageStateWithConn
var createLinuxSecureStorageCollectionForRecovery = linuxOAuthSecretCreateCollection
var unlockLinuxSecureStorageCollectionForRecovery = linuxOAuthSecretUnlock
var secureStorageRecoveryCollectionLocked = secretServiceCollectionLocked
var secureStorageRecoveryInteractiveSession = nativeLinuxSecureStoragePromptAvailable
var secureStorageRecoveryPromptTimeout = secureStorageNativePromptTimeout

func platformNativeSecretPromptContextAvailable() bool {
	if !nativeSecretPromptBaseContextAvailable() {
		return false
	}
	sessionType := strings.ToLower(strings.TrimSpace(os.Getenv("XDG_SESSION_TYPE")))
	if sessionType != "wayland" && sessionType != "x11" {
		return false
	}
	if strings.TrimSpace(os.Getenv("DBUS_SESSION_BUS_ADDRESS")) == "" {
		return false
	}
	return strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != "" ||
		strings.TrimSpace(os.Getenv("DISPLAY")) != ""
}

func nativeLinuxSecureStoragePromptAvailable() bool {
	return nativeSecretPromptSessionAvailable(os.Stdout)
}

func platformNativePromptRunsUnderWSL() bool {
	if strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "" ||
		strings.TrimSpace(os.Getenv("WSL_INTEROP")) != "" {
		return true
	}
	release, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		// A supported desktop Linux session has a readable procfs. If the
		// runtime cannot prove that it is native Linux, do not open keyring UI.
		return true
	}
	value := strings.ToLower(string(release))
	return strings.Contains(value, "microsoft") || strings.Contains(value, "wsl")
}

func detectPlatformSecureStorageRecoverySupport() (bool, error) {
	conn, err := secureStorageRecoverySessionBusWithTimeout(relayAuthTokenPreflightTimeout)
	if err != nil {
		return false, err
	}
	if _, err := inspectLinuxSecureStorageStateWithConnForRecovery(conn); err != nil {
		return false, err
	}
	return true, nil
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

func runPlatformSecureStorageRecovery(action platformSecureStorageRecoveryAction) error {
	if !secureStorageRecoveryInteractiveSession() {
		return desktopKeyringSessionUnavailableError(
			"native secure-storage recovery requires an interactive Linux desktop session",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), secureStorageRecoveryPromptTimeout)
	defer cancel()

	conn, err := secureStorageRecoverySessionBusWithTimeout(relayAuthTokenPreflightTimeout)
	if err != nil {
		return localSecureStorageRecoveryError(err)
	}
	state, err := inspectLinuxSecureStorageStateWithConnForRecovery(conn)
	if err != nil {
		return localSecureStorageRecoveryError(err)
	}

	switch state.kind {
	case linuxSecureStorageStateNeedsInit:
		if action != platformSecureStorageRecoveryInitialize {
			return desktopKeyringInitializationRequiredError(
				"no default Secret Service collection is configured",
			)
		}
		collection, createErr := createLinuxSecureStorageCollectionForRecovery(ctx, conn)
		if createErr != nil {
			return classifyNativeSecureStorageRecoveryError(action, createErr)
		}
		locked, inspectErr := secureStorageRecoveryCollectionLocked(conn, collection)
		if inspectErr != nil {
			return localSecureStorageRecoveryError(inspectErr)
		}
		if locked {
			return errors.New(
				"the new Secret Service collection remained locked; rerun setup to unlock it",
			)
		}
	case linuxSecureStorageStateLocked:
		if err := unlockLinuxSecureStorageCollectionForRecovery(
			ctx,
			conn,
			state.defaultCollection,
			SecretStoreAllowUI,
		); err != nil {
			return classifyNativeSecureStorageRecoveryError(
				platformSecureStorageRecoveryUnlock,
				err,
			)
		}
	case linuxSecureStorageStateWritable:
		// A concurrent desktop unlock already completed the recovery.
	default:
		return fmt.Errorf("local secure storage returned an unsupported state")
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("native secure-storage prompt timed out: %w", err)
	}
	return nil
}

func classifyNativeSecureStorageRecoveryError(
	action platformSecureStorageRecoveryAction,
	err error,
) error {
	switch {
	case IsCloudErrorCode(err, CloudErrSecretPromptCanceled),
		IsCloudErrorCode(err, CloudErrOAuthCanceled):
		return fmt.Errorf("%w: native Secret Service prompt was dismissed", errLocalSecureStoragePromptCanceled)
	case IsCloudErrorCode(err, CloudErrSecretStoreLocked),
		IsCloudErrorCode(err, CloudErrSecretUIForbidden):
		if action == platformSecureStorageRecoveryInitialize {
			return desktopKeyringInitializationRequiredError(
				"native Secret Service prompt did not create secure storage",
			)
		}
		return desktopKeyringLockedError("native Secret Service prompt did not unlock secure storage")
	case IsCloudErrorCode(err, CloudErrTimeout):
		return fmt.Errorf("native secure-storage prompt timed out: %w", err)
	default:
		return localSecureStorageRecoveryError(err)
	}
}
