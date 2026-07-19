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
		t.Fatal("expected the probe to force file mode for this process")
	}
	// The marker is NOT written at probe time — only when a current credential is
	// actually promoted to a file. A canceled pairing must leave nothing behind.
	if deviceFileBackendMarkerExists() {
		t.Fatal("probe must not persist the file-backend marker before a credential is stored")
	}

	// A pending write is provisional and still does not commit the mode.
	cred := generateTestDeviceCredential(t)
	if err := writePendingDeviceCredential(cred); err != nil {
		t.Fatalf("writePendingDeviceCredential in file mode: %v", err)
	}
	if deviceFileBackendMarkerExists() {
		t.Fatal("a pending write must not persist the file-backend marker")
	}

	// Promotion writes the CURRENT credential to a file — THAT commits the mode.
	if err := writeDeviceCredential(cred); err != nil {
		t.Fatalf("writeDeviceCredential in file mode: %v", err)
	}
	if !deviceFileBackendMarkerExists() {
		t.Fatal("expected the file-backend marker after a current credential is stored")
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
}

func TestCanceledFilePairDoesNotDowngradeAKeyringInstall(t *testing.T) {
	withDeviceStorageTestHome(t)
	// A desktop-paired install (credential in the keyring) is run from an SSH
	// shell with no session bus: the probe forces file mode for this process but
	// must NOT persist the marker. If the pairing is canceled before a current
	// credential is written, the keyring credential must remain the source of
	// truth in the next (desktop) process.
	keyringCred := generateTestDeviceCredential(t)
	if err := keyring.Set(deviceCredentialService, secretUser(), keyringCred); err != nil {
		t.Fatalf("seed keyring current credential: %v", err)
	}
	stubStorageCanaries(t, fmt.Errorf("%w: dbus-launch not found", errDesktopKeyringSessionUnavailable))
	probe, err := probeDeviceCredentialStorage()
	if err != nil || probe.mode != "file" {
		t.Fatalf("expected file mode for this process, got mode=%q err=%v", probe.mode, err)
	}
	// Pairing canceled here — no current credential written, so no marker.
	if deviceFileBackendMarkerExists() {
		t.Fatal("a canceled file pairing left a marker → a keyring install would be downgraded")
	}
	// A fresh process (forced flag reset) still reads the keyring credential.
	deviceCredentialFileModeForced = false
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != keyringCred {
		t.Fatalf("keyring credential lost after a canceled SSH pairing: got=%q ok=%v err=%v", got, ok, err)
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
	// Seed a raw orphan credential file WITHOUT a marker (an aborted early file
	// attempt); deviceSecretFileSet would itself write a marker, which is exactly
	// what must NOT be present here.
	seedRawSecretFile(t, deviceCredentialService,
		"hanova-dev-v1.EEEEEEEEEEEEEEEEEEEEEE.FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")
	if deviceFileBackendMarkerExists() {
		t.Fatal("test setup error: orphan seed must not create a marker")
	}
	stubStorageCanaries(t, nil) // keyring healthy

	probe, err := probeDeviceCredentialStorage()
	if err != nil || probe.mode != "keyring" {
		t.Fatalf("expected keyring mode (no downgrade), got mode=%q err=%v", probe.mode, err)
	}
	// The orphan file is harmless (never read without a marker) and must NOT be
	// deleted — a pending file could be an interrupted headless pairing awaiting
	// resume. The current slot still resolves to the keyring credential.
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

func TestRepairOnFileInstallDoesNotDeleteCredentialWhenMarkerIsReadOnly(t *testing.T) {
	withDeviceStorageTestHome(t)
	deviceCredentialFileModeForced = true // headless install: writes go to the file backend
	// Established file install: marker + current credential present. The marker
	// has become read-only (0400). A re-pair overwrites the current credential;
	// it must NOT try to rewrite the marker (which would fail and roll the
	// freshly promoted, already-activated credential back to nothing).
	first := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(first); err != nil { // first commit writes marker + file
		t.Fatalf("seed file install: %v", err)
	}
	if !deviceFileBackendMarkerExists() {
		t.Fatal("seed did not commit file mode (marker missing)")
	}
	markerPath, _ := deviceFileBackendMarkerPath()
	if err := os.Chmod(markerPath, 0o400); err != nil {
		t.Fatalf("chmod marker 0400: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(markerPath, 0o600) })

	repaired := "hanova-dev-v1.GGGGGGGGGGGGGGGGGGGGGG.HHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHHH"
	if err := writeDeviceCredential(repaired); err != nil {
		t.Fatalf("re-pair overwrite must succeed without touching the marker: %v", err)
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != repaired {
		t.Fatalf("re-paired credential lost: got=%q ok=%v err=%v", got, ok, err)
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

func TestResumeFinishesAFileModePendingEvenWhenKeyringNowWorks(t *testing.T) {
	withDeviceStorageTestHome(t)
	// A headless pairing was interrupted after writing the pending FILE (no
	// marker yet) and saving the pending endpoint, but before promotion. Even if
	// the keyring is now usable, resume must finish it IN FILE MODE — reading its
	// own pending file, not rerouting to (or being cleaned by) the keyring.
	pendingCred := generateTestDeviceCredential(t)
	if err := deviceSecretFileSet(deviceCredentialPendingService, pendingCred); err != nil {
		t.Fatalf("seed pending file: %v", err)
	}
	if deviceFileBackendMarkerExists() {
		t.Fatal("test setup error: a pending write must not create a marker")
	}

	origActivate := activateDeviceV1ForPairing
	activated := ""
	activateDeviceV1ForPairing = func(_, _, cred string) error { activated = cred; return nil }
	t.Cleanup(func() { activateDeviceV1ForPairing = origActivate })

	cfg := runtimeConfig{PendingSecureBaseURL: "https://relay:8792", PendingSpkiPin: "PIN"}
	resumed, err := resumePendingActivation(&cfg, func(*runtimeConfig) error { return nil })
	if err != nil || !resumed {
		t.Fatalf("resume failed: resumed=%v err=%v", resumed, err)
	}
	if activated != pendingCred {
		t.Fatalf("resume activated the wrong credential: %q", activated)
	}
	// Promotion completed in file mode: current file + marker exist, pending gone.
	if !deviceFileBackendMarkerExists() {
		t.Fatal("expected the file-backend marker after file-mode promotion")
	}
	if deviceSecretFileExists(deviceCredentialPendingService) {
		t.Fatal("pending file not cleared after promotion")
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != pendingCred {
		t.Fatalf("current credential not readable from file after resume: got=%q ok=%v err=%v", got, ok, err)
	}
	if cfg.RelaySecureBaseURL != "https://relay:8792" || cfg.PendingSecureBaseURL != "" {
		t.Fatalf("endpoints not finalized: live=%q pending=%q", cfg.RelaySecureBaseURL, cfg.PendingSecureBaseURL)
	}
}

func TestResumePrefersKeyringPendingOverOrphanFile(t *testing.T) {
	withDeviceStorageTestHome(t)
	// A desktop re-pair was interrupted: its pending lives in the keyring. An
	// orphan .pending FILE from an aborted earlier headless attempt also exists
	// (the probe no longer deletes it). Resume must activate the REAL keyring
	// pending, not the stale file — otherwise it would spend another pairing code.
	keyringPending := generateTestDeviceCredential(t)
	if err := keyring.Set(deviceCredentialPendingService, secretUser(), keyringPending); err != nil {
		t.Fatalf("seed keyring pending: %v", err)
	}
	orphanFile := "hanova-dev-v1.IIIIIIIIIIIIIIIIIIIIII.JJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJJ"
	seedRawSecretFile(t, deviceCredentialPendingService, orphanFile)

	origActivate := activateDeviceV1ForPairing
	activated := ""
	activateDeviceV1ForPairing = func(_, _, cred string) error { activated = cred; return nil }
	t.Cleanup(func() { activateDeviceV1ForPairing = origActivate })

	cfg := runtimeConfig{PendingSecureBaseURL: "https://relay:8792", PendingSpkiPin: "PIN"}
	resumed, err := resumePendingActivation(&cfg, func(*runtimeConfig) error { return nil })
	if err != nil || !resumed {
		t.Fatalf("resume failed: resumed=%v err=%v", resumed, err)
	}
	if activated != keyringPending {
		t.Fatalf("resume activated the stale orphan file instead of the keyring pending: %q", activated)
	}
	// Keyring-mode promotion: no file marker; current credential is in the keyring.
	if deviceFileBackendMarkerExists() {
		t.Fatal("keyring-mode resume must not write a file-backend marker")
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != keyringPending {
		t.Fatalf("current credential not the promoted keyring pending: got=%q ok=%v err=%v", got, ok, err)
	}
}

func TestResumeRefusesFileFallbackWhenKeyringIsLocked(t *testing.T) {
	withDeviceStorageTestHome(t)
	// A desktop whose Secret Service is LOCKED: the real keyring pending/current
	// cannot be checked. An orphan .pending file exists. Resume must NOT activate
	// and promote that stale file (a silent downgrade) — it surfaces the error.
	orphanFile := "hanova-dev-v1.KKKKKKKKKKKKKKKKKKKKKK.LLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLLL"
	seedRawSecretFile(t, deviceCredentialPendingService, orphanFile)

	// Force the keyring read to report a LOCKED backend (present but unreadable),
	// distinct from the headless "session/provider unavailable" classes.
	origPreflight := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return desktopKeyringLockedError("default collection is locked") }
	origActivate := activateDeviceV1ForPairing
	activateDeviceV1ForPairing = func(_, _, _ string) error {
		t.Fatal("resume must not activate a credential when the keyring is locked")
		return nil
	}
	t.Cleanup(func() {
		deviceCredentialPreflight = origPreflight
		activateDeviceV1ForPairing = origActivate
	})

	cfg := runtimeConfig{PendingSecureBaseURL: "https://relay:8792", PendingSpkiPin: "PIN"}
	resumed, err := resumePendingActivation(&cfg, func(*runtimeConfig) error { return nil })
	if resumed || err == nil {
		t.Fatalf("expected resume to fail-safe on a locked keyring, got resumed=%v err=%v", resumed, err)
	}
	if deviceFileBackendMarkerExists() {
		t.Fatal("a locked-keyring resume must not commit file mode (downgrade)")
	}
	if !deviceSecretFileExists(deviceCredentialPendingService) {
		t.Fatal("the orphan pending file must be left intact, not promoted/removed")
	}
}

func TestResumeFileBackedInstallSkipsKeyringEvenWhenLocked(t *testing.T) {
	withDeviceStorageTestHome(t)
	// An established file-backed install (marker present) crashed with a pending
	// file, then runs where Secret Service exists but is LOCKED. Resume must read
	// the file pending and promote it — the keyring must not be probed at all.
	if err := writeDeviceFileBackendMarker(); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	pendingCred := generateTestDeviceCredential(t)
	if err := deviceSecretFileSet(deviceCredentialPendingService, pendingCred); err != nil {
		t.Fatalf("seed pending file: %v", err)
	}
	// If resume wrongly probed the keyring, this locked preflight would abort it.
	origPreflight := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return desktopKeyringLockedError("locked; must not be consulted") }
	origActivate := activateDeviceV1ForPairing
	activated := ""
	activateDeviceV1ForPairing = func(_, _, cred string) error { activated = cred; return nil }
	t.Cleanup(func() {
		deviceCredentialPreflight = origPreflight
		activateDeviceV1ForPairing = origActivate
	})

	cfg := runtimeConfig{PendingSecureBaseURL: "https://relay:8792", PendingSpkiPin: "PIN"}
	resumed, err := resumePendingActivation(&cfg, func(*runtimeConfig) error { return nil })
	if err != nil || !resumed {
		t.Fatalf("file-backed resume must ignore a locked keyring: resumed=%v err=%v", resumed, err)
	}
	if activated != pendingCred {
		t.Fatalf("resume activated the wrong credential: %q", activated)
	}
	if deviceSecretFileExists(deviceCredentialPendingService) {
		t.Fatal("pending file not cleared after promotion")
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != pendingCred {
		t.Fatalf("current credential not the promoted file pending: got=%q ok=%v err=%v", got, ok, err)
	}
}

func TestResumeSurfacesMalformedKeyringPending(t *testing.T) {
	withDeviceStorageTestHome(t)
	// The keyring pending slot exists but is corrupted/partial. Resume must
	// surface that, not silently activate/promote an orphan .pending file behind it.
	if err := keyring.Set(deviceCredentialPendingService, secretUser(), "not-a-valid-credential"); err != nil {
		t.Fatalf("seed malformed keyring pending: %v", err)
	}
	seedRawSecretFile(t, deviceCredentialPendingService,
		"hanova-dev-v1.MMMMMMMMMMMMMMMMMMMMMM.NNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNNN")

	origActivate := activateDeviceV1ForPairing
	activateDeviceV1ForPairing = func(_, _, _ string) error {
		t.Fatal("must not activate anything when the keyring pending is malformed")
		return nil
	}
	t.Cleanup(func() { activateDeviceV1ForPairing = origActivate })

	cfg := runtimeConfig{PendingSecureBaseURL: "https://relay:8792", PendingSpkiPin: "PIN"}
	resumed, err := resumePendingActivation(&cfg, func(*runtimeConfig) error { return nil })
	if resumed || err == nil {
		t.Fatalf("expected resume to surface the malformed keyring pending, got resumed=%v err=%v", resumed, err)
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("expected a malformed-credential error, got: %v", err)
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

// seedRawSecretFile writes a credential file directly, bypassing the marker
// side effect of deviceSecretFileSet — used to stage orphan/no-marker residue.
func seedRawSecretFile(t *testing.T, service, value string) {
	t.Helper()
	dir, err := deviceSecretFileDir()
	if err != nil {
		t.Fatalf("secret dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}
	if err := os.WriteFile(testSecretPath(dir, service), []byte(value), 0o600); err != nil {
		t.Fatalf("seed raw secret file: %v", err)
	}
}
