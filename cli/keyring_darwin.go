//go:build darwin

package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

func init() {
	secretKeyringGetWithPolicy = darwinDeviceSecretGet
	secretKeyringSetWithPolicy = darwinDeviceSecretSet
	secretKeyringDeleteWithPolicy = darwinDeviceSecretDelete
	deviceCredentialPreflightWithContext = darwinDeviceCredentialPreflight
}

var macOSDeviceCredentialKeychainAvailableNoUI = macOSKeychainAvailableNoUI

func darwinDeviceCredentialPreflight(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) error {
	if err := validateDeviceCredentialPreflightRequest(ctx, ui); err != nil {
		return err
	}
	if ui == SecretStoreAllowUI {
		if !deviceCredentialPromptSessionAvailable() {
			return deviceCredentialPromptUnavailableError()
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := macOSDeviceCredentialKeychainAvailableNoUI(ctx); err != nil {
		return desktopKeyringLockedError(
			"the login keychain is locked or unavailable",
		)
	}
	return nil
}

func darwinDeviceSecretGet(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) (string, error) {
	if !platformCloudRemoteSecureStorageBoundaryAvailable() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return keyring.Get(service, account)
	}
	value, found, err := readDarwinKeychainSecret(
		ctx,
		service,
		account,
		ui,
		"read device credential",
	)
	if err != nil {
		return "", err
	}
	if !found {
		return "", keyring.ErrNotFound
	}
	return decodeDarwinGoKeyringValue(value)
}

func darwinDeviceSecretSet(
	ctx context.Context,
	service, account, value string,
	ui SecretStoreUIPolicy,
) error {
	if !platformCloudRemoteSecureStorageBoundaryAvailable() {
		if err := ctx.Err(); err != nil {
			return err
		}
		return keyring.Set(service, account, value)
	}
	raw := []byte(value)
	defer zeroSecretBytes(raw)
	operationCtx, cancel := boundedNativeOAuthSecretContext(ctx, ui)
	defer cancel()
	request := nativeSecretWorkerRequest{
		SchemaVersion: nativeSecretWorkerSchema,
		Operation:     nativeSecretSet,
		UI:            ui,
		Service:       service,
		Account:       account,
		Value:         raw,
	}
	_, err := runNativeSecretWorkerProcess(operationCtx, request)
	return reconcileNativeSecretSet(ctx, request, err)
}

func darwinDeviceSecretDelete(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) error {
	if !platformCloudRemoteSecureStorageBoundaryAvailable() {
		if err := ctx.Err(); err != nil {
			return err
		}
		return keyring.Delete(service, account)
	}
	operationCtx, cancel := boundedNativeOAuthSecretContext(ctx, ui)
	defer cancel()
	request := nativeSecretWorkerRequest{
		SchemaVersion: nativeSecretWorkerSchema,
		Operation:     nativeSecretDelete,
		UI:            ui,
		Service:       service,
		Account:       account,
	}
	_, err := runNativeSecretWorkerProcess(operationCtx, request)
	return reconcileNativeSecretDelete(ctx, request, err)
}

func decodeDarwinGoKeyringValue(value string) (string, error) {
	const (
		hexPrefix    = "go-keyring-encoded:"
		base64Prefix = "go-keyring-base64:"
	)
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(value, hexPrefix):
		decoded, err := hex.DecodeString(strings.TrimPrefix(value, hexPrefix))
		if err != nil {
			return "", fmt.Errorf("decode macOS Keychain device credential: %w", err)
		}
		return string(decoded), nil
	case strings.HasPrefix(value, base64Prefix):
		decoded, err := base64.StdEncoding.DecodeString(
			strings.TrimPrefix(value, base64Prefix),
		)
		if err != nil {
			return "", fmt.Errorf("decode macOS Keychain device credential: %w", err)
		}
		return string(decoded), nil
	default:
		return value, nil
	}
}

func macOSKeychainAvailableNoUI(ctx context.Context) error {
	return exec.CommandContext(
		ctx,
		"/usr/bin/security",
		"show-keychain-info",
	).Run()
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
