package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Regression: device-slot reads/writes must run the keyring preflight (Linux
// wires it to the Secret Service check) before touching go-keyring, so a locked
// backend fails fast with a classified error instead of hanging in an unlock
// prompt.
func TestSecretOpsHonorDeviceCredentialPreflight(t *testing.T) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "") // force the real backend path, not the file escape hatch

	orig := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return errors.New("backend locked") }
	t.Cleanup(func() { deviceCredentialPreflight = orig })

	if _, err := secretGet("ha-nova.device-credential"); err == nil || !strings.Contains(err.Error(), "backend locked") {
		t.Fatalf("secretGet bypassed the preflight: %v", err)
	}
	if err := secretSet("ha-nova.device-credential", "v"); err == nil || !strings.Contains(err.Error(), "backend locked") {
		t.Fatalf("secretSet bypassed the preflight: %v", err)
	}
	if err := secretDelete("ha-nova.device-credential"); err == nil || !strings.Contains(err.Error(), "backend locked") {
		t.Fatalf("secretDelete bypassed the preflight: %v", err)
	}
}

func TestDeviceCredentialReadPropagatesNoUIPolicy(t *testing.T) {
	withDeviceStorageTestHome(t)
	credential := validCredential(73)
	original := secretKeyringGetWithPolicy
	var policy SecretStoreUIPolicy
	secretKeyringGetWithPolicy = func(
		_ context.Context,
		_, _ string,
		ui SecretStoreUIPolicy,
	) (string, error) {
		policy = ui
		return credential, nil
	}
	t.Cleanup(func() { secretKeyringGetWithPolicy = original })

	got, exists, err := readDeviceCredentialWithPolicy(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists || got != credential {
		t.Fatalf(
			"readDeviceCredentialWithPolicy() = (%q, %v, %v)",
			got,
			exists,
			err,
		)
	}
	if policy != SecretStoreForbidUI {
		t.Fatalf("device credential policy = %q", policy)
	}
}

func TestProductionSecretRouterIgnoresTestDirectoryEnvironment(t *testing.T) {
	withDeviceStorageTestHome(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	originalDir := testSecretDirForRuntime
	testSecretDirForRuntime = productionTestSecretDir
	t.Cleanup(func() { testSecretDirForRuntime = originalDir })

	credential := validCredential(74)
	originalGet := secretKeyringGetWithPolicy
	calls := 0
	secretKeyringGetWithPolicy = func(
		_ context.Context,
		service, _ string,
		_ SecretStoreUIPolicy,
	) (string, error) {
		calls++
		if service != deviceCredentialService {
			t.Fatalf("keyring service = %q", service)
		}
		return credential, nil
	}
	t.Cleanup(func() { secretKeyringGetWithPolicy = originalGet })

	got, err := secretGet(deviceCredentialService)
	if err != nil || got != credential || calls != 1 {
		t.Fatalf(
			"production secret route = (%q, %v), keyring calls = %d",
			got,
			err,
			calls,
		)
	}
}
