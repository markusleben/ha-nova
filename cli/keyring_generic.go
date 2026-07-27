package main

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// Generic OS-keyring access by service name, used for the device-credential
// slots (current + pending). It is additive and does not touch the existing
// relay-auth-token functions. Tests may inject a private file backend through
// testSecretDirForRuntime. Release binaries never consult an environment
// variable for secret-store selection.

var errSecretNotFound = errors.New("secret not found")

// These indirections keep platform policy at one boundary. Linux replaces
// them with the bounded native Secret Service backend so a relock can never
// make an ordinary credential operation open provider UI.
var secretKeyringGet = keyring.Get
var secretKeyringSet = keyring.Set
var secretKeyringDelete = keyring.Delete
var secretKeyringGetWithPolicy = defaultSecretKeyringGetWithPolicy
var secretKeyringSetWithPolicy = defaultSecretKeyringSetWithPolicy
var secretKeyringDeleteWithPolicy = defaultSecretKeyringDeleteWithPolicy

// deviceCredentialPreflight guards the OS keyring backend before a device-slot
// read/write. It is a no-op by default; macOS and Linux replace the contextual
// form to enforce explicit prompt policy and fail fast for no-UI operations.
var deviceCredentialPreflight = func() error { return nil }
var deviceCredentialPromptSessionAvailable = platformNativeSecretPromptAvailable
var deviceCredentialPreflightWithContext = func(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) error {
	if err := validateDeviceCredentialPreflightRequest(ctx, ui); err != nil {
		return err
	}
	return deviceCredentialPreflight()
}

func validateDeviceCredentialPreflightRequest(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func deviceCredentialPromptUnavailableError() error {
	return newCloudError(
		CloudErrSecretUIForbidden,
		"open device secure-storage prompt",
		nil,
	)
}

func secretUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "ha-nova"
}

func productionTestSecretDir() (string, bool) { return "", false }

var testSecretDirForRuntime = productionTestSecretDir

func testSecretDir() (string, bool) { return testSecretDirForRuntime() }

func testSecretPath(dir, service string) string {
	// Service names contain dots; keep them as-is but strip any path separators.
	safe := strings.ReplaceAll(strings.ReplaceAll(service, "/", "_"), string(os.PathSeparator), "_")
	return filepath.Join(dir, safe)
}

func defaultSecretKeyringGetWithPolicy(
	ctx context.Context,
	service, account string,
	_ SecretStoreUIPolicy,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := secretKeyringGet(service, account)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return value, nil
}

func defaultSecretKeyringSetWithPolicy(
	ctx context.Context,
	service, account, value string,
	_ SecretStoreUIPolicy,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := secretKeyringSet(service, account, value); err != nil {
		return err
	}
	return ctx.Err()
}

func defaultSecretKeyringDeleteWithPolicy(
	ctx context.Context,
	service, account string,
	_ SecretStoreUIPolicy,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := secretKeyringDelete(service, account); err != nil {
		return err
	}
	return ctx.Err()
}

func secretGet(service string) (string, error) {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return secretGetWithPolicy(ctx, service, SecretStoreForbidUI)
}

func secretGetWithPolicy(
	ctx context.Context,
	service string,
	ui SecretStoreUIPolicy,
) (string, error) {
	if err := validateSecretUIPolicy(ui); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if dir, ok := testSecretDir(); ok {
		data, err := os.ReadFile(testSecretPath(dir, service))
		if err != nil {
			if os.IsNotExist(err) {
				return "", errSecretNotFound
			}
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	// Headless installs keep device credentials in private files (see
	// device_credential_storage.go). This is one install-wide backend decision
	// (an explicit marker or the process-forced flag), so a leftover credential
	// file can never redirect a slot on a keyring install, and no dbus probing
	// happens on the hot read path.
	if deviceSecretFileBacked() {
		value, err := deviceSecretFileGet(service)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(value), nil
	}
	if err := deviceCredentialPreflightWithContext(ctx, ui); err != nil {
		return "", err
	}
	value, err := secretKeyringGetWithPolicy(
		ctx,
		service,
		secretUser(),
		ui,
	)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", errSecretNotFound
		}
		return "", err
	}
	return value, nil
}

func secretSet(service, value string) error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return secretSetWithPolicy(ctx, service, value, SecretStoreForbidUI)
}

func secretSetWithPolicy(
	ctx context.Context,
	service, value string,
	ui SecretStoreUIPolicy,
) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if dir, ok := testSecretDir(); ok {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(testSecretPath(dir, service), []byte(value), 0o600)
	}
	if deviceSecretFileBacked() {
		return deviceSecretFileSet(service, value)
	}
	if err := deviceCredentialPreflightWithContext(ctx, ui); err != nil {
		return err
	}
	return secretKeyringSetWithPolicy(
		ctx,
		service,
		secretUser(),
		value,
		ui,
	)
}

func secretDelete(service string) error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return secretDeleteWithPolicy(ctx, service, SecretStoreForbidUI)
}

func secretDeleteWithPolicy(
	ctx context.Context,
	service string,
	ui SecretStoreUIPolicy,
) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if dir, ok := testSecretDir(); ok {
		err := os.Remove(testSecretPath(dir, service))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if deviceSecretFileBacked() {
		return deviceSecretFileDelete(service)
	}
	if err := deviceCredentialPreflightWithContext(ctx, ui); err != nil {
		return err
	}
	err := secretKeyringDeleteWithPolicy(
		ctx,
		service,
		secretUser(),
		ui,
	)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}
