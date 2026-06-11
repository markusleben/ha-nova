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

// The secure-storage recovery probe stubs the keyring vars and must reach
// them directly: the low-level wrapper stays preflight-free by contract.
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
