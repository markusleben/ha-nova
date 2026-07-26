package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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

// readKeyringDeviceSecret reads a device slot from the OS keyring directly,
// bypassing the marker/file routing. Returns (value, true, nil) on a hit,
// ("", false, nil) when the keyring has no such entry, and an error when the
// keyring is unreachable (headless). Resume uses it to prefer a real keyring
// pending over an orphan .pending FILE from an aborted earlier headless attempt.
func readKeyringDeviceSecret(service string) (string, bool, error) {
	if dir, ok := testSecretDir(); ok {
		data, err := os.ReadFile(testSecretPath(dir, service))
		if err != nil {
			if os.IsNotExist(err) {
				return "", false, nil
			}
			return "", false, err
		}
		return strings.TrimSpace(string(data)), true, nil
	}
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	if err := deviceCredentialPreflightWithContext(
		ctx,
		SecretStoreForbidUI,
	); err != nil {
		return "", false, err
	}
	value, err := secretKeyringGetWithPolicy(
		ctx,
		service,
		secretUser(),
		SecretStoreForbidUI,
	)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(value), true, nil
}

func deleteKeyringDeviceSecret(service string) error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	if err := deviceCredentialPreflightWithContext(
		ctx,
		SecretStoreForbidUI,
	); err != nil {
		return err
	}
	err := secretKeyringDeleteWithPolicy(
		ctx,
		service,
		secretUser(),
		SecretStoreForbidUI,
	)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
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
	if isCurrentDeviceCredentialSlotService(service) {
		path := testSecretPath(dir, service)
		if deviceFileBackendMarkerExists() {
			return writeSecretFile0600(path, value)
		}
		// First commit: keep the invariant "current credential file exists IFF
		// marker exists" by writing the file, then the marker, and rolling the
		// file back if the marker cannot be created. Nothing valuable is lost —
		// there is no prior committed file credential on a first commit.
		if err := writeSecretFile0600(path, value); err != nil {
			return err
		}
		if err := writeDeviceFileBackendMarker(); err != nil {
			return fmt.Errorf(
				"persist file-backend marker: %w",
				errors.Join(
					err,
					removeDeviceResiduePath(path),
					rollbackFailedDeviceFileBackendMarkerWrite(),
				),
			)
		}
		return nil
	}
	return writeSecretFile0600(testSecretPath(dir, service), value)
}

// writeSecretFile0600 writes a device secret and ENFORCES 0600, even when the
// file already existed with looser permissions: os.WriteFile's mode applies only
// on create, so a re-pair overwriting a manually-repaired credential file could
// otherwise leave it readable by other local users.
func writeSecretFile0600(path, value string) error {
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
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
