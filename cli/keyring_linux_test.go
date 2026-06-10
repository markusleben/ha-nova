//go:build linux

package main

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestReadSecretWithServiceStopsBeforeKeyringWhenLinuxStorageLocked(t *testing.T) {
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
		t.Fatal("readSecretWithService called go-keyring even though the default collection is locked")
		return "", keyring.ErrNotFound
	}

	_, err := readSecretWithService("ha-nova.test")
	if !isDesktopKeyringLockedError(err) {
		t.Fatalf("expected locked keyring classification, got %v", err)
	}
}

func TestReadSecretWithServiceUsesKeyringWhenLinuxStorageWritable(t *testing.T) {
	originalInspect := inspectLinuxSecureStorageStateForKeyring
	originalGet := keyringGetWithService
	defer func() {
		inspectLinuxSecureStorageStateForKeyring = originalInspect
		keyringGetWithService = originalGet
	}()

	inspectLinuxSecureStorageStateForKeyring = func() (linuxSecureStorageState, error) {
		return linuxSecureStorageState{kind: linuxSecureStorageStateWritable}, nil
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

func TestReadSecretWithServiceClassifiesLinuxStorageInspectionErrors(t *testing.T) {
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
		t.Fatal("readSecretWithService called go-keyring after preflight failure")
		return "", errors.New("unreachable")
	}

	_, err := readSecretWithService("ha-nova.test")
	if !isDesktopKeyringUnavailableError(err) {
		t.Fatalf("expected unavailable keyring classification, got %v", err)
	}
}
