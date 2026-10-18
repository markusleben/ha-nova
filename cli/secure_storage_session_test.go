package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

// Localized Windows (rc2 lane, German): the logon-session failure is matched
// by its error number, not by the English message.
func TestWindowsLogonSessionErrorMatchesLocalizedErrno(t *testing.T) {
	wrapped := fmt.Errorf("cannot read relay auth token (x): %w", syscall.Errno(1312))
	if !isWindowsNetworkLogonSessionError(wrapped) {
		t.Fatalf("errno 1312 not classified: %v", wrapped)
	}
	if isWindowsNetworkLogonSessionError(fmt.Errorf("x: %w", syscall.Errno(5))) {
		t.Fatal("another errno classified as a logon-session failure")
	}
}

func failLocalDeviceCredentialRead(t *testing.T) {
	t.Helper()
	old := relayFunctionalTransportForDoctor
	t.Cleanup(func() { relayFunctionalTransportForDoctor = old })
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return "", nil, "", false, newCloudError(CloudErrSecretStore, "access native secure storage", errors.New("backend detail"))
	}
}

// A local-only install whose device credential read fails is not a Home
// Assistant Link problem (rc2 lane D6).
func TestLocalCredentialStorageFailureNeverNamesLink(t *testing.T) {
	failLocalDeviceCredentialRead(t)
	for _, ssh := range []bool{false, true} {
		installSafeNativePromptContext(t)
		if ssh {
			t.Setenv("SSH_CONNECTION", "10.0.0.2 50000 10.0.0.3 22")
		}
		_, err := selectRelayTransport(context.Background(), runtimeConfig{RoutePolicy: routePolicyLocal}, "", false)
		if err == nil || !IsCloudErrorCode(err, CloudErrSecretStore) {
			t.Fatalf("ssh=%v: err = %v, want the wrapped SECRET_STORE_UNAVAILABLE", ssh, err)
		}
		message := relayTransportErrorMessage(err)
		if strings.Contains(message, "Link") || strings.Contains(message, "cloud_secure_storage") || strings.Contains(message, "backend detail") {
			t.Fatalf("ssh=%v: message = %q", ssh, message)
		}
		if got := strings.Contains(message, "terminal on this computer's desktop"); got != ssh {
			t.Fatalf("ssh=%v: desktop next step present=%v in %q", ssh, got, message)
		}
	}
}

// Setup over SSH (rc2 lane D5): a plain next step, the raw error as a detail.
func TestSetupSecureStorageFailureGivesDesktopNextStepOverSSH(t *testing.T) {
	err := newCloudError(CloudErrSecretStore, "access native secure storage", nil)

	installSafeNativePromptContext(t)
	out := &strings.Builder{}
	renderSetupSecureStorageFailure(out, "Local pairing cannot start", err)
	if !strings.Contains(out.String(), "Local pairing cannot start: access native secure storage: SECRET_STORE_UNAVAILABLE") ||
		strings.Contains(out.String(), "Next step") {
		t.Fatalf("desktop session output:\n%s", out.String())
	}

	t.Setenv("SSH_CONNECTION", "10.0.0.2 50000 10.0.0.3 22")
	out.Reset()
	renderSetupSecureStorageFailure(out, "Local pairing cannot start", err)
	for _, want := range []string{
		"can't use this computer's secure storage from this terminal",
		"Next step: open a normal terminal on this computer's desktop",
		"run: ha-nova setup",
		"Details: Local pairing cannot start: access native secure storage: SECRET_STORE_UNAVAILABLE",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("SSH output missing %q:\n%s", want, out.String())
		}
	}
}
