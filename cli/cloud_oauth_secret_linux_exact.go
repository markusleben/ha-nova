//go:build linux

package main

import (
	"context"
	"crypto/subtle"
)

func (b *linuxOAuthSecretBackend) DeleteExact(
	ctx context.Context,
	service, account, expected string,
	ui SecretStoreUIPolicy,
) error {
	if expected == "" || len(expected) > oauthSecretMaxEncodedSize {
		return newCloudError(
			CloudErrInvalidInput,
			"delete exact OAuth secret",
			nil,
		)
	}
	linuxOAuthSecretMutationMu.Lock()
	defer linuxOAuthSecretMutationMu.Unlock()
	actual, exists, err := b.Get(ctx, service, account, ui)
	if err != nil || !exists {
		return err
	}
	actualRaw := []byte(actual)
	expectedRaw := []byte(expected)
	defer zeroSecretBytes(actualRaw)
	defer zeroSecretBytes(expectedRaw)
	if subtle.ConstantTimeCompare(
		actualRaw,
		expectedRaw,
	) != 1 {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact OAuth secret",
			nil,
		)
	}
	return b.delete(
		ctx,
		service,
		account,
		ui,
		expectedRaw,
	)
}
