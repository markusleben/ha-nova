//go:build darwin

package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os/user"
	"strings"

	"github.com/zalando/go-keyring"
)

func init() {
	secretKeyringGetWithPolicy = darwinDeviceSecretGet
	secretKeyringSetWithPolicy = darwinDeviceSecretSet
	secretKeyringDeleteWithPolicy = darwinDeviceSecretDelete
	deviceCredentialPreflightWithContext = darwinDeviceCredentialPreflight
	explicitPairingSecretStoreUIPolicy = func() SecretStoreUIPolicy {
		return SecretStoreAllowUI
	}
	snapshotRelayAuthTokenForSetupPersistence =
		snapshotDarwinRelayAuthTokenForSetupPersistence
}

var darwinDeviceSecureStorageBoundaryAvailable = platformCloudRemoteSecureStorageBoundaryAvailable
var readDarwinSecretInProcess = readDarwinKeychainSecretInProcess
var setDarwinSecretInProcess = setDarwinKeychainSecretInProcess
var deleteDarwinSecretInProcess = deleteDarwinKeychainSecretInProcess
var readDarwinSecretThroughBoundary = readDarwinKeychainSecret
var runNativeSecretWorkerForDarwinDevice = runNativeSecretWorkerProcess

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
	}
	return nil
}

func darwinDeviceSecretGet(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) (string, error) {
	if !darwinDeviceSecureStorageBoundaryAvailable() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		value, found, err := readDarwinSecretInProcess(
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
	value, found, err := readDarwinSecretThroughBoundary(
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
	if ui == SecretStoreAllowUI {
		if err := darwinDeviceSecretDelete(
			ctx,
			service,
			account,
			ui,
		); err != nil {
			return fmt.Errorf(
				"replace macOS Keychain device credential: %w",
				err,
			)
		}
	}
	if !darwinDeviceSecureStorageBoundaryAvailable() {
		if err := ctx.Err(); err != nil {
			return err
		}
		expected := []byte(value)
		defer zeroSecretBytes(expected)
		err := setDarwinSecretInProcess(
			ctx,
			service,
			account,
			value,
			ui,
			"write device credential",
		)
		return reconcileDarwinInProcessSet(
			ctx,
			service,
			account,
			expected,
			err,
		)
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
	_, err := runNativeSecretWorkerForDarwinDevice(operationCtx, request)
	return reconcileNativeSecretSet(ctx, request, err)
}

func darwinDeviceSecretDelete(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) error {
	if !darwinDeviceSecureStorageBoundaryAvailable() {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := deleteDarwinSecretInProcess(
			ctx,
			service,
			account,
			ui,
			"delete device credential",
		)
		return reconcileDarwinInProcessDelete(
			ctx,
			service,
			account,
			err,
		)
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
	_, err := runNativeSecretWorkerForDarwinDevice(operationCtx, request)
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
		defer zeroSecretBytes(decoded)
		if err != nil {
			return "", fmt.Errorf("decode macOS Keychain device credential: %w", err)
		}
		return string(decoded), nil
	case strings.HasPrefix(value, base64Prefix):
		decoded, err := base64.StdEncoding.DecodeString(
			strings.TrimPrefix(value, base64Prefix),
		)
		defer zeroSecretBytes(decoded)
		if err != nil {
			return "", fmt.Errorf("decode macOS Keychain device credential: %w", err)
		}
		return string(decoded), nil
	default:
		return value, nil
	}
}

func readRelayAuthToken() (string, error) {
	return readRelayAuthTokenWithPolicy(SecretStoreForbidUI)
}

func readRelayAuthTokenWithPolicy(
	ui SecretStoreUIPolicy,
) (string, error) {
	if err := validateSecretUIPolicy(ui); err != nil {
		return "", err
	}
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

	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		ui,
	)
	defer cancel()
	var token string
	var found bool
	if darwinDeviceSecureStorageBoundaryAvailable() {
		token, found, err = readDarwinSecretThroughBoundary(
			ctx,
			service,
			u.Username,
			ui,
			"read relay auth token",
		)
	} else {
		token, found, err = readDarwinSecretInProcess(
			ctx,
			service,
			u.Username,
			ui,
			"read relay auth token",
		)
	}
	if err != nil {
		return "", relayAuthTokenReadError(service, err)
	}
	if !found {
		return "", missingRelayAuthTokenError(service)
	}
	token, err = decodeDarwinGoKeyringValue(token)
	if err != nil {
		return "", relayAuthTokenReadError(service, err)
	}
	return strings.TrimSpace(token), nil
}

func writeRelayAuthToken(token string) error {
	return writeRelayAuthTokenWithPolicy(token, SecretStoreForbidUI)
}

func writeRelayAuthTokenWithPolicy(
	token string,
	ui SecretStoreUIPolicy,
) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
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

	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		ui,
	)
	defer cancel()
	var writeErr error
	if ui == SecretStoreAllowUI {
		writeErr = replaceDarwinRelayAuthToken(
			ctx,
			service,
			u.Username,
			token,
		)
	} else {
		writeErr = setDarwinRelayAuthToken(
			ctx,
			service,
			u.Username,
			token,
			ui,
		)
	}
	if writeErr != nil {
		return fmt.Errorf("cannot write relay auth token: %w", writeErr)
	}
	return nil
}

func deleteRelayAuthToken() error {
	return deleteRelayAuthTokenWithPolicy(SecretStoreForbidUI)
}

func deleteRelayAuthTokenWithPolicy(ui SecretStoreUIPolicy) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
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

	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		ui,
	)
	defer cancel()
	deleteErr := deleteDarwinRelayAuthToken(
		ctx,
		service,
		u.Username,
		ui,
	)
	if deleteErr != nil {
		return fmt.Errorf("cannot delete relay auth token: %w", deleteErr)
	}
	return nil
}
