//go:build linux

package main

import "context"

var linuxOAuthSecretReadForReconciliation = func(
	ctx context.Context,
	backend *linuxOAuthSecretBackend,
	service, account string,
) ([]byte, bool, error) {
	value, found, err := backend.Get(
		ctx,
		service,
		account,
		SecretStoreForbidUI,
	)
	return []byte(value), found, err
}

func linuxOAuthSecretMutationUnknown(op string, cause error) error {
	return newCloudError(
		CloudErrSecretOutcomeUnknown,
		op,
		normalizeLinuxKeyringError(cause),
	)
}

func reconcileLinuxOAuthSecretSet(
	ctx context.Context,
	backend *linuxOAuthSecretBackend,
	service, account string,
	expected []byte,
	mutationErr error,
) error {
	return reconcileSecretSetWithRead(
		ctx,
		expected,
		mutationErr,
		func(readCtx context.Context) ([]byte, bool, error) {
			return linuxOAuthSecretReadForReconciliation(
				readCtx,
				backend,
				service,
				account,
			)
		},
	)
}

func reconcileLinuxOAuthSecretDelete(
	ctx context.Context,
	backend *linuxOAuthSecretBackend,
	service, account string,
	mutationErr error,
) error {
	return reconcileLinuxOAuthSecretDeleteExpected(
		ctx,
		backend,
		service,
		account,
		nil,
		mutationErr,
	)
}

func reconcileLinuxOAuthSecretDeleteExpected(
	ctx context.Context,
	backend *linuxOAuthSecretBackend,
	service, account string,
	exactExpected []byte,
	mutationErr error,
) error {
	if exactExpected != nil {
		return reconcileSecretDeleteExactWithRead(
			ctx,
			exactExpected,
			mutationErr,
			func(readCtx context.Context) ([]byte, bool, error) {
				return linuxOAuthSecretReadForReconciliation(
					readCtx,
					backend,
					service,
					account,
				)
			},
		)
	}
	return reconcileSecretDeleteWithRead(
		ctx,
		mutationErr,
		func(readCtx context.Context) ([]byte, bool, error) {
			return linuxOAuthSecretReadForReconciliation(
				readCtx,
				backend,
				service,
				account,
			)
		},
	)
}
