package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The headless fallback + code-burn guard: a broken keyring must fail BEFORE the
// one-time code is consumed, and a system with no desktop session at all must
// fall back to private files instead of dying on a raw dbus error.

func withDeviceStorageTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// The generic slots honor HA_NOVA_TEST_SECRET_DIR first — clear it so these
	// tests exercise the REAL keyring/file selection logic.
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	prevForced := deviceCredentialFileModeForced
	deviceCredentialFileModeForced = false
	t.Cleanup(func() { deviceCredentialFileModeForced = prevForced })
	return home
}

func stubStorageCanaries(t *testing.T, keyringErr error) {
	t.Helper()
	prevK, prevF := deviceStorageKeyringCanary, deviceStorageFileCanary
	deviceStorageKeyringCanary = func() error { return keyringErr }
	deviceStorageFileCanary = fileStorageCanary
	t.Cleanup(func() { deviceStorageKeyringCanary, deviceStorageFileCanary = prevK, prevF })
}

func TestProbeFallsBackToFilesWhenNoDesktopSession(t *testing.T) {
	home := withDeviceStorageTestHome(t)
	stubStorageCanaries(t, fmt.Errorf("%w: exec dbus-launch not found", errDesktopKeyringSessionUnavailable))

	probe, err := probeDeviceCredentialStorage()
	if err != nil {
		t.Fatalf("expected file fallback, got error: %v", err)
	}
	if probe.mode != "file" {
		t.Fatalf("expected file mode, got %q", probe.mode)
	}
	if !strings.Contains(probe.note, "private file") {
		t.Fatalf("expected a user-facing note about the file fallback, got %q", probe.note)
	}
	if !deviceCredentialFileModeForced {
		t.Fatal("expected the probe to force file mode for subsequent writes")
	}

	// Writes now land in a 0600 file under the config dir, and reads find them.
	cred := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(cred); err != nil {
		t.Fatalf("writeDeviceCredential in file mode: %v", err)
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != cred {
		t.Fatalf("readDeviceCredential in file mode: got=%q ok=%v err=%v", got, ok, err)
	}
	path := filepath.Join(home, ".config", "ha-nova", "secrets", "ha-nova.device-credential")
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("expected credential file at %s: %v", path, statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 file, got %v", info.Mode().Perm())
	}
	if err := deleteDeviceCredential(); err != nil {
		t.Fatalf("deleteDeviceCredential in file mode: %v", err)
	}
	if _, ok, _ := readDeviceCredential(); ok {
		t.Fatal("credential still readable after delete")
	}
}

func TestProbeDoesNotDowngradeALockedDesktopKeyring(t *testing.T) {
	withDeviceStorageTestHome(t)
	locked := fmt.Errorf("%w: default collection is locked", errDesktopKeyringLocked)
	stubStorageCanaries(t, locked)

	_, err := probeDeviceCredentialStorage()
	if !errors.Is(err, errDesktopKeyringLocked) {
		t.Fatalf("expected the locked-keyring error to surface, got %v", err)
	}
	if deviceCredentialFileModeForced {
		t.Fatal("a locked desktop keyring must NOT silently switch to file storage")
	}
}

func TestProbeShortCircuitsWhenAFileCredentialExists(t *testing.T) {
	withDeviceStorageTestHome(t)
	// A previous headless pairing left a credential file: the install is in file
	// mode without any probe/canary — reads must self-select the file.
	cred := generateTestDeviceCredential(t)
	if err := deviceSecretFileSet(deviceCredentialService, cred); err != nil {
		t.Fatalf("seed file credential: %v", err)
	}
	stubStorageCanaries(t, fmt.Errorf("canary must not run"))
	deviceStorageKeyringCanary = func() error { t.Fatal("keyring canary ran despite existing file credential"); return nil }

	probe, err := probeDeviceCredentialStorage()
	if err != nil || probe.mode != "file" {
		t.Fatalf("expected silent file mode, got mode=%q err=%v", probe.mode, err)
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != cred {
		t.Fatalf("file credential not readable: got=%q ok=%v err=%v", got, ok, err)
	}
}

func TestSecurePairingAbortsBeforeConsumingTheCodeWhenStorageIsBroken(t *testing.T) {
	withDeviceStorageTestHome(t)
	stubStorageCanaries(t, fmt.Errorf("%w: default collection is locked", errDesktopKeyringLocked))

	pairCalled := false
	prevPair := pairDeviceV1ForPairing
	pairDeviceV1ForPairing = func(_ *http.Client, _ string, _ string, _ deviceMetadata) (*provisionedCredential, error) {
		pairCalled = true
		return nil, fmt.Errorf("must not be reached")
	}
	t.Cleanup(func() { pairDeviceV1ForPairing = prevPair })

	cfg := runtimeConfig{RelayBaseURL: "http://relay.test:8791", ClientInstallID: "inst-test"}
	_, err := runSecurePairing("http://relay.test:8791", "123456", &cfg, func(*runtimeConfig) error { return nil }, defaultPairingClientInfo())
	if err == nil {
		t.Fatal("expected pairing to fail on broken storage")
	}
	if !strings.Contains(err.Error(), "the code was not used") {
		t.Fatalf("expected the code-not-used guarantee in the error, got: %v", err)
	}
	if pairCalled {
		t.Fatal("pairing reached the relay although storage was broken — the one-time code would have been consumed")
	}
}

func generateTestDeviceCredential(t *testing.T) string {
	t.Helper()
	const cred = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if parseDeviceCredential(cred) == nil {
		t.Fatalf("test credential malformed: %q", cred)
	}
	return cred
}
