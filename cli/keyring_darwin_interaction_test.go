//go:build darwin

package main

import (
	"context"
	"errors"
	"testing"
)

func TestReadDarwinKeychainSecretInProcessNoUIRestoresPolicy(t *testing.T) {
	installDarwinKeychainNoUITestSeams(t)
	var restored uint8
	restoreDarwinKeychainInteraction = func(
		previous uint8,
	) darwinOAuthOSStatus {
		restored = previous
		return darwinOAuthSuccess
	}
	getDarwinKeychainSecret = func(
		service, account string,
	) ([]byte, bool, darwinOAuthOSStatus) {
		if service != deviceCredentialService || account != secretUser() {
			t.Fatalf("unexpected Keychain key: %q / %q", service, account)
		}
		return []byte("credential"), true, darwinOAuthSuccess
	}
	value, found, err := readDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
		"read test credential",
	)
	if err != nil || !found || value != "credential" {
		t.Fatalf("no-UI native read = (%q, %v, %v)", value, found, err)
	}
	if restored != 1 {
		t.Fatalf("restored interaction policy = %d, want 1", restored)
	}
}

func TestReadDarwinKeychainSecretInProcessNoUIFailsWithoutPrompt(
	t *testing.T,
) {
	installDarwinKeychainNoUITestSeams(t)
	restoreCalls := 0
	restoreDarwinKeychainInteraction = func(
		uint8,
	) darwinOAuthOSStatus {
		restoreCalls++
		return darwinOAuthSuccess
	}
	getDarwinKeychainSecret = func(
		_, _ string,
	) ([]byte, bool, darwinOAuthOSStatus) {
		return nil, false, darwinOAuthInteractionNotAllowed
	}
	_, _, err := readDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
		"read test credential",
	)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) {
		t.Fatalf("interaction-required read error = %v", err)
	}
	if restoreCalls != 1 {
		t.Fatalf("restore calls = %d, want 1", restoreCalls)
	}
}

func TestReadDarwinKeychainSecretInProcessNoUIRetriesRestoreStatus(
	t *testing.T,
) {
	installDarwinKeychainNoUITestSeams(t)
	restoreCalls := 0
	restoreDarwinKeychainInteraction = func(
		uint8,
	) darwinOAuthOSStatus {
		restoreCalls++
		if restoreCalls == 1 {
			return darwinOAuthAuthFailed
		}
		return darwinOAuthSuccess
	}
	getDarwinKeychainSecret = func(
		_, _ string,
	) ([]byte, bool, darwinOAuthOSStatus) {
		return []byte("credential"), true, darwinOAuthSuccess
	}
	value, found, err := readDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
		"read test credential",
	)
	if err != nil || !found || value != "credential" || restoreCalls != 2 {
		t.Fatalf(
			"restore retry = (%q, %v, %v), calls=%d",
			value,
			found,
			err,
			restoreCalls,
		)
	}
}

func TestReadDarwinKeychainSecretInProcessNoUIZerosAfterRestorePanic(
	t *testing.T,
) {
	installDarwinKeychainNoUITestSeams(t)
	raw := []byte("credential")
	restoreCalls := 0
	getDarwinKeychainSecret = func(
		_, _ string,
	) ([]byte, bool, darwinOAuthOSStatus) {
		return raw, true, darwinOAuthSuccess
	}
	restoreDarwinKeychainInteraction = func(
		uint8,
	) darwinOAuthOSStatus {
		restoreCalls++
		panic("restore panic")
	}

	value, found, err := readDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
		"read test credential",
	)
	if value != "" || found || !IsCloudErrorCode(err, CloudErrSecretStore) {
		t.Fatalf("restore panic = (%q, %v, %v)", value, found, err)
	}
	if restoreCalls != 1 {
		t.Fatalf("restore calls = %d, want 1", restoreCalls)
	}
	for index, value := range raw {
		if value != 0 {
			t.Fatalf("raw[%d] was not zeroed", index)
		}
	}
}

func TestDarwinKeychainNoUIWaitHonorsContext(t *testing.T) {
	installDarwinKeychainNoUITestSeams(t)
	darwinKeychainInteractionSemaphore <- struct{}{}
	t.Cleanup(func() {
		select {
		case <-darwinKeychainInteractionSemaphore:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getCalls := 0
	getDarwinKeychainSecret = func(
		_, _ string,
	) ([]byte, bool, darwinOAuthOSStatus) {
		getCalls++
		return nil, false, darwinOAuthSuccess
	}

	_, _, err := readDarwinKeychainSecretInProcess(
		ctx,
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
		"read test credential",
	)
	if !IsCloudErrorCode(err, CloudErrTimeout) || getCalls != 0 {
		t.Fatalf("canceled wait err=%v getCalls=%d", err, getCalls)
	}
}

func TestSetDarwinKeychainSecretInProcessUsesRequestedPolicy(t *testing.T) {
	installDarwinKeychainNoUITestSeams(t)
	var policy SecretStoreUIPolicy
	setDarwinKeychainInteraction = func(
		ui SecretStoreUIPolicy,
	) (uint8, error) {
		policy = ui
		return 1, nil
	}
	var stored string
	setDarwinKeychainSecret = func(
		service, account string,
		value []byte,
	) darwinOAuthOSStatus {
		if service != deviceCredentialService || account != secretUser() {
			t.Fatalf("unexpected Keychain key: %q / %q", service, account)
		}
		stored = string(value)
		return darwinOAuthSuccess
	}

	err := setDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreAllowUI,
		"write test credential",
	)
	if err != nil || stored != "credential" ||
		policy != SecretStoreAllowUI {
		t.Fatalf("native write err=%v stored=%q policy=%q", err, stored, policy)
	}
}

func TestSetDarwinKeychainSecretInProcessRejectsUnsafePromptSession(
	t *testing.T,
) {
	installDarwinKeychainNoUITestSeams(t)
	deviceCredentialPromptSessionAvailable = func() bool { return false }
	setCalls := 0
	setDarwinKeychainSecret = func(
		_, _ string,
		_ []byte,
	) darwinOAuthOSStatus {
		setCalls++
		return darwinOAuthSuccess
	}

	err := setDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreAllowUI,
		"write test credential",
	)
	if !IsCloudErrorCode(err, CloudErrSecretUIForbidden) || setCalls != 0 {
		t.Fatalf("unsafe prompt session error=%v setCalls=%d", err, setCalls)
	}
}

func TestSetDarwinKeychainSecretInProcessPanicIsOutcomeUnknown(
	t *testing.T,
) {
	installDarwinKeychainNoUITestSeams(t)
	var raw []byte
	setDarwinKeychainSecret = func(
		_, _ string,
		value []byte,
	) darwinOAuthOSStatus {
		raw = value
		panic("write panic")
	}

	err := setDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreForbidUI,
		"write test credential",
	)
	if !IsCloudErrorCode(err, CloudErrSecretOutcomeUnknown) {
		t.Fatalf("write panic error = %v", err)
	}
	for index, value := range raw {
		if value != 0 {
			t.Fatalf("raw[%d] was not zeroed", index)
		}
	}
}

func TestSetDarwinKeychainSecretInProcessPanicPreservesRestoreFailure(
	t *testing.T,
) {
	installDarwinKeychainNoUITestSeams(t)
	setDarwinKeychainSecret = func(
		_, _ string,
		_ []byte,
	) darwinOAuthOSStatus {
		panic("write panic")
	}
	restoreDarwinKeychainInteraction = func(
		uint8,
	) darwinOAuthOSStatus {
		panic("restore panic")
	}

	err := setDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreForbidUI,
		"write test credential",
	)
	if !IsCloudErrorCode(err, CloudErrSecretOutcomeUnknown) ||
		!errors.Is(err, errDarwinKeychainInteractionRestore) {
		t.Fatalf("combined panic error = %v", err)
	}
}

func TestReadDarwinKeychainSecretInProcessRejectsExpiredResult(t *testing.T) {
	installDarwinKeychainNoUITestSeams(t)
	ctx, cancel := context.WithCancel(context.Background())
	raw := []byte("credential")
	getDarwinKeychainSecret = func(
		_, _ string,
	) ([]byte, bool, darwinOAuthOSStatus) {
		cancel()
		return raw, true, darwinOAuthSuccess
	}

	value, found, err := readDarwinKeychainSecretInProcess(
		ctx,
		deviceCredentialService,
		secretUser(),
		SecretStoreForbidUI,
		"read test credential",
	)
	if value != "" || found || !IsCloudErrorCode(err, CloudErrTimeout) {
		t.Fatalf("expired read = (%q, %v, %v)", value, found, err)
	}
	for index, value := range raw {
		if value != 0 {
			t.Fatalf("raw[%d] was not zeroed", index)
		}
	}
}

func TestSetDarwinKeychainSecretInProcessRestoreFailureIsOutcomeUnknown(
	t *testing.T,
) {
	installDarwinKeychainNoUITestSeams(t)
	restoreDarwinKeychainInteraction = func(
		uint8,
	) darwinOAuthOSStatus {
		return darwinOAuthAuthFailed
	}

	err := setDarwinKeychainSecretInProcess(
		context.Background(),
		deviceCredentialService,
		secretUser(),
		"credential",
		SecretStoreForbidUI,
		"write test credential",
	)
	if !IsCloudErrorCode(err, CloudErrSecretOutcomeUnknown) {
		t.Fatalf("restore failure error = %v", err)
	}
	if !errors.Is(err, errDarwinKeychainInteractionRestore) {
		t.Fatalf("restore marker missing from error = %v", err)
	}
}
