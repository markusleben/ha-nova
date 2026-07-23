package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zalando/go-keyring"
)

// Backend-flip and migration coverage for the explicit file-backend opt-ins
// (`setup --service`, `pair --credential-store=file`): interrupted-activation
// durability, no-flip-before-durable-endpoint, and the keyring→file credential
// migration in all its states. Split from keyring_locked_ux_test.go per the
// <~400 LOC file guideline. Shares resetKeyringDeviceSlots and the storage
// test seams from that pack.

func TestExplicitFileOptInSurvivesInterruptedActivation(t *testing.T) {
	// Codex P2 on #388: the explicit opt-in force is process-local. A crash
	// between activation and promotion must leave the install file-routed —
	// otherwise the next run probes the locked keyring again and strands the
	// activated pairing (one-time code burned).
	withDeviceStorageTestHome(t)
	stubStorageCanaries(t, desktopKeyringLockedError("default Secret Service collection is locked"))

	validCred := generateTestDeviceCredential(t)
	origPair, origActivate := pairDeviceV1ForPairing, activateDeviceV1ForPairing
	pairDeviceV1ForPairing = func(_ *http.Client, _, _ string, _ deviceMetadata) (*provisionedCredential, error) {
		return &provisionedCredential{DeviceID: "dev-new", Credential: validCred, SpkiPin: "PIN", SecurePort: 8792}, nil
	}
	activateDeviceV1ForPairing = func(_, _, _ string) error {
		return errors.New("interrupted before promotion")
	}
	t.Cleanup(func() {
		pairDeviceV1ForPairing = origPair
		activateDeviceV1ForPairing = origActivate
	})

	cfgFile := filepath.Join(t.TempDir(), "config.json")
	cfg := runtimeConfig{RelayBaseURL: "http://ha:8791"}
	saveCfg := func(c *runtimeConfig) error {
		return saveConfig(runtimePaths{ConfigFile: cfgFile}, *c)
	}

	forceDeviceCredentialFileMode() // the explicit opt-in
	if _, err := runSecurePairing("http://ha:8791", "123456", &cfg, saveCfg, defaultPairingClientInfo()); err == nil {
		t.Fatal("activation was stubbed to fail")
	}
	if !deviceFileBackendMarkerExists() {
		t.Fatal("explicit opt-in must persist the marker at pending-write time so the interrupted pairing stays resumable")
	}
	if got, ok, err := readPendingDeviceCredential(); err != nil || !ok || got != validCred {
		t.Fatalf("pending credential must stay file-readable after the interruption: ok=%v err=%v", ok, err)
	}
}

func TestExplicitFileOptInDoesNotFlipBackendWhenEndpointSaveFails(t *testing.T) {
	// Codex P2 on #388 (round 3): the marker must not land before the pending
	// endpoint is durable — a crash in that window would flip the install to
	// files with nothing resumable and mask a still-valid keyring credential.
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	keyringCred := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(keyringCred); err != nil {
		t.Fatalf("seed keyring credential: %v", err)
	}

	newCred := generateTestDeviceCredential(t)
	origPair, origActivate := pairDeviceV1ForPairing, activateDeviceV1ForPairing
	pairDeviceV1ForPairing = func(_ *http.Client, _, _ string, _ deviceMetadata) (*provisionedCredential, error) {
		return &provisionedCredential{DeviceID: "dev-new", Credential: newCred, SpkiPin: "PIN", SecurePort: 8792}, nil
	}
	activateDeviceV1ForPairing = func(_, _, _ string) error { return nil }
	t.Cleanup(func() {
		pairDeviceV1ForPairing = origPair
		activateDeviceV1ForPairing = origActivate
	})

	cfg := runtimeConfig{RelayBaseURL: "http://ha:8791", ClientInstallID: "inst-test"}
	saveCfg := func(_ *runtimeConfig) error { return errors.New("disk full") }

	forceDeviceCredentialFileMode()
	if _, err := runSecurePairing("http://ha:8791", "123456", &cfg, saveCfg, defaultPairingClientInfo()); err == nil {
		t.Fatal("endpoint save was stubbed to fail")
	}
	if deviceFileBackendMarkerExists() {
		t.Fatal("marker must not persist before the pending endpoint is durable")
	}
	// A fresh process (flags reset) still reads the untouched keyring credential.
	deviceCredentialFileModeForced = false
	deviceCredentialFileModeExplicit = false
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != keyringCred {
		t.Fatalf("keyring credential masked after failed endpoint save: got ok=%v err=%v", ok, err)
	}
}

func TestRunPairCommandCredentialStoreFileMigratesKeyringCredential(t *testing.T) {
	// `pair --credential-store=file` on a desktop install with a readable
	// keyring pairing takes the credential along before the flip — the install
	// stays continuously paired even if the new pairing is interrupted.
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	keyringCred := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(keyringCred); err != nil {
		t.Fatalf("seed keyring credential: %v", err)
	}
	configFile := filepath.Join(t.TempDir(), "config.json")
	paths := runtimePaths{ConfigDir: filepath.Dir(configFile), ConfigFile: configFile}
	if err := saveConfig(paths, runtimeConfig{RelayBaseURL: "http://ha:8791"}); err != nil {
		t.Fatal(err)
	}
	orig := runSecurePairingForPairCmd
	runSecurePairingForPairCmd = func(_, _ string, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo) (string, error) {
		return "dev-1", nil
	}
	defer func() { runSecurePairingForPairCmd = orig }()

	if rc := runPairCommand(paths, []string{"--code", "123456", "--credential-store=file"}); rc != 0 {
		t.Fatalf("rc=%d, want 0", rc)
	}
	if !deviceFileBackendMarkerExists() {
		t.Fatal("migration must commit the file-backend marker")
	}
	got, ok, err := readDeviceCredential()
	if err != nil || !ok || got != keyringCred {
		t.Fatalf("migrated credential unreadable: ok=%v err=%v", ok, err)
	}
	if _, err := keyring.Get(deviceCredentialService, secretUser()); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("keyring copy must be removed after migration, got err=%v", err)
	}
}

func TestHeadlessAutoFallbackStaysUnmarkedOnInterruptedActivation(t *testing.T) {
	// Counterpart: the AUTO-detected headless fallback keeps its stricter
	// contract — nothing persists before promotion. It self-heals on the next
	// run because the probe detects the missing keyring again.
	withDeviceStorageTestHome(t)
	stubStorageCanaries(t, fmt.Errorf("%w: no session bus", errDesktopKeyringSessionUnavailable))

	validCred := generateTestDeviceCredential(t)
	origPair, origActivate := pairDeviceV1ForPairing, activateDeviceV1ForPairing
	pairDeviceV1ForPairing = func(_ *http.Client, _, _ string, _ deviceMetadata) (*provisionedCredential, error) {
		return &provisionedCredential{DeviceID: "dev-new", Credential: validCred, SpkiPin: "PIN", SecurePort: 8792}, nil
	}
	activateDeviceV1ForPairing = func(_, _, _ string) error {
		return errors.New("interrupted before promotion")
	}
	t.Cleanup(func() {
		pairDeviceV1ForPairing = origPair
		activateDeviceV1ForPairing = origActivate
	})

	cfgFile := filepath.Join(t.TempDir(), "config.json")
	cfg := runtimeConfig{RelayBaseURL: "http://ha:8791"}
	saveCfg := func(c *runtimeConfig) error {
		return saveConfig(runtimePaths{ConfigFile: cfgFile}, *c)
	}

	if _, err := runSecurePairing("http://ha:8791", "123456", &cfg, saveCfg, defaultPairingClientInfo()); err == nil {
		t.Fatal("activation was stubbed to fail")
	}
	if deviceFileBackendMarkerExists() {
		t.Fatal("auto-detected headless fallback must not persist the marker before promotion")
	}
}

func TestMigrateServiceDeviceCredentialToFile(t *testing.T) {
	// Codex P2 on #388: a `setup --service` re-run with a healthy keyring
	// pairing short-circuits to verify and never reaches the pairing-stage
	// force — the credential must therefore migrate to the file backend BEFORE
	// the assessment reads, or a later locked-keyring session still cannot use
	// this install.
	t.Run("moves current and pending keyring credentials and commits the marker", func(t *testing.T) {
		withDeviceStorageTestHome(t)
		resetKeyringDeviceSlots(t)
		current := generateTestDeviceCredential(t)
		pending := generateTestDeviceCredential(t)
		if err := writeDeviceCredential(current); err != nil {
			t.Fatalf("seed keyring credential: %v", err)
		}
		if err := writePendingDeviceCredential(pending); err != nil {
			t.Fatalf("seed pending keyring credential: %v", err)
		}
		if deviceFileBackendMarkerExists() {
			t.Fatal("precondition: install must start on the keyring backend")
		}

		migrated, err := migrateKeyringDeviceCredentialToFile()
		if err != nil || !migrated {
			t.Fatalf("expected the migration to run: migrated=%v err=%v", migrated, err)
		}
		if !deviceFileBackendMarkerExists() {
			t.Fatal("migration must commit the file-backend marker")
		}
		got, ok, err := readDeviceCredential()
		if err != nil || !ok || got != current {
			t.Fatalf("current credential after migration: got %q ok=%v err=%v", got, ok, err)
		}
		gotPending, ok, err := readPendingDeviceCredential()
		if err != nil || !ok || gotPending != pending {
			t.Fatalf("pending credential after migration: got %q ok=%v err=%v", gotPending, ok, err)
		}
		if _, ok, _ := readPendingKeyringSlotDirect(); ok {
			t.Fatal("keyring copy of the pending credential must be removed after migration")
		}
		if _, err := keyring.Get(deviceCredentialService, secretUser()); !errors.Is(err, keyring.ErrNotFound) {
			t.Fatalf("keyring copy of the current credential must be removed, got err=%v", err)
		}
	})

	t.Run("moves a pending-only interrupted first pairing and commits the flip", func(t *testing.T) {
		// Codex P2 on #388 (round 6): an interrupted FIRST pairing leaves only a
		// pending keyring credential. Without migration, interactive resume would
		// promote it back into the desktop keyring before service mode forces
		// files — the service contract would silently fail again.
		withDeviceStorageTestHome(t)
		resetKeyringDeviceSlots(t)
		pending := generateTestDeviceCredential(t)
		if err := writePendingDeviceCredential(pending); err != nil {
			t.Fatal(err)
		}

		migrated, err := migrateKeyringDeviceCredentialToFile()
		if err != nil || !migrated {
			t.Fatalf("pending-only migration must run: migrated=%v err=%v", migrated, err)
		}
		if !deviceFileBackendMarkerExists() {
			t.Fatal("pending-only migration must commit the marker so resume stays file-backed")
		}
		got, ok, readErr := readPendingDeviceCredential()
		if readErr != nil || !ok || got != pending {
			t.Fatalf("pending credential must be file-readable after migration: ok=%v err=%v", ok, readErr)
		}
		if _, pok, _ := readPendingKeyringSlotDirect(); pok {
			t.Fatal("keyring pending copy must be removed after migration")
		}
		if _, cok, _ := readDeviceCredential(); cok {
			t.Fatal("no current credential must appear out of thin air")
		}
	})

	t.Run("no-op without a readable keyring credential", func(t *testing.T) {
		withDeviceStorageTestHome(t)
		if migrated, err := migrateKeyringDeviceCredentialToFile(); err != nil || migrated {
			t.Fatalf("nothing to migrate — must be a no-op: migrated=%v err=%v", migrated, err)
		}
		if deviceFileBackendMarkerExists() {
			t.Fatal("a no-op migration must not commit the marker")
		}
	})

	t.Run("no-op when the keyring is locked", func(t *testing.T) {
		withDeviceStorageTestHome(t)
		prev := deviceCredentialPreflight
		deviceCredentialPreflight = func() error {
			return desktopKeyringLockedError("default Secret Service collection is locked")
		}
		t.Cleanup(func() { deviceCredentialPreflight = prev })
		if migrated, err := migrateKeyringDeviceCredentialToFile(); err != nil || migrated {
			t.Fatalf("locked keyring — must be a no-op: migrated=%v err=%v", migrated, err)
		}
		if deviceFileBackendMarkerExists() {
			t.Fatal("a locked-keyring no-op must not commit the marker")
		}
	})

	t.Run("no-op on an established file-backend install", func(t *testing.T) {
		withDeviceStorageTestHome(t)
		if err := writeDeviceFileBackendMarker(); err != nil {
			t.Fatal(err)
		}
		if migrated, err := migrateKeyringDeviceCredentialToFile(); err != nil || migrated {
			t.Fatalf("file-backed install — must be a no-op: migrated=%v err=%v", migrated, err)
		}
	})

	t.Run("aborts before the flip when the pending keyring slot is unreadable", func(t *testing.T) {
		// Codex P2 on #388 (round 5): an unreadable/malformed pending slot must
		// abort BEFORE the marker commits — flipping anyway would hide a pending
		// credential that activation may already have made the live one.
		withDeviceStorageTestHome(t)
		resetKeyringDeviceSlots(t)
		cred := generateTestDeviceCredential(t)
		if err := writeDeviceCredential(cred); err != nil {
			t.Fatal(err)
		}
		if err := keyring.Set(deviceCredentialPendingService, secretUser(), "garbage-not-a-credential"); err != nil {
			t.Fatal(err)
		}

		migrated, err := migrateKeyringDeviceCredentialToFile()
		if err == nil || migrated {
			t.Fatalf("unreadable pending slot must abort the migration: migrated=%v err=%v", migrated, err)
		}
		if deviceFileBackendMarkerExists() {
			t.Fatal("aborted migration must not leave a marker")
		}
		got, ok, readErr := readDeviceCredential()
		if readErr != nil || !ok || got != cred {
			t.Fatalf("keyring credential must remain readable after aborted migration: ok=%v err=%v", ok, readErr)
		}
	})

	t.Run("fails loudly when the file store is unwritable", func(t *testing.T) {
		// Codex P2 on #388 (round 4): a failed file WRITE must not look like
		// "nothing to migrate" — the service opt-in would silently leave the
		// credential in the desktop keyring.
		if runtime.GOOS == "windows" {
			t.Skip("unix permission semantics")
		}
		home := withDeviceStorageTestHome(t)
		resetKeyringDeviceSlots(t)
		cred := generateTestDeviceCredential(t)
		if err := writeDeviceCredential(cred); err != nil {
			t.Fatal(err)
		}
		blocked := filepath.Join(home, ".config", "ha-nova")
		if err := os.MkdirAll(blocked, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(blocked, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })

		migrated, err := migrateKeyringDeviceCredentialToFile()
		if err == nil || migrated {
			t.Fatalf("unwritable file store must fail loudly: migrated=%v err=%v", migrated, err)
		}
		if deviceFileBackendMarkerExists() {
			t.Fatal("failed migration must not leave a marker")
		}
		got, ok, readErr := readDeviceCredential()
		if readErr != nil || !ok || got != cred {
			t.Fatalf("keyring credential must remain readable after failed migration: ok=%v err=%v", ok, readErr)
		}
	})
}
