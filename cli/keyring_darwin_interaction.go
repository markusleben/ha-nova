//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
)

var errDarwinKeychainInteractionRestore = errors.New(
	"macOS Keychain interaction restoration failed",
)

var darwinKeychainInteractionSemaphore = make(chan struct{}, 1)
var loadDarwinKeychainSecurity = loadDarwinOAuthSecurity
var setDarwinKeychainInteraction = setDarwinOAuthInteraction
var restoreDarwinKeychainInteraction = func(previous uint8) darwinOAuthOSStatus {
	return darwinOAuthSecurity.setInteraction(previous)
}
var getDarwinKeychainSecret = darwinOAuthGet
var setDarwinKeychainSecret = darwinOAuthSet
var deleteDarwinKeychainSecret = darwinOAuthDelete

func readDarwinKeychainSecretInProcess(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
	operation string,
) (string, bool, error) {
	if !validNativeSecretWorkerKey(service, account) {
		return "", false, newCloudError(
			CloudErrInvalidInput,
			operation,
			nil,
		)
	}

	var raw []byte
	var found bool
	err := withDarwinKeychainInteraction(
		ctx,
		operation,
		ui,
		false,
		func() error {
			var status darwinOAuthOSStatus
			raw, found, status = getDarwinKeychainSecret(service, account)
			if status == darwinOAuthSuccess {
				return nil
			}
			return newCloudError(
				darwinOAuthErrorCode(status, ui),
				operation,
				fmt.Errorf("OSStatus %d", status),
			)
		},
	)
	if err != nil {
		zeroSecretBytes(raw)
		return "", false, err
	}
	defer zeroSecretBytes(raw)
	if !found {
		return "", false, nil
	}
	return string(raw), true, nil
}

func setDarwinKeychainSecretInProcess(
	ctx context.Context,
	service, account, value string,
	ui SecretStoreUIPolicy,
	operation string,
) error {
	if !validNativeSecretWorkerKey(service, account) || value == "" {
		return newCloudError(CloudErrInvalidInput, operation, nil)
	}
	raw := []byte(value)
	defer zeroSecretBytes(raw)
	return withDarwinKeychainInteraction(
		ctx,
		operation,
		ui,
		true,
		func() error {
			status := setDarwinKeychainSecret(service, account, raw)
			if status == darwinOAuthSuccess {
				return nil
			}
			return newCloudError(
				darwinOAuthErrorCode(status, ui),
				operation,
				fmt.Errorf("OSStatus %d", status),
			)
		},
	)
}

func deleteDarwinKeychainSecretInProcess(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
	operation string,
) error {
	if !validNativeSecretWorkerKey(service, account) {
		return newCloudError(CloudErrInvalidInput, operation, nil)
	}
	return withDarwinKeychainInteraction(
		ctx,
		operation,
		ui,
		true,
		func() error {
			status := deleteDarwinKeychainSecret(service, account)
			if status == darwinOAuthSuccess ||
				status == darwinOAuthItemNotFound {
				return nil
			}
			return newCloudError(
				darwinOAuthErrorCode(status, ui),
				operation,
				fmt.Errorf("OSStatus %d", status),
			)
		},
	)
}

func reconcileDarwinInProcessSet(
	ctx context.Context,
	service, account string,
	expected []byte,
	mutationErr error,
) error {
	reconciled := reconcileSecretSetWithRead(
		ctx,
		expected,
		mutationErr,
		darwinInProcessReconciliationRead(service, account),
	)
	if reconciled == nil &&
		errors.Is(mutationErr, errDarwinKeychainInteractionRestore) {
		return mutationErr
	}
	return reconciled
}

func reconcileDarwinInProcessDelete(
	ctx context.Context,
	service, account string,
	mutationErr error,
) error {
	reconciled := reconcileSecretDeleteWithRead(
		ctx,
		mutationErr,
		darwinInProcessReconciliationRead(service, account),
	)
	if reconciled == nil &&
		errors.Is(mutationErr, errDarwinKeychainInteractionRestore) {
		return mutationErr
	}
	return reconciled
}

func darwinInProcessReconciliationRead(
	service, account string,
) nativeSecretReconciliationRead {
	return func(readCtx context.Context) ([]byte, bool, error) {
		value, found, err := readDarwinSecretInProcess(
			readCtx,
			service,
			account,
			SecretStoreForbidUI,
			"reconcile macOS Keychain mutation",
		)
		return []byte(value), found, err
	}
}

func withDarwinKeychainInteraction(
	ctx context.Context,
	operation string,
	ui SecretStoreUIPolicy,
	mutating bool,
	run func() error,
) (resultErr error) {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	if ui == SecretStoreAllowUI &&
		!deviceCredentialPromptSessionAvailable() {
		return deviceCredentialPromptUnavailableError()
	}
	if err := ctx.Err(); err != nil {
		return newCloudError(CloudErrTimeout, operation, err)
	}
	select {
	case darwinKeychainInteractionSemaphore <- struct{}{}:
		defer func() { <-darwinKeychainInteractionSemaphore }()
	case <-ctx.Done():
		return newCloudError(CloudErrTimeout, operation, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return newCloudError(CloudErrTimeout, operation, err)
	}

	// SecKeychainSetUserInteractionAllowed is process-wide. Signed production
	// builds isolate this policy in a verified worker. Development/ad-hoc builds
	// serialize the short in-process operation and restore the prior policy.
	if err := loadDarwinKeychainSecurity(); err != nil {
		return newCloudError(
			CloudErrSecretStore,
			"load macOS Keychain",
			err,
		)
	}
	previous, err := setDarwinKeychainInteraction(ui)
	if err != nil {
		return newCloudError(
			CloudErrSecretStore,
			"set macOS Keychain interaction policy",
			err,
		)
	}
	defer func() {
		panicValue := recover()
		restoreErr := restoreDarwinKeychainPolicy(previous)
		switch {
		case panicValue != nil:
			code := CloudErrSecretStore
			if mutating {
				code = CloudErrSecretOutcomeUnknown
			}
			panicErr := newCloudError(
				code,
				operation,
				fmt.Errorf("macOS Keychain operation panicked"),
			)
			if restoreErr != nil {
				resultErr = errors.Join(panicErr, restoreErr)
			} else {
				resultErr = panicErr
			}
		case restoreErr != nil && resultErr != nil:
			resultErr = errors.Join(resultErr, restoreErr)
		case restoreErr != nil && mutating:
			resultErr = newCloudError(
				CloudErrSecretOutcomeUnknown,
				operation,
				restoreErr,
			)
		case restoreErr != nil:
			resultErr = restoreErr
		}
	}()
	resultErr = run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		code := CloudErrTimeout
		if mutating {
			code = CloudErrSecretOutcomeUnknown
		}
		resultErr = newCloudError(code, operation, ctxErr)
	}
	return resultErr
}

func restoreDarwinKeychainPolicy(previous uint8) error {
	status, panicValue := callDarwinKeychainRestore(previous)
	if panicValue != nil {
		return newCloudError(
			CloudErrSecretStore,
			"restore macOS Keychain interaction policy",
			fmt.Errorf(
				"%w: restore panicked",
				errDarwinKeychainInteractionRestore,
			),
		)
	}
	if status == darwinOAuthSuccess {
		return nil
	}
	status, panicValue = callDarwinKeychainRestore(previous)
	if panicValue == nil && status == darwinOAuthSuccess {
		return nil
	}
	if panicValue != nil {
		return newCloudError(
			CloudErrSecretStore,
			"restore macOS Keychain interaction policy",
			fmt.Errorf(
				"%w: restore panicked",
				errDarwinKeychainInteractionRestore,
			),
		)
	}
	return newCloudError(
		CloudErrSecretStore,
		"restore macOS Keychain interaction policy",
		fmt.Errorf(
			"%w: OSStatus %d",
			errDarwinKeychainInteractionRestore,
			status,
		),
	)
}

func callDarwinKeychainRestore(
	previous uint8,
) (status darwinOAuthOSStatus, panicValue any) {
	defer func() {
		panicValue = recover()
	}()
	status = restoreDarwinKeychainInteraction(previous)
	return status, nil
}
