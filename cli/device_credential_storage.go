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
// The mode is decided ONCE per pairing by probeDeviceCredentialStorage — BEFORE
// the one-time code is consumed — and is self-describing afterwards: reads use
// the file whenever it exists, so an install paired headless keeps working
// without any marker or config plumbing.

const deviceCredentialProbeService = "ha-nova.device-credential.probe"

// Process-local decision from the probe: route new writes to the file backend.
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

func deviceSecretFileExists(service string) bool {
	path, err := deviceSecretFilePath(service)
	if err != nil {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// deviceSecretFileBacked reports whether THIS slot lives in the file backend:
// its own file exists, or the probe forced file mode for this process. The
// decision is strictly per slot — a stale pending file left by an interrupted
// headless re-pair (e.g. pairing over SSH into a desktop machine) must never
// redirect reads of the CURRENT slot away from a valid keyring credential.
func deviceSecretFileBacked(service string) bool {
	return deviceCredentialFileModeForced || deviceSecretFileExists(service)
}

// anyDeviceSecretFileExists reports whether any real credential slot already
// lives in the file backend (the probe service never counts).
func anyDeviceSecretFileExists() bool {
	return deviceSecretFileExists(deviceCredentialService) || deviceSecretFileExists(deviceCredentialPendingService)
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

// Test seams: the canaries hit the real OS keyring / filesystem by default.
var deviceStorageKeyringCanary = keyringStorageCanary
var deviceStorageFileCanary = fileStorageCanary

// probeDeviceCredentialStorage verifies that a device credential CAN be stored
// before any pairing starts, so a broken backend never burns the owner's
// one-time code. On a headless system (no Secret Service session at all) it
// switches this install to the private-file backend and says so; a PRESENT but
// locked/uninitialized desktop keyring is a hard error — no silent downgrade.
func probeDeviceCredentialStorage() (deviceStorageProbe, error) {
	if _, ok := testSecretDir(); ok {
		return deviceStorageProbe{mode: "file"}, nil
	}
	if deviceCredentialFileModeForced || anyDeviceSecretFileExists() {
		// Existing file-mode install: still prove the files are writable NOW —
		// a read-only volume or chmod-ed directory discovered after the fact
		// would consume the one-time code with nothing storable.
		if fileErr := deviceStorageFileCanary(); fileErr != nil {
			return deviceStorageProbe{}, fmt.Errorf("this install keeps its device credential in a file, but the file store is not writable: %w", fileErr)
		}
		return deviceStorageProbe{mode: "file"}, nil
	}

	keyringErr := deviceStorageKeyringCanary()
	if keyringErr == nil {
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

func fileStorageCanary() error {
	if err := deviceSecretFileSet(deviceCredentialProbeService, "probe"); err != nil {
		return err
	}
	if _, err := deviceSecretFileGet(deviceCredentialProbeService); err != nil {
		return err
	}
	return deviceSecretFileDelete(deviceCredentialProbeService)
}
