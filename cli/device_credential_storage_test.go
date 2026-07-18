package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// The headless fallback + code-burn guard: a broken keyring must fail BEFORE the
// one-time code is consumed, and a system with no desktop session at all must
// fall back to private files instead of dying on a raw dbus error.

func withDeviceStorageTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// The generic slots honor HA_NOVA_TEST_SECRET_DIR first — clear it so these
	// tests exercise the REAL keyring/file selection logic (go-keyring is mocked
	// package-wide by TestMain, so "keyring" here means the in-memory mock).
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	prevForced := deviceCredentialFileModeForced
	deviceCredentialFileModeForced = false
	// Keyring reads run deviceCredentialPreflight first; on Linux that inspects
	// the REAL DBus Secret Service, which is absent on headless CI. Stub it to a
	// no-op so keyring-mode reads reach the mock on every platform. Tests that
	// need a preflight/keyring failure override this again.
	prevPreflight := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return nil }
	t.Cleanup(func() {
		deviceCredentialFileModeForced = prevForced
		deviceCredentialPreflight = prevPreflight
	})
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
	if !deviceFileBackendMarkerExists() {
		t.Fatal("expected the probe to persist a file-backend marker for future processes")
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

func TestProbeShortCircuitsWhenFileBackendMarkerExists(t *testing.T) {
	withDeviceStorageTestHome(t)
	// An established headless install: the file-backend marker records the mode,
	// so the probe must NOT consult the keyring and reads self-select the file.
	cred := generateTestDeviceCredential(t)
	if err := writeDeviceFileBackendMarker(); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	if err := deviceSecretFileSet(deviceCredentialService, cred); err != nil {
		t.Fatalf("seed file credential: %v", err)
	}
	stubStorageCanaries(t, fmt.Errorf("canary must not run"))
	deviceStorageKeyringCanary = func() error { t.Fatal("keyring canary ran despite the file-backend marker"); return nil }

	probe, err := probeDeviceCredentialStorage()
	if err != nil || probe.mode != "file" {
		t.Fatalf("expected silent file mode, got mode=%q err=%v", probe.mode, err)
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != cred {
		t.Fatalf("file credential not readable: got=%q ok=%v err=%v", got, ok, err)
	}
}

func TestOrphanCredentialFileWithoutMarkerStaysOnKeyring(t *testing.T) {
	withDeviceStorageTestHome(t)
	// A credential file WITHOUT a marker is orphan residue (e.g. an aborted early
	// file attempt). It must not flip the install to file mode: the current slot
	// stays on the keyring, and the probe (keyring healthy) cleans the orphan.
	keyringCred := generateTestDeviceCredential(t)
	if err := keyring.Set(deviceCredentialService, secretUser(), keyringCred); err != nil {
		t.Fatalf("seed keyring current credential: %v", err)
	}
	orphan := "hanova-dev-v1.EEEEEEEEEEEEEEEEEEEEEE.FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"
	if err := deviceSecretFileSet(deviceCredentialService, orphan); err != nil {
		t.Fatalf("seed orphan current file: %v", err)
	}
	stubStorageCanaries(t, nil) // keyring healthy

	probe, err := probeDeviceCredentialStorage()
	if err != nil || probe.mode != "keyring" {
		t.Fatalf("expected keyring mode (no downgrade), got mode=%q err=%v", probe.mode, err)
	}
	// The orphan file is cleaned, and the current slot resolves to the keyring.
	if deviceSecretFileExists(deviceCredentialService) {
		t.Fatal("expected the probe to clear the orphan credential file on the keyring path")
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != keyringCred {
		t.Fatalf("keyring credential masked by orphan file: got=%q ok=%v err=%v", got, ok, err)
	}
}

func TestStalePendingFileWithoutMarkerDoesNotMaskKeyringCurrent(t *testing.T) {
	withDeviceStorageTestHome(t)
	// Desktop-paired install: the CURRENT credential lives in the (mocked) OS
	// keyring, with no file-backend marker. An interrupted attempt left a stale
	// PENDING file. Neither slot may switch to the file backend.
	keyringCred := generateTestDeviceCredential(t)
	if err := keyring.Set(deviceCredentialService, secretUser(), keyringCred); err != nil {
		t.Fatalf("seed keyring current credential: %v", err)
	}
	if err := deviceSecretFileSet(deviceCredentialPendingService,
		"hanova-dev-v1.CCCCCCCCCCCCCCCCCCCCCC.DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"); err != nil {
		t.Fatalf("seed stale pending file: %v", err)
	}

	// Current resolves to the keyring credential — not masked.
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != keyringCred {
		t.Fatalf("current credential masked by stale pending file: got=%q ok=%v err=%v", got, ok, err)
	}
	// Pending resolves to the keyring too (empty) — the stale file is orphan
	// residue on a keyring install and is ignored, never read.
	_, okP, errP := readPendingDeviceCredential()
	if errP != nil || okP {
		t.Fatalf("stale pending file was read despite keyring backend: ok=%v err=%v", okP, errP)
	}
}

func TestFileCanaryRejectsAnUnwritableExistingCredentialFile(t *testing.T) {
	withDeviceStorageTestHome(t)
	// Established file install (marker present) whose current credential file has
	// become non-writable (root-owned / 0400). The canary must catch the failed
	// OVERWRITE path, not just a fresh probe file — BEFORE any code is consumed.
	if err := writeDeviceFileBackendMarker(); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	if err := deviceSecretFileSet(deviceCredentialService, generateTestDeviceCredential(t)); err != nil {
		t.Fatalf("seed credential file: %v", err)
	}
	path, _ := deviceSecretFilePath(deviceCredentialService)
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod 0400: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	// Real file canary (not stubbed); keyring canary must not run for a file install.
	prevK := deviceStorageKeyringCanary
	deviceStorageKeyringCanary = func() error { t.Fatal("keyring canary must not run for a marked file install"); return nil }
	t.Cleanup(func() { deviceStorageKeyringCanary = prevK })
	_, err := probeDeviceCredentialStorage()
	if err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("expected an unwritable-file-store error from the overwrite check, got %v", err)
	}
}

func TestProbeFallsBackWhenNoSecretServiceProviderExists(t *testing.T) {
	withDeviceStorageTestHome(t)
	// "No Secret Service provider installed" (errDesktopKeyringUnavailable) is
	// the container/LXC signature, distinct from "no session" — both mean no
	// keyring EXISTS, so both fall back to files rather than erroring.
	stubStorageCanaries(t, fmt.Errorf("%w: org.freedesktop.secrets not provided", errDesktopKeyringUnavailable))
	probe, err := probeDeviceCredentialStorage()
	if err != nil || probe.mode != "file" {
		t.Fatalf("expected file fallback for a missing Secret Service provider, got mode=%q err=%v", probe.mode, err)
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
