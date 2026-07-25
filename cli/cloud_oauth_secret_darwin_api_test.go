//go:build darwin

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestDarwinOAuthSecurityBindingLoads(t *testing.T) {
	if err := loadDarwinOAuthSecurity(); err != nil {
		t.Fatalf("loadDarwinOAuthSecurity() error: %v", err)
	}
	previous, err := setDarwinOAuthInteraction(SecretStoreForbidUI)
	if err != nil {
		t.Fatalf("disable Keychain UI: %v", err)
	}
	if status := darwinOAuthSecurity.setInteraction(previous); status != 0 {
		t.Fatalf("restore Keychain UI status: %d", status)
	}
}

func TestDarwinOAuthKeychainLive(t *testing.T) {
	if os.Getenv("HA_NOVA_TEST_MACOS_KEYCHAIN") != "1" {
		t.Skip("set HA_NOVA_TEST_MACOS_KEYCHAIN=1 for the native Keychain probe")
	}
	if !nativeSecretPromptBaseContextAvailable() {
		t.Fatal("native Keychain probe requires a local non-elevated process")
	}
	originalCommand := nativeSecretWorkerCommandForProcess
	nativeSecretWorkerCommandForProcess = func(
		ctx context.Context,
		_ string,
	) *exec.Cmd {
		command := exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=TestDarwinOAuthKeychainLiveWorker",
		)
		command.Env = append(
			os.Environ(),
			"HA_NOVA_TEST_MACOS_KEYCHAIN_WORKER=1",
		)
		return command
	}
	t.Cleanup(func() {
		nativeSecretWorkerCommandForProcess = originalCommand
	})
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	generation := hex.EncodeToString(random[:16])
	service := oauthSecretPreflightServicePrefix + generation
	account := "probe-" + generation
	backend := &darwinOAuthSecretBackend{}
	if err := backend.Set(
		context.Background(),
		service,
		account,
		string(random),
		SecretStoreForbidUI,
	); err != nil {
		t.Fatalf("native Keychain set: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Delete(
			context.Background(),
			service,
			account,
			SecretStoreForbidUI,
		); err != nil {
			t.Errorf("native Keychain cleanup: %v", err)
		}
	})

	value, found, err := backend.Get(
		context.Background(),
		service,
		account,
		SecretStoreForbidUI,
	)
	if err != nil || !found || len(value) != len(random) {
		t.Fatalf(
			"native Keychain get: found=%v length=%d err=%v",
			found,
			len(value),
			err,
		)
	}
	for index := range random {
		if value[index] != random[index] {
			t.Fatal("native Keychain value mismatch")
		}
	}
	zeroSecretBytes(random)
}

func TestDarwinOAuthKeychainLiveWorker(t *testing.T) {
	if os.Getenv("HA_NOVA_TEST_MACOS_KEYCHAIN_WORKER") != "1" {
		t.Skip("native Keychain worker helper")
	}
	handled, exitCode := maybeRunNativeSecretWorker(
		[]string{nativeSecretWorkerCommand},
		os.Stdin,
		os.Stdout,
	)
	if !handled {
		exitCode = 1
	}
	os.Exit(exitCode)
}

func TestDarwinOAuthBackendStopsBeforeNativeAccessWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := (&darwinOAuthSecretBackend{}).Get(
		ctx,
		oauthSecretCurrentService,
		"default",
		SecretStoreForbidUI,
	)
	if !IsCloudErrorCode(err, CloudErrTimeout) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("canceled direct Keychain read error = %v", err)
	}
}
