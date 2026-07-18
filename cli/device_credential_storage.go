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
		// No usable keyring EXISTS on this system (no session bus, or no Secret
		// Service provider installed — typical for containers/servers/LXCs).
		// There is nothing a keyring could protect here, so fall back to a
		// private file and say so. A keyring that exists but needs the user
		// (locked / uninitialized) is handled below and never downgrades.
		if fileErr := deviceStorageFileCanary(); fileErr != nil {
			return deviceStorageProbe{}, fmt.Errorf("no desktop keyring (%v) and the file fallback failed: %w", keyringErr, fileErr)
		}
		if markerErr := writeDeviceFileBackendMarker(); markerErr != nil {
			return deviceStorageProbe{}, fmt.Errorf("no desktop keyring and could not record file-backend mode: %w", markerErr)
		}
		deviceCredentialFileModeForced = true
		dir, _ := deviceSecretFileDir()
		return deviceStorageProbe{
			mode: "file",
			note: fmt.Sprintf("No desktop keyring on this system — the device credential will be stored in a private file (0600) under %s.", dir),
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
