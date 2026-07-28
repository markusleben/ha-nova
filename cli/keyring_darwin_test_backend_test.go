//go:build darwin

package main

import (
	"context"
	"errors"

	"github.com/zalando/go-keyring"
)

// Darwin production code uses Security.framework directly. Package tests use
// go-keyring's in-memory mock, so keep ordinary storage tests isolated from the
// user's real login Keychain. Policy tests call the native core through seams.
func init() {
	// Install the mock during package initialization, before TestMain and before
	// any helper subprocess can reach a package-level test hook. Waiting for
	// TestMain leaves an initialization window where go-keyring still owns its
	// production /usr/bin/security provider.
	keyring.MockInit()
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	readDarwinSecretInProcess = func(
		ctx context.Context,
		service, account string,
		_ SecretStoreUIPolicy,
		_ string,
	) (string, bool, error) {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		value, err := keyring.Get(service, account)
		if errors.Is(err, keyring.ErrNotFound) {
			return "", false, nil
		}
		return value, err == nil, err
	}
	setDarwinSecretInProcess = func(
		ctx context.Context,
		service, account, value string,
		_ SecretStoreUIPolicy,
		_ string,
	) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return keyring.Set(service, account, value)
	}
	deleteDarwinSecretInProcess = func(
		ctx context.Context,
		service, account string,
		_ SecretStoreUIPolicy,
		_ string,
	) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := keyring.Delete(service, account)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
}
