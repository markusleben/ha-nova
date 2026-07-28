//go:build darwin

package main

import (
	"context"
	"testing"
)

func TestDeleteDarwinKeychainSecretInProcessNoUIRestoresPolicy(
	t *testing.T,
) {
	installDarwinKeychainNoUITestSeams(t)
	var policy SecretStoreUIPolicy
	var restored uint8
	setDarwinKeychainInteraction = func(
		ui SecretStoreUIPolicy,
	) (uint8, error) {
		policy = ui
		return 1, nil
	}
	restoreDarwinKeychainInteraction = func(
		previous uint8,
	) darwinOAuthOSStatus {
		restored = previous
		return darwinOAuthSuccess
	}
	deleteCalls := 0
	deleteDarwinKeychainSecret = func(
		service, account string,
	) darwinOAuthOSStatus {
		if service != deviceCredentialService || account != secretUser() {
			t.Fatalf("unexpected Keychain key: %q / %q", service, account)
		}
		deleteCalls++
		return darwinOAuthSuccess
	}

	err := deleteDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
		"delete test credential",
	)
	if err != nil ||
		policy != SecretStoreForbidUI ||
		restored != 1 ||
		deleteCalls != 1 {
		t.Fatalf(
			"delete err=%v policy=%q restored=%d calls=%d",
			err,
			policy,
			restored,
			deleteCalls,
		)
	}
}

func TestDeleteDarwinKeychainSecretInProcessRejectsUnsafePromptSession(
	t *testing.T,
) {
	installDarwinKeychainNoUITestSeams(t)
	deviceCredentialPromptSessionAvailable = func() bool { return false }
	deleteCalls := 0
	deleteDarwinKeychainSecret = func(
		_, _ string,
	) darwinOAuthOSStatus {
		deleteCalls++
		return darwinOAuthSuccess
	}

	err := deleteDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreAllowUI,
		"delete test credential",
	)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) ||
		deleteCalls != 0 {
		t.Fatalf("unsafe prompt error=%v calls=%d", err, deleteCalls)
	}
}
