//go:build darwin

package main

import (
	"context"
)

type darwinOAuthSecretBackend struct{}

func newNativeOAuthSecretBackend() (OAuthSecretBackend, error) {
	return &darwinOAuthSecretBackend{}, nil
}

func (b *darwinOAuthSecretBackend) Get(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) (string, bool, error) {
	if err := validateOAuthSecretBackendKey(ctx, service, account, ui); err != nil {
		return "", false, err
	}
	return readDarwinKeychainSecret(
		ctx,
		service,
		account,
		ui,
		"read OAuth secret",
	)
}

func readDarwinKeychainSecret(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
	_ string,
) (string, bool, error) {
	operationCtx, cancel := boundedNativeOAuthSecretContext(ctx, ui)
	defer cancel()
	response, err := runNativeSecretWorkerProcess(
		operationCtx,
		nativeSecretWorkerRequest{
			SchemaVersion: nativeSecretWorkerSchema,
			Operation:     nativeSecretGet,
			UI:            ui,
			Service:       service,
			Account:       account,
		},
	)
	if err != nil {
		return "", false, err
	}
	secret := string(response.Value)
	zeroSecretBytes(response.Value)
	return secret, response.Found, nil
}

func (b *darwinOAuthSecretBackend) Set(
	ctx context.Context,
	service, account, value string,
	ui SecretStoreUIPolicy,
) error {
	if err := validateOAuthSecretBackendKey(ctx, service, account, ui); err != nil {
		return err
	}
	if value == "" || len(value) > oauthSecretMaxEncodedSize {
		return newCloudError(CloudErrInvalidInput, "write OAuth secret", nil)
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

func (b *darwinOAuthSecretBackend) Delete(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) error {
	if err := validateOAuthSecretBackendKey(ctx, service, account, ui); err != nil {
		return err
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
