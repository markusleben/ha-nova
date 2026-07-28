//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
)

func replaceDarwinRelayAuthToken(
	ctx context.Context,
	service, account, token string,
) error {
	var previous string
	var found bool
	var err error
	if darwinDeviceSecureStorageBoundaryAvailable() {
		previous, found, err = readDarwinSecretThroughBoundary(
			ctx,
			service,
			account,
			SecretStoreAllowUI,
			"read relay auth token before replacement",
		)
	} else {
		previous, found, err = readDarwinSecretInProcess(
			ctx,
			service,
			account,
			SecretStoreAllowUI,
			"read relay auth token before replacement",
		)
	}
	if err != nil {
		return err
	}
	if found {
		if err := deleteDarwinRelayAuthToken(
			ctx,
			service,
			account,
			SecretStoreAllowUI,
		); err != nil {
			return err
		}
	}
	if err := setDarwinRelayAuthToken(
		ctx,
		service,
		account,
		token,
		SecretStoreAllowUI,
	); err != nil {
		if found {
			restoreCtx, cancelRestore :=
				boundedNativeOAuthSecretContext(
					context.Background(),
					SecretStoreAllowUI,
				)
			defer cancelRestore()
			restoreErr := setDarwinRelayAuthToken(
				restoreCtx,
				service,
				account,
				previous,
				SecretStoreAllowUI,
			)
			if restoreErr != nil {
				return errors.Join(
					err,
					fmt.Errorf(
						"restore previous relay auth token: %w",
						restoreErr,
					),
				)
			}
		}
		return err
	}
	return nil
}

func snapshotDarwinRelayAuthTokenForSetupPersistence(
	previousToken string,
	hadPreviousToken bool,
) (string, bool, error) {
	if hadPreviousToken {
		return previousToken, true, nil
	}
	token, err := readRelayAuthTokenWithPolicy(SecretStoreAllowUI)
	if err == nil {
		return token, true, nil
	}
	if isMissingRelayAuthTokenError(err) {
		return "", false, nil
	}
	return previousToken, hadPreviousToken, err
}

func setDarwinRelayAuthToken(
	ctx context.Context,
	service, account, token string,
	ui SecretStoreUIPolicy,
) error {
	if darwinDeviceSecureStorageBoundaryAvailable() {
		raw := []byte(token)
		defer zeroSecretBytes(raw)
		request := nativeSecretWorkerRequest{
			SchemaVersion: nativeSecretWorkerSchema,
			Operation:     nativeSecretSet,
			UI:            ui,
			Service:       service,
			Account:       account,
			Value:         raw,
		}
		_, err := runNativeSecretWorkerForDarwinDevice(ctx, request)
		return reconcileNativeSecretSet(ctx, request, err)
	}
	expected := []byte(token)
	defer zeroSecretBytes(expected)
	err := setDarwinSecretInProcess(
		ctx,
		service,
		account,
		token,
		ui,
		"write relay auth token",
	)
	return reconcileDarwinInProcessSet(
		ctx,
		service,
		account,
		expected,
		err,
	)
}

func deleteDarwinRelayAuthToken(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) error {
	if darwinDeviceSecureStorageBoundaryAvailable() {
		request := nativeSecretWorkerRequest{
			SchemaVersion: nativeSecretWorkerSchema,
			Operation:     nativeSecretDelete,
			UI:            ui,
			Service:       service,
			Account:       account,
		}
		_, err := runNativeSecretWorkerForDarwinDevice(ctx, request)
		return reconcileNativeSecretDelete(ctx, request, err)
	}
	err := deleteDarwinSecretInProcess(
		ctx,
		service,
		account,
		ui,
		"delete relay auth token",
	)
	return reconcileDarwinInProcessDelete(
		ctx,
		service,
		account,
		err,
	)
}
