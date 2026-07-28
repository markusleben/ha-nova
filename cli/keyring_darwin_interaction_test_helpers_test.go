//go:build darwin

package main

import "testing"

func installDarwinKeychainNoUITestSeams(t *testing.T) {
	t.Helper()
	originalLoad := loadDarwinKeychainSecurity
	originalSetInteraction := setDarwinKeychainInteraction
	originalRestore := restoreDarwinKeychainInteraction
	originalGet := getDarwinKeychainSecret
	originalSet := setDarwinKeychainSecret
	originalDelete := deleteDarwinKeychainSecret
	originalPrompt := deviceCredentialPromptSessionAvailable
	loadDarwinKeychainSecurity = func() error { return nil }
	setDarwinKeychainInteraction = func(
		ui SecretStoreUIPolicy,
	) (uint8, error) {
		if ui != SecretStoreForbidUI {
			t.Fatalf("interaction policy = %q", ui)
		}
		return 1, nil
	}
	restoreDarwinKeychainInteraction = func(
		uint8,
	) darwinOAuthOSStatus {
		return darwinOAuthSuccess
	}
	getDarwinKeychainSecret = func(
		_, _ string,
	) ([]byte, bool, darwinOAuthOSStatus) {
		return nil, false, darwinOAuthSuccess
	}
	setDarwinKeychainSecret = func(
		_, _ string,
		_ []byte,
	) darwinOAuthOSStatus {
		return darwinOAuthSuccess
	}
	deleteDarwinKeychainSecret = func(
		_, _ string,
	) darwinOAuthOSStatus {
		return darwinOAuthSuccess
	}
	deviceCredentialPromptSessionAvailable = func() bool { return true }
	t.Cleanup(func() {
		loadDarwinKeychainSecurity = originalLoad
		setDarwinKeychainInteraction = originalSetInteraction
		restoreDarwinKeychainInteraction = originalRestore
		getDarwinKeychainSecret = originalGet
		setDarwinKeychainSecret = originalSet
		deleteDarwinKeychainSecret = originalDelete
		deviceCredentialPromptSessionAvailable = originalPrompt
	})
}
