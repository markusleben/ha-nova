//go:build darwin

package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestDecodeDarwinGoKeyringValue(t *testing.T) {
	const secret = "device-secret\nwith-unicode-ß"
	for name, encoded := range map[string]string{
		"base64": "go-keyring-base64:" +
			base64.StdEncoding.EncodeToString([]byte(secret)),
		"hex": "go-keyring-encoded:" +
			hex.EncodeToString([]byte(secret)),
		"legacy raw": secret,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeDarwinGoKeyringValue(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if got != secret {
				t.Fatalf("decoded value = %q", got)
			}
		})
	}
}

func TestDarwinDeviceCredentialPreflightAllowsExplicitPrompt(t *testing.T) {
	originalPrompt := deviceCredentialPromptSessionAvailable
	deviceCredentialPromptSessionAvailable = func() bool { return true }
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = originalPrompt
	})

	if err := darwinDeviceCredentialPreflight(
		context.Background(),
		SecretStoreAllowUI,
	); err != nil {
		t.Fatalf("explicit prompt preflight error = %v", err)
	}
}

func TestDarwinDeviceCredentialPreflightRejectsUnsafePromptSession(t *testing.T) {
	originalPrompt := deviceCredentialPromptSessionAvailable
	deviceCredentialPromptSessionAvailable = func() bool { return false }
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = originalPrompt
	})

	err := darwinDeviceCredentialPreflight(
		context.Background(),
		SecretStoreAllowUI,
	)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
		t.Fatalf("unsafe prompt session error = %v", err)
	}
}

func TestDarwinDeviceCredentialPreflightForbidUIDoesNotProbe(t *testing.T) {
	originalPrompt := deviceCredentialPromptSessionAvailable
	deviceCredentialPromptSessionAvailable = func() bool {
		t.Fatal("no-UI preflight consulted interactive prompt state")
		return false
	}
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = originalPrompt
	})

	err := darwinDeviceCredentialPreflight(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatalf("no-UI preflight error = %v", err)
	}
}

func TestDarwinDeviceSecretGetUsesNoUIPathWithoutHardenedBoundary(
	t *testing.T,
) {
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalRead := readDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	calls := 0
	readDarwinSecretInProcess = func(
		_ context.Context,
		service, account string,
		ui SecretStoreUIPolicy,
		operation string,
	) (string, bool, error) {
		calls++
		if service != deviceCredentialService || account != secretUser() {
			t.Fatalf("unexpected Keychain key: %q / %q", service, account)
		}
		if ui != SecretStoreForbidUI || operation == "" {
			t.Fatalf("unexpected policy/operation: %q / %q", ui, operation)
		}
		return "go-keyring-base64:" +
			base64.StdEncoding.EncodeToString([]byte("credential")), true, nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		readDarwinSecretInProcess = originalRead
	})

	value, err := darwinDeviceSecretGet(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
	)
	if err != nil || value != "credential" || calls != 1 {
		t.Fatalf("no-UI read = (%q, %v), calls=%d", value, err, calls)
	}
}

func TestDarwinDeviceSecretGetNoUIReportsMissingItem(t *testing.T) {
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalRead := readDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	readDarwinSecretInProcess = func(
		context.Context,
		string, string, SecretStoreUIPolicy, string,
	) (string, bool, error) {
		return "", false, nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		readDarwinSecretInProcess = originalRead
	})

	_, err := darwinDeviceSecretGet(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
	)
	if !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("missing no-UI read error = %v", err)
	}
}

func TestDarwinDeviceSecretMutationsUseNoUIPathWithoutHardenedBoundary(
	t *testing.T,
) {
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalSet := setDarwinSecretInProcess
	originalDelete := deleteDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	var calls []string
	setDarwinSecretInProcess = func(
		_ context.Context,
		service, account, value string,
		ui SecretStoreUIPolicy,
		operation string,
	) error {
		if service != deviceCredentialService ||
			account != secretUser() ||
			value != "credential" ||
			ui != SecretStoreForbidUI {
			t.Fatalf("unexpected no-UI set input")
		}
		calls = append(calls, operation)
		return nil
	}
	deleteDarwinSecretInProcess = func(
		_ context.Context,
		service, account string,
		ui SecretStoreUIPolicy,
		operation string,
	) error {
		if service != deviceCredentialService ||
			account != secretUser() ||
			ui != SecretStoreForbidUI {
			t.Fatalf("unexpected no-UI delete input")
		}
		calls = append(calls, operation)
		return nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		setDarwinSecretInProcess = originalSet
		deleteDarwinSecretInProcess = originalDelete
	})

	if err := darwinDeviceSecretSet(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	if err := darwinDeviceSecretDelete(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] == "" || calls[1] == "" {
		t.Fatalf("no-UI mutation calls = %v", calls)
	}
}

func TestDarwinDeviceSecretUsesWorkerPathWithHardenedBoundary(
	t *testing.T,
) {
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalRead := readDarwinSecretThroughBoundary
	originalWorker := runNativeSecretWorkerForDarwinDevice
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return true }
	readDarwinSecretThroughBoundary = func(
		_ context.Context,
		service, account string,
		ui SecretStoreUIPolicy,
		operation string,
	) (string, bool, error) {
		if service != deviceCredentialService ||
			account != secretUser() ||
			ui != SecretStoreForbidUI ||
			operation == "" {
			t.Fatalf("unexpected hardened read input")
		}
		return "go-keyring-base64:" +
			base64.StdEncoding.EncodeToString([]byte("credential")), true, nil
	}
	var operations []nativeSecretOperation
	runNativeSecretWorkerForDarwinDevice = func(
		_ context.Context,
		request nativeSecretWorkerRequest,
	) (nativeSecretWorkerResponse, error) {
		if request.Service != deviceCredentialService ||
			request.Account != secretUser() ||
			request.UI != SecretStoreForbidUI {
			t.Fatalf("unexpected hardened worker request: %+v", request)
		}
		operations = append(operations, request.Operation)
		return nativeSecretWorkerResponse{
			SchemaVersion: nativeSecretWorkerSchema,
		}, nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		readDarwinSecretThroughBoundary = originalRead
		runNativeSecretWorkerForDarwinDevice = originalWorker
	})

	value, err := darwinDeviceSecretGet(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
	)
	if err != nil || value != "credential" {
		t.Fatalf("hardened read = %q, %v", value, err)
	}
	if err := darwinDeviceSecretSet(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	if err := darwinDeviceSecretDelete(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	if len(operations) != 2 ||
		operations[0] != nativeSecretSet ||
		operations[1] != nativeSecretDelete {
		t.Fatalf("hardened worker operations = %v", operations)
	}
}
