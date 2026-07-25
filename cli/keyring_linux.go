//go:build linux

package main

import (
	"context"
	"fmt"
	"os/user"

	"github.com/zalando/go-keyring"
)

var keyringGetWithService = nativeLinuxKeyringGet
var keyringSetWithService = nativeLinuxKeyringSet
var keyringDeleteWithService = nativeLinuxKeyringDelete
var newNativeLinuxCredentialBackend = newNativeCredentialSecretBackend
var inspectLinuxSecureStorageStateForKeyring = inspectLinuxSecureStorageState

// Device-credential slots share the relay token's Secret Service preflight, so a
// locked/uninitialized backend fails fast with a classified error instead of
// hanging go-keyring in an unlock prompt.
func init() {
	deviceCredentialPreflight = relayAuthTokenLinuxReadPreflight
	deviceCredentialPreflightWithContext = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return relayAuthTokenLinuxReadPreflight()
	}
	secretKeyringGet = nativeLinuxKeyringGet
	secretKeyringSet = nativeLinuxKeyringSet
	secretKeyringDelete = nativeLinuxKeyringDelete
	secretKeyringGetWithPolicy = linuxDeviceSecretGet
	secretKeyringSetWithPolicy = linuxDeviceSecretSet
	secretKeyringDeleteWithPolicy = linuxDeviceSecretDelete
}

func linuxDeviceSecretGet(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) (string, error) {
	backend, err := newNativeLinuxCredentialBackend()
	if err != nil {
		return "", normalizeLinuxKeyringProbeError(err)
	}
	value, exists, err := backend.Get(ctx, service, account, ui)
	if err != nil {
		return "", normalizeLinuxKeyringProbeError(err)
	}
	if !exists {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func linuxDeviceSecretSet(
	ctx context.Context,
	service, account, value string,
	ui SecretStoreUIPolicy,
) error {
	backend, err := newNativeLinuxCredentialBackend()
	if err != nil {
		return normalizeLinuxKeyringProbeError(err)
	}
	return normalizeLinuxKeyringProbeError(
		backend.Set(ctx, service, account, value, ui),
	)
}

func linuxDeviceSecretDelete(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) error {
	backend, err := newNativeLinuxCredentialBackend()
	if err != nil {
		return normalizeLinuxKeyringProbeError(err)
	}
	return normalizeLinuxKeyringProbeError(
		backend.Delete(ctx, service, account, ui),
	)
}

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
	// Fail fast with the local credential-store class instead of letting
	// go-keyring hang in a Secret Service unlock prompt (issue #200). The
	// recovery probe uses the low-level wrappers directly and stays exempt.
	if err := relayAuthTokenLinuxReadPreflight(); err != nil {
		return "", relayAuthTokenReadError(relayAuthTokenServiceName(), err)
	}
	return readSecretWithService(relayAuthTokenServiceName())
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
	return writeSecretWithService(relayAuthTokenServiceName(), token)
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
	if err := relayAuthTokenLinuxReadPreflight(); err != nil {
		return err
	}
	return deleteSecretWithService(relayAuthTokenServiceName())
}

func currentKeyringUsername() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}
	return u.Username, nil
}

func nativeLinuxKeyringGet(service, username string) (string, error) {
	backend, err := newNativeLinuxCredentialBackend()
	if err != nil {
		return "", normalizeLinuxKeyringProbeError(err)
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		relayAuthTokenPreflightTimeout,
	)
	defer cancel()
	value, exists, err := backend.Get(
		ctx,
		service,
		username,
		SecretStoreForbidUI,
	)
	if err != nil {
		return "", normalizeLinuxKeyringProbeError(err)
	}
	if !exists {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func nativeLinuxKeyringSet(service, username, value string) error {
	backend, err := newNativeLinuxCredentialBackend()
	if err != nil {
		return normalizeLinuxKeyringProbeError(err)
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		relayAuthTokenPreflightTimeout,
	)
	defer cancel()
	return normalizeLinuxKeyringProbeError(
		backend.Set(
			ctx,
			service,
			username,
			value,
			SecretStoreForbidUI,
		),
	)
}

func nativeLinuxKeyringDelete(service, username string) error {
	backend, err := newNativeLinuxCredentialBackend()
	if err != nil {
		return normalizeLinuxKeyringProbeError(err)
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		relayAuthTokenPreflightTimeout,
	)
	defer cancel()
	return normalizeLinuxKeyringProbeError(
		backend.Delete(
			ctx,
			service,
			username,
			SecretStoreForbidUI,
		),
	)
}

func readSecretWithService(service string) (string, error) {
	username, err := currentKeyringUsername()
	if err != nil {
		return "", err
	}

	token, err := keyringGetWithService(service, username)
	if err != nil {
		if err == keyring.ErrNotFound {
			return "", missingRelayAuthTokenError(service)
		}
		return "", relayAuthTokenReadError(service, normalizeLinuxKeyringError(err))
	}
	return token, nil
}

func writeSecretWithService(service, token string) error {
	username, err := currentKeyringUsername()
	if err != nil {
		return err
	}
	return normalizeLinuxKeyringError(keyringSetWithService(service, username, token))
}

func deleteSecretWithService(service string) error {
	username, err := currentKeyringUsername()
	if err != nil {
		return err
	}
	if err := keyringDeleteWithService(service, username); err != nil && err != keyring.ErrNotFound {
		return normalizeLinuxKeyringError(err)
	}
	return nil
}

func relayAuthTokenLinuxReadPreflight() error {
	state, err := inspectLinuxSecureStorageStateForKeyring()
	if err != nil {
		return err
	}
	switch state.kind {
	case linuxSecureStorageStateNeedsInit:
		return desktopKeyringInitializationRequiredError("no default Secret Service collection configured")
	case linuxSecureStorageStateLocked:
		return desktopKeyringLockedError("default Secret Service collection is locked")
	default:
		return nil
	}
}
