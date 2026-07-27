//go:build darwin

package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
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
	originalNoUI := macOSDeviceCredentialKeychainAvailableNoUI
	deviceCredentialPromptSessionAvailable = func() bool { return true }
	macOSDeviceCredentialKeychainAvailableNoUI = func(context.Context) error {
		t.Fatal("explicit prompt preflight ran the no-UI keychain probe")
		return nil
	}
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = originalPrompt
		macOSDeviceCredentialKeychainAvailableNoUI = originalNoUI
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
	originalNoUI := macOSDeviceCredentialKeychainAvailableNoUI
	deviceCredentialPromptSessionAvailable = func() bool { return false }
	macOSDeviceCredentialKeychainAvailableNoUI = func(context.Context) error {
		t.Fatal("unsafe prompt session reached the no-UI keychain probe")
		return nil
	}
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = originalPrompt
		macOSDeviceCredentialKeychainAvailableNoUI = originalNoUI
	})

	err := darwinDeviceCredentialPreflight(
		context.Background(),
		SecretStoreAllowUI,
	)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
		t.Fatalf("unsafe prompt session error = %v", err)
	}
}

func TestDarwinDeviceCredentialPreflightForbidUIFailsLocked(t *testing.T) {
	originalPrompt := deviceCredentialPromptSessionAvailable
	originalNoUI := macOSDeviceCredentialKeychainAvailableNoUI
	deviceCredentialPromptSessionAvailable = func() bool {
		t.Fatal("no-UI preflight consulted interactive prompt state")
		return false
	}
	macOSDeviceCredentialKeychainAvailableNoUI = func(context.Context) error {
		return errors.New("locked")
	}
	t.Cleanup(func() {
		deviceCredentialPromptSessionAvailable = originalPrompt
		macOSDeviceCredentialKeychainAvailableNoUI = originalNoUI
	})

	err := darwinDeviceCredentialPreflight(
		context.Background(),
		SecretStoreForbidUI,
	)
	if !isDesktopKeyringLockedError(err) {
		t.Fatalf("no-UI locked preflight error = %v", err)
	}
}
