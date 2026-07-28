//go:build darwin

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

func TestDarwinRelayTokenReadUsesNoUIPath(t *testing.T) {
	withDeviceStorageTestHome(t)
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalRead := readDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	readDarwinSecretInProcess = func(
		_ context.Context,
		service, account string,
		ui SecretStoreUIPolicy,
		operation string,
	) (string, bool, error) {
		if service != relayAuthTokenServiceName() ||
			account != secretUser() ||
			ui != SecretStoreForbidUI ||
			operation == "" {
			t.Fatalf("unexpected relay-token read")
		}
		return "go-keyring-base64:" +
			base64.StdEncoding.EncodeToString([]byte("relay-token")), true, nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		readDarwinSecretInProcess = originalRead
	})

	token, err := readRelayAuthToken()
	if err != nil || token != "relay-token" {
		t.Fatalf("relay token = %q, %v", token, err)
	}
}

func TestDarwinRelayTokenInteractiveWriteUsesMatchingNativePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", "")
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalRead := readDarwinSecretInProcess
	originalSet := setDarwinSecretInProcess
	originalDelete := deleteDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	var operations []string
	readDarwinSecretInProcess = func(
		_ context.Context,
		service, account string,
		ui SecretStoreUIPolicy,
		operation string,
	) (string, bool, error) {
		if service != relayAuthTokenServiceName() ||
			account != secretUser() ||
			ui != SecretStoreAllowUI ||
			operation == "" {
			t.Fatalf("unexpected relay-token replacement read")
		}
		operations = append(operations, "read")
		return "previous-token", true, nil
	}
	deleteDarwinSecretInProcess = func(
		_ context.Context,
		service, account string,
		ui SecretStoreUIPolicy,
		operation string,
	) error {
		if service != relayAuthTokenServiceName() ||
			account != secretUser() ||
			ui != SecretStoreAllowUI ||
			operation == "" {
			t.Fatalf("unexpected relay-token replacement delete")
		}
		operations = append(operations, "delete")
		return nil
	}
	setDarwinSecretInProcess = func(
		_ context.Context,
		service, account, value string,
		ui SecretStoreUIPolicy,
		operation string,
	) error {
		if service != relayAuthTokenServiceName() ||
			account != secretUser() ||
			value != "relay-token" ||
			ui != SecretStoreAllowUI ||
			operation == "" {
			t.Fatalf("unexpected relay-token write")
		}
		operations = append(operations, "set")
		return nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		readDarwinSecretInProcess = originalRead
		setDarwinSecretInProcess = originalSet
		deleteDarwinSecretInProcess = originalDelete
	})

	if err := writeRelayAuthTokenInteractive("relay-token"); err != nil {
		t.Fatal(err)
	}
	if len(operations) != 3 ||
		operations[0] != "read" ||
		operations[1] != "delete" ||
		operations[2] != "set" {
		t.Fatalf("interactive replacement operations = %v", operations)
	}
}

func TestDarwinRelayTokenBackgroundMutationsForbidUI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", "")
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalSet := setDarwinSecretInProcess
	originalDelete := deleteDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	var policies []SecretStoreUIPolicy
	deleteDarwinSecretInProcess = func(
		_ context.Context,
		_, _ string,
		ui SecretStoreUIPolicy,
		_ string,
	) error {
		policies = append(policies, ui)
		return nil
	}
	setDarwinSecretInProcess = func(
		_ context.Context,
		_, _, _ string,
		ui SecretStoreUIPolicy,
		_ string,
	) error {
		policies = append(policies, ui)
		return nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		setDarwinSecretInProcess = originalSet
		deleteDarwinSecretInProcess = originalDelete
	})

	if err := writeRelayAuthToken("relay-token"); err != nil {
		t.Fatal(err)
	}
	if err := deleteRelayAuthToken(); err != nil {
		t.Fatal(err)
	}
	if len(policies) != 2 ||
		policies[0] != SecretStoreForbidUI ||
		policies[1] != SecretStoreForbidUI {
		t.Fatalf("background mutation policies = %v", policies)
	}
}

func TestDarwinRelayTokenInteractiveReplacementRestoresOnWriteFailure(
	t *testing.T,
) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", "")
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalRead := readDarwinSecretInProcess
	originalSet := setDarwinSecretInProcess
	originalDelete := deleteDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	readDarwinSecretInProcess = func(
		context.Context,
		string, string, SecretStoreUIPolicy, string,
	) (string, bool, error) {
		return "previous-token", true, nil
	}
	deleteDarwinSecretInProcess = func(
		context.Context,
		string, string, SecretStoreUIPolicy, string,
	) error {
		return nil
	}
	writeErr := errors.New("new token write failed")
	var values []string
	var cancelReplacement context.CancelFunc
	setDarwinSecretInProcess = func(
		ctx context.Context,
		_, _, value string,
		_ SecretStoreUIPolicy,
		_ string,
	) error {
		values = append(values, value)
		if value == "new-token" {
			cancelReplacement()
			return writeErr
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("rollback reused failed context: %v", err)
		}
		return nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		readDarwinSecretInProcess = originalRead
		setDarwinSecretInProcess = originalSet
		deleteDarwinSecretInProcess = originalDelete
	})

	replacementCtx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()
	cancelReplacement = cancel
	err := replaceDarwinRelayAuthToken(
		replacementCtx,
		relayAuthTokenServiceName(),
		secretUser(),
		"new-token",
	)
	if !errors.Is(err, writeErr) ||
		len(values) != 2 ||
		values[0] != "new-token" ||
		values[1] != "previous-token" {
		t.Fatalf("replacement error=%v values=%v", err, values)
	}
}

func TestDarwinRelayTokenUsesHardenedWorker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", "")
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalRead := readDarwinSecretThroughBoundary
	originalWorker := runNativeSecretWorkerForDarwinDevice
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return true }
	var readPolicies []SecretStoreUIPolicy
	readDarwinSecretThroughBoundary = func(
		_ context.Context,
		service, account string,
		ui SecretStoreUIPolicy,
		operation string,
	) (string, bool, error) {
		if service != relayAuthTokenServiceName() ||
			account != secretUser() ||
			operation == "" {
			t.Fatalf("unexpected hardened relay-token read")
		}
		readPolicies = append(readPolicies, ui)
		return "relay-token", true, nil
	}
	var requests []nativeSecretWorkerRequest
	runNativeSecretWorkerForDarwinDevice = func(
		_ context.Context,
		request nativeSecretWorkerRequest,
	) (nativeSecretWorkerResponse, error) {
		requests = append(requests, request)
		return nativeSecretWorkerResponse{
			SchemaVersion: nativeSecretWorkerSchema,
		}, nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		readDarwinSecretThroughBoundary = originalRead
		runNativeSecretWorkerForDarwinDevice = originalWorker
	})

	if token, err := readRelayAuthToken(); err != nil || token != "relay-token" {
		t.Fatalf("hardened relay-token read = %q, %v", token, err)
	}
	if err := writeRelayAuthToken("relay-token"); err != nil {
		t.Fatal(err)
	}
	if err := deleteRelayAuthToken(); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayAuthTokenInteractive("replacement-token"); err != nil {
		t.Fatal(err)
	}
	if len(readPolicies) != 2 ||
		readPolicies[0] != SecretStoreForbidUI ||
		readPolicies[1] != SecretStoreAllowUI {
		t.Fatalf("hardened relay-token read policies = %v", readPolicies)
	}
	if len(requests) != 4 ||
		requests[0].Operation != nativeSecretSet ||
		requests[1].Operation != nativeSecretDelete ||
		requests[2].Operation != nativeSecretDelete ||
		requests[3].Operation != nativeSecretSet {
		t.Fatalf("hardened relay-token requests = %+v", requests)
	}
	for index, request := range requests {
		wantUI := SecretStoreForbidUI
		if index >= 2 {
			wantUI = SecretStoreAllowUI
		}
		if request.Service != relayAuthTokenServiceName() ||
			request.Account != secretUser() ||
			request.UI != wantUI {
			t.Fatalf("unexpected hardened relay-token request: %+v", request)
		}
	}
}

func TestDarwinInteractiveCurrentCredentialRecreatesKeychainItem(
	t *testing.T,
) {
	originalBoundary := darwinDeviceSecureStorageBoundaryAvailable
	originalSet := setDarwinSecretInProcess
	originalDelete := deleteDarwinSecretInProcess
	darwinDeviceSecureStorageBoundaryAvailable = func() bool { return false }
	var operations []string
	deleteDarwinSecretInProcess = func(
		_ context.Context,
		service, _ string,
		ui SecretStoreUIPolicy,
		_ string,
	) error {
		if service != deviceCredentialService || ui != SecretStoreAllowUI {
			t.Fatalf("unexpected interactive delete: %q / %q", service, ui)
		}
		operations = append(operations, "delete")
		return nil
	}
	setDarwinSecretInProcess = func(
		_ context.Context,
		service, _, _ string,
		ui SecretStoreUIPolicy,
		_ string,
	) error {
		if service != deviceCredentialService || ui != SecretStoreAllowUI {
			t.Fatalf("unexpected interactive set: %q / %q", service, ui)
		}
		operations = append(operations, "set")
		return nil
	}
	t.Cleanup(func() {
		darwinDeviceSecureStorageBoundaryAvailable = originalBoundary
		setDarwinSecretInProcess = originalSet
		deleteDarwinSecretInProcess = originalDelete
	})

	err := darwinDeviceSecretSet(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 2 ||
		operations[0] != "delete" ||
		operations[1] != "set" {
		t.Fatalf("interactive replacement operations = %v", operations)
	}
}

func TestNativeSecretWorkerAcceptsRelayTokenKey(t *testing.T) {
	service := relayAuthTokenServiceName()
	if !validNativeSecretWorkerKey(service, secretUser()) {
		t.Fatalf("relay-token key rejected: %q / %q", service, secretUser())
	}
	if validNativeSecretWorkerKey(service, secretUser()+".other") {
		t.Fatal("relay-token key accepted for another account")
	}
}
