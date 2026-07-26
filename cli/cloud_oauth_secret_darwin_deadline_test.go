//go:build darwin

package main

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestDarwinOAuthNativeOperationsHonorHardDeadline(t *testing.T) {
	originalTimeout := nativeOAuthSecretNoUITimeout
	originalCommand := nativeSecretWorkerCommandForProcess
	nativeOAuthSecretNoUITimeout = 20 * time.Millisecond
	nativeSecretWorkerCommandForProcess = func(
		ctx context.Context,
		_ string,
	) *exec.Cmd {
		command := exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=TestNativeSecretWorkerBlockingHelper",
		)
		command.Env = append(
			os.Environ(),
			"HA_NOVA_NATIVE_SECRET_BLOCKING_HELPER=1",
		)
		return command
	}
	t.Cleanup(func() {
		nativeOAuthSecretNoUITimeout = originalTimeout
		nativeSecretWorkerCommandForProcess = originalCommand
	})

	backend := &darwinOAuthSecretBackend{}
	for _, test := range []struct {
		name string
		code CloudErrorCode
		run  func() error
	}{
		{
			name: "Get",
			code: CloudErrTimeout,
			run: func() error {
				_, _, err := backend.Get(
					context.Background(),
					oauthSecretCurrentService,
					"default",
					SecretStoreForbidUI,
				)
				return err
			},
		},
		{
			name: "Set",
			code: CloudErrSecretOutcomeUnknown,
			run: func() error {
				return backend.Set(
					context.Background(),
					oauthSecretCurrentService,
					"default",
					"encoded-secret",
					SecretStoreForbidUI,
				)
			},
		},
		{
			name: "Delete",
			code: CloudErrSecretOutcomeUnknown,
			run: func() error {
				return backend.Delete(
					context.Background(),
					oauthSecretCurrentService,
					"default",
					SecretStoreForbidUI,
				)
			},
		},
		{
			name: "DeleteExact",
			code: CloudErrSecretOutcomeUnknown,
			run: func() error {
				return backend.DeleteExact(
					context.Background(),
					oauthSecretCurrentService,
					"default",
					"encoded-secret",
					SecretStoreForbidUI,
				)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); !IsCloudErrorCode(err, test.code) {
				t.Fatalf("blocked %s error = %v", test.name, err)
			}
		})
	}
}
