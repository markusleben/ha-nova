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

func onSecureStoragePlatform(t *testing.T, goos string) {
	t.Helper()
	old := secureStorageSessionPlatformOS
	t.Cleanup(func() { secureStorageSessionPlatformOS = old })
	secureStorageSessionPlatformOS = goos
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
	onSecureStoragePlatform(t, "windows")
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
		next := "sign in to the desktop of the computer HA NOVA is installed on"
		if ssh {
			next = "start your AI assistant from that terminal"
		} else if !strings.Contains(message, "unlock this computer's keychain or keyring") {
			t.Fatalf("desktop: no unlock step in %q", message)
		}
		if got := strings.Contains(message, next); got != ssh {
			t.Fatalf("ssh=%v: desktop next step present=%v in %q", ssh, got, message)
		}
	}
}

// Setup over SSH (rc2 lane D5): a plain next step, the raw error as a detail,
// and the caller does not print the raw error again.
func TestSetupSecureStorageFailureGivesDesktopNextStepOverSSH(t *testing.T) {
	err := newCloudError(CloudErrSecretStore, "access native secure storage", nil)
	onSecureStoragePlatform(t, "windows")

	installSafeNativePromptContext(t)
	out := &strings.Builder{}
	returned := renderSetupSecureStorageFailure(out, "Local pairing cannot start", err)
	if !strings.Contains(out.String(), "Local pairing cannot start: access native secure storage: SECRET_STORE_UNAVAILABLE") ||
		strings.Contains(out.String(), "Next step") || isSetupFailureReported(returned) {
		t.Fatalf("desktop session output:\n%s", out.String())
	}

	t.Setenv("SSH_CONNECTION", "10.0.0.2 50000 10.0.0.3 22")
	out.Reset()
	returned = renderSetupSecureStorageFailure(out, "Local pairing cannot start", err)
	if !isSetupFailureReported(fmt.Errorf("wrapped: %w", returned)) || !IsCloudErrorCode(returned, CloudErrSecretStore) {
		t.Fatalf("returned = %v, want the reported marker around the cause", returned)
	}
	for _, want := range []string{
		"can't reach secure storage from this terminal",
		"Next step: sign in to the desktop of the computer HA NOVA is installed on",
		"run: ha-nova setup",
		"Details: Local pairing cannot start: access native secure storage: SECRET_STORE_UNAVAILABLE",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("SSH output missing %q:\n%s", want, out.String())
		}
	}

	// Linux keeps its keyring guidance (a keyring nobody unlocks needs
	// setup --service, not a desktop).
	onSecureStoragePlatform(t, "linux")
	out.Reset()
	renderSetupSecureStorageFailure(out, "Local pairing cannot start", err)
	if strings.Contains(out.String(), "Next step") {
		t.Fatalf("linux SSH output:\n%s", out.String())
	}
}
