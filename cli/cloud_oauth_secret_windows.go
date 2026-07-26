//go:build windows

package main

import (
	"context"
)

type windowsOAuthSecretBackend struct{}

func newNativeOAuthSecretBackend() (OAuthSecretBackend, error) {
	return &windowsOAuthSecretBackend{}, nil
}

func (b *windowsOAuthSecretBackend) Get(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) (string, bool, error) {
	if err := validateOAuthSecretBackendKey(ctx, service, account, ui); err != nil {
		return "", false, err
	}
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
	value := string(response.Value)
	zeroSecretBytes(response.Value)
	return value, response.Found, nil
}

func (b *windowsOAuthSecretBackend) Set(
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
	operationCtx, cancel := boundedNativeOAuthSecretContext(ctx, ui)
	defer cancel()
	raw := []byte(value)
	defer zeroSecretBytes(raw)
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

func (b *windowsOAuthSecretBackend) Delete(
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

func (b *windowsOAuthSecretBackend) DeleteExact(
	ctx context.Context,
	service, account, expected string,
	ui SecretStoreUIPolicy,
) error {
	if err := validateOAuthSecretBackendKey(
		ctx,
		service,
		account,
		ui,
	); err != nil {
		return err
	}
	if expected == "" || len(expected) > oauthSecretMaxEncodedSize {
		return newCloudError(
			CloudErrInvalidInput,
			"delete exact OAuth secret",
			nil,
		)
	}
	raw := []byte(expected)
	defer zeroSecretBytes(raw)
	operationCtx, cancel := boundedNativeOAuthSecretContext(ctx, ui)
	defer cancel()
	request := nativeSecretWorkerRequest{
		SchemaVersion: nativeSecretWorkerSchema,
		Operation:     nativeSecretDeleteExact,
		UI:            ui,
		Service:       service,
		Account:       account,
		Value:         raw,
	}
	_, err := runNativeSecretWorkerProcess(
		operationCtx,
		request,
	)
	return reconcileNativeSecretDeleteExact(
		ctx,
		request,
		err,
	)
}
