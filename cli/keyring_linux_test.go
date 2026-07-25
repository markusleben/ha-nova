//go:build linux

package main

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestReadRelayAuthTokenStopsBeforeKeyringWhenLinuxStorageLocked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalInspect := inspectLinuxSecureStorageStateForKeyring
	originalGet := keyringGetWithService
	defer func() {
		inspectLinuxSecureStorageStateForKeyring = originalInspect
		keyringGetWithService = originalGet
	}()

	inspectLinuxSecureStorageStateForKeyring = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateLocked}, nil
	}
	keyringGetWithService = func(service, username string) (string, error) {
		t.Fatal("readRelayAuthToken called go-keyring even though the default collection is locked")
		return "", keyring.ErrNotFound
	}

	_, err := readRelayAuthToken()
	if !isDesktopKeyringLockedError(err) {
		t.Fatalf("expected locked keyring classification, got %v", err)
	}
}

// The low-level wrapper stays preflight-free; its native backend independently
// enforces a bounded no-UI operation so a relock cannot race the inspection.
func TestReadSecretWithServiceBypassesPreflight(t *testing.T) {
	originalInspect := inspectLinuxSecureStorageStateForKeyring
	originalGet := keyringGetWithService
	defer func() {
		inspectLinuxSecureStorageStateForKeyring = originalInspect
		keyringGetWithService = originalGet
	}()

	inspectLinuxSecureStorageStateForKeyring = func() (linuxSecureStorageState, error) {
		t.Fatal("readSecretWithService must not run the preflight; the recovery probe depends on direct keyring access")
		return linuxSecureStorageState{}, nil
	}
	keyringGetWithService = func(service, username string) (string, error) {
		return "relay-token", nil
	}

	token, err := readSecretWithService("ha-nova.test")
	if err != nil {
		t.Fatalf("readSecretWithService() error = %v", err)
	}
	if token != "relay-token" {
		t.Fatalf("token = %q, want relay-token", token)
	}
}

func TestNativeLinuxKeyringOperationsAlwaysForbidUI(t *testing.T) {
	originalBackend := newNativeLinuxCredentialBackend
	t.Cleanup(func() {
		newNativeLinuxCredentialBackend = originalBackend
	})
	backend := &linuxKeyringProbeTestBackend{}
	newNativeLinuxCredentialBackend = func() (OAuthSecretBackend, error) {
		return backend, nil
	}

	if err := nativeLinuxKeyringSet("ha-nova.test", "user", "secret"); err != nil {
		t.Fatalf("nativeLinuxKeyringSet() error = %v", err)
	}
	value, err := nativeLinuxKeyringGet("ha-nova.test", "user")
	if err != nil || value != "secret" {
		t.Fatalf("nativeLinuxKeyringGet() value=%q err=%v", value, err)
	}
	if err := nativeLinuxKeyringDelete("ha-nova.test", "user"); err != nil {
		t.Fatalf("nativeLinuxKeyringDelete() error = %v", err)
	}
	if len(backend.policies) != 3 {
		t.Fatalf("native backend policies = %v", backend.policies)
	}
	for _, policy := range backend.policies {
		if policy != SecretStoreForbidUI {
			t.Fatalf("ordinary Linux keyring operation allowed UI: %v", backend.policies)
		}
	}
}

func TestNativeLinuxKeyringRelockFailsFastAsLocked(t *testing.T) {
	originalBackend := newNativeLinuxCredentialBackend
	t.Cleanup(func() {
		newNativeLinuxCredentialBackend = originalBackend
	})
	backend := &linuxKeyringProbeTestBackend{
		getErr: newCloudError(
			CloudErrSecretUIForbidden,
			"unlock Secret Service",
			nil,
		),
	}
	newNativeLinuxCredentialBackend = func() (OAuthSecretBackend, error) {
		return backend, nil
	}

	_, err := nativeLinuxKeyringGet("ha-nova.test", "user")
	if !isDesktopKeyringLockedError(err) {
		t.Fatalf("relocked native read error = %v", err)
	}
	if len(backend.policies) != 1 ||
		backend.policies[0] != SecretStoreForbidUI {
		t.Fatalf("relocked native read policies = %v", backend.policies)
	}
}

func TestReadRelayAuthTokenClassifiesLinuxStorageInspectionErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalInspect := inspectLinuxSecureStorageStateForKeyring
	originalGet := keyringGetWithService
	defer func() {
		inspectLinuxSecureStorageStateForKeyring = originalInspect
		keyringGetWithService = originalGet
	}()

	inspectLinuxSecureStorageStateForKeyring = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{}, desktopKeyringUnavailableError("Secret Service preflight timed out")
	}
	keyringGetWithService = func(service, username string) (string, error) {
		t.Fatal("readRelayAuthToken called go-keyring after preflight failure")
		return "", errors.New("unreachable")
	}

	_, err := readRelayAuthToken()
	if !isDesktopKeyringUnavailableError(err) {
		t.Fatalf("expected unavailable keyring classification, got %v", err)
	}
}
