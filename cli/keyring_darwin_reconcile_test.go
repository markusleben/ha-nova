//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestDarwinDeviceSecretSetReconcilesUnknownOutcome(t *testing.T) {
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalSet := setDarwinSecretInProcess
	originalRead := readDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	setDarwinSecretInProcess = func(
		context.Context,
		string, string, string, SecretStoreUIPolicy, string,
	) error {
		return newCloudError(
			CloudErrSecretOutcomeUnknown,
			"write test credential",
			nil,
		)
	}
	readDarwinSecretInProcess = func(
		_ context.Context,
		_, _ string,
		ui SecretStoreUIPolicy,
		_ string,
	) (string, bool, error) {
		if ui != SecretStoreForbidUI {
			t.Fatalf("reconciliation policy = %q", ui)
		}
		return "credential", true, nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		setDarwinSecretInProcess = originalSet
		readDarwinSecretInProcess = originalRead
	})

	err := darwinDeviceSecretSet(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatalf("reconciled write error = %v", err)
	}
}

func TestDarwinDeviceSecretSetPreservesRestoreFailureAfterReconciliation(
	t *testing.T,
) {
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalSet := setDarwinSecretInProcess
	originalRead := readDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	restoreErr := fmt.Errorf(
		"%w: test restore",
		errDarwinKeychainInteractionRestore,
	)
	setDarwinSecretInProcess = func(
		context.Context,
		string, string, string, SecretStoreUIPolicy, string,
	) error {
		return newCloudError(
			CloudErrSecretOutcomeUnknown,
			"write test credential",
			restoreErr,
		)
	}
	readDarwinSecretInProcess = func(
		context.Context,
		string, string, SecretStoreUIPolicy, string,
	) (string, bool, error) {
		return "credential", true, nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		setDarwinSecretInProcess = originalSet
		readDarwinSecretInProcess = originalRead
	})

	err := darwinDeviceSecretSet(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreForbidUI,
	)
	if !errors.Is(err, errDarwinKeychainInteractionRestore) {
		t.Fatalf("reconciled write lost restore failure: %v", err)
	}
}

func TestDarwinDeviceSecretDeleteReconcilesUnknownOutcome(t *testing.T) {
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalDelete := deleteDarwinSecretInProcess
	originalRead := readDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	deleteDarwinSecretInProcess = func(
		context.Context,
		string, string, SecretStoreUIPolicy, string,
	) error {
		return newCloudError(
			CloudErrSecretOutcomeUnknown,
			"delete test credential",
			nil,
		)
	}
	readDarwinSecretInProcess = func(
		context.Context,
		string, string, SecretStoreUIPolicy, string,
	) (string, bool, error) {
		return "", false, nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		deleteDarwinSecretInProcess = originalDelete
		readDarwinSecretInProcess = originalRead
	})

	err := darwinDeviceSecretDelete(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatalf("reconciled delete error = %v", err)
	}
}
