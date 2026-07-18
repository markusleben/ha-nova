package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zalando/go-keyring"
)

// Storage-mode handling for the device-credential slots.
//
// Desktop systems keep device credentials in the OS keyring. Headless systems
// (containers, servers, LXCs — a core Home Assistant audience) have no Secret
// Service session at all; for them the credential lives in a private 0600 file
// under the config directory, mirroring the relay token's service-file mode.
//
// The backend is a SINGLE per-install decision recorded by an explicit marker
// file, NOT inferred from whether a credential file happens to exist. Inferring
// from credential files is fragile: a stale `.pending` file left by an aborted
// headless re-pair on a desktop must never flip the install to file mode (which
// would mask the real keyring credential and silently downgrade storage). The
// marker is written only when the probe commits to file mode, so every slot on
// an install resolves to the same backend.

const deviceCredentialProbeService = "ha-nova.device-credential.probe"
const deviceCredentialFileBackendMarker = ".file-backend"

// Process-local decision from the probe: route this run to the file backend even
// before the on-disk marker is consulted (belt-and-suspenders for the first run
// on a headless system, before the marker write).
var deviceCredentialFileModeForced = false

type deviceStorageProbe struct {
	// mode is "keyring" or "file" — informational for callers/tests.
	mode string
	// note is a user-facing sentence to print when the fallback engaged.
	note string
}

func deviceSecretFileDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(windowsAppDataDir(home), "ha-nova", "secrets"), nil
	}
	return filepath.Join(home, ".config", "ha-nova", "secrets"), nil
}

func deviceSecretFilePath(service string) (string, error) {
	dir, err := deviceSecretFileDir()
	if err != nil {
		return "", err
	}
	return testSecretPath(dir, service), nil
}

func deviceFileBackendMarkerPath() (string, error) {
	dir, err := deviceSecretFileDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, deviceCredentialFileBackendMarker), nil
}

func deviceFileBackendMarkerExists() bool {
	path, err := deviceFileBackendMarkerPath()
	if err != nil {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func writeDeviceFileBackendMarker() error {
	dir, err := deviceSecretFileDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, err := deviceFileBackendMarkerPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte("file\n"), 0o600)
}

// deviceSecretFileBacked reports whether THIS install stores device credentials
// in files. It is a single install-wide decision (marker or process-forced) so
// every slot resolves to the same backend — a leftover credential file for one
// slot can never redirect another slot.
func deviceSecretFileBacked() bool {
	return deviceCredentialFileModeForced || deviceFileBackendMarkerExists()
}

func deviceSecretFileExists(service string) bool {
	path, err := deviceSecretFilePath(service)
	if err != nil {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func deviceSecretFileGet(service string) (string, error) {
	path, err := deviceSecretFilePath(service)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errSecretNotFound
		}
		return "", err
	}
	return string(data), nil
}

func deviceSecretFileSet(service, value string) error {
	dir, err := deviceSecretFileDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Persisting the CURRENT (active) credential to a file is the moment this
	// install commits to the file backend, so the marker is written with it — but
	// ONLY on the FIRST commit (pending writes are provisional and never commit
	// the mode). On an already-committed file install the marker exists, so a
	// re-pair/resume just overwrites the credential: never rewrite the marker
	// there (a marker gone 0400 would otherwise fail and trigger a rollback that
	// deletes the freshly promoted, already-activated credential).
	if service == deviceCredentialService {
		path := testSecretPath(dir, service)
		if deviceFileBackendMarkerExists() {
			return os.WriteFile(path, []byte(value), 0o600)
		}
		// First commit: keep the invariant "current credential file exists IFF
		// marker exists" by writing the file, then the marker, and rolling the
		// file back if the marker cannot be created. Nothing valuable is lost —
		// there is no prior committed file credential on a first commit.
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			return err
		}
		if err := writeDeviceFileBackendMarker(); err != nil {
			_ = os.Remove(path)
			return err
		}
		return nil
	}
	return os.WriteFile(testSecretPath(dir, service), []byte(value), 0o600)
}

func deviceSecretFileDelete(service string) error {
	path, err := deviceSecretFilePath(service)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removeDeviceFileStorageResidue clears the file-backend marker and removes the
// secrets directory if it is now empty. Best-effort, purge-only: a leftover
// marker would otherwise make a fresh reinstall inherit file mode without
// re-probing. Also drops the in-process forced flag so the same run re-decides.
func removeDeviceFileStorageResidue() {
	if path, err := deviceFileBackendMarkerPath(); err == nil {
		_ = os.Remove(path)
	}
	if dir, err := deviceSecretFileDir(); err == nil {
		_ = os.Remove(dir) // removes only when empty
	}
	deviceCredentialFileModeForced = false
}

// Test seams: the canaries hit the real OS keyring / filesystem by default.
var deviceStorageKeyringCanary = keyringStorageCanary
var deviceStorageFileCanary = fileStorageCanary

// probeDeviceCredentialStorage verifies that a device credential CAN be stored
// before any pairing starts, so a broken backend never burns the owner's
// one-time code. On a headless system (no Secret Service at all) it switches
// this install to the private-file backend, records the marker, and says so; a
// PRESENT-but-locked/uninitialized desktop keyring is a hard error — no silent
// downgrade.
func probeDeviceCredentialStorage() (deviceStorageProbe, error) {
	if _, ok := testSecretDir(); ok {
		return deviceStorageProbe{mode: "file"}, nil
	}
	if deviceSecretFileBacked() {
		// Established file-mode install: prove the slots are writable NOW —
		// a read-only volume or a root-owned/0400 credential file discovered
		// after the fact would consume the one-time code with nothing storable.
		if err := deviceStorageFileCanary(); err != nil {
			return deviceStorageProbe{}, fmt.Errorf("this install keeps its device credential in a file, but the file store is not writable: %w", err)
		}
		return deviceStorageProbe{mode: "file"}, nil
	}

	keyringErr := deviceStorageKeyringCanary()
	if keyringErr == nil {
		// Committed to the keyring: drop any orphan credential files from an
		// aborted earlier file attempt so they can never be mistaken for state.
		_ = deviceSecretFileDelete(deviceCredentialService)
		_ = deviceSecretFileDelete(deviceCredentialPendingService)
		return deviceStorageProbe{mode: "keyring"}, nil
	}
	if errors.Is(keyringErr, errDesktopKeyringSessionUnavailable) || errors.Is(keyringErr, errDesktopKeyringUnavailable) {
		// No keyring is reachable here (no session bus, or no Secret Service
		// provider — typical for containers/servers/LXCs, but ALSO for SSH into a
		// desktop whose keyring still holds a credential). So force file mode for
		// THIS process only and do NOT persist the marker yet: the install-wide
		// switch happens only when a current credential is actually promoted to a
		// file (deviceSecretFileSet). A canceled pair/setup then leaves nothing
		// behind and can never mask or downgrade an existing keyring credential.
		if fileErr := deviceStorageFileCanary(); fileErr != nil {
			return deviceStorageProbe{}, fmt.Errorf("no desktop keyring (%v) and the file fallback failed: %w", keyringErr, fileErr)
		}
		deviceCredentialFileModeForced = true
		dir, _ := deviceSecretFileDir()
		return deviceStorageProbe{
			mode: "file",
			note: fmt.Sprintf("No desktop keyring reachable — the device credential will be stored in a private file (0600) under %s.", dir),
		}, nil
	}
	// Locked or uninitialized desktop keyring, permission problems, …: guide the
	// user instead of silently weakening storage on a desktop system.
	return deviceStorageProbe{}, keyringErr
}

func keyringStorageCanary() error {
	if err := deviceCredentialPreflight(); err != nil {
		return err
	}
	user := secretUser()
	if err := keyring.Set(deviceCredentialProbeService, user, "probe"); err != nil {
		return err
	}
	if _, err := keyring.Get(deviceCredentialProbeService, user); err != nil {
		return err
	}
	if err := keyring.Delete(deviceCredentialProbeService, user); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

// fileStorageCanary proves the file backend can actually store credentials: the
// secrets directory accepts a new file, AND every existing credential slot is
// overwritable (a root-owned or 0400 credential file would otherwise only fail
// at promotion, after the one-time code was already spent).
func fileStorageCanary() error {
	if err := deviceSecretFileSet(deviceCredentialProbeService, "probe"); err != nil {
		return err
	}
	if _, err := deviceSecretFileGet(deviceCredentialProbeService); err != nil {
		return err
	}
	if err := deviceSecretFileDelete(deviceCredentialProbeService); err != nil {
		return err
	}
	for _, service := range []string{deviceCredentialService, deviceCredentialPendingService} {
		path, err := deviceSecretFilePath(service)
		if err != nil {
			return err
		}
		if !deviceSecretFileExists(service) {
			continue
		}
		// O_WRONLY (no O_TRUNC) proves write permission without corrupting the
		// stored credential; close immediately.
		f, err := os.OpenFile(path, os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("existing credential file %s is not overwritable: %w", filepath.Base(path), err)
		}
		_ = f.Close()
	}
	return nil
}
