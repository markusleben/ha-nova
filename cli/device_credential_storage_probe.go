package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

// Test seams: the canaries hit the real OS keyring / filesystem by default.
var deviceStorageKeyringCanary = keyringStorageCanary
var deviceStorageFileCanary = fileStorageCanary

// deviceCredentialStorageViable reports whether a device credential can be
// stored here (OS keyring, or the headless file fallback). Used by the setup
// wizard to decide that a missing relay-token keyring must not abort the
// pairing path, which does not need the legacy token store.
func deviceCredentialStorageViable() bool {
	_, err := probeDeviceCredentialStorage()
	return err == nil
}

// probeDeviceCredentialStorage verifies that a device credential CAN be stored
// before any pairing starts, so a broken backend never burns the owner's
// one-time code. On a headless system (no Secret Service at all) it switches
// this install to the private-file backend, records the marker, and says so; a
// PRESENT-but-locked/uninitialized desktop keyring is a hard error — no silent
// downgrade.
func probeDeviceCredentialStorage() (deviceStorageProbe, error) {
	return probeDeviceCredentialStorageWithCanary(deviceStorageKeyringCanary)
}

func probeDeviceCredentialStorageWithPolicy(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) (deviceStorageProbe, error) {
	return probeDeviceCredentialStorageWithCanary(func() error {
		return keyringStorageCanaryWithPolicy(ctx, ui)
	})
}

func probeDeviceCredentialStorageWithCanary(
	keyringCanary func() error,
) (deviceStorageProbe, error) {
	if _, ok := testSecretDir(); ok {
		return deviceStorageProbe{mode: "file"}, nil
	}
	if deviceSecretFileBacked() {
		if err := deviceStorageFileCanary(); err != nil {
			return deviceStorageProbe{}, fmt.Errorf("this install keeps its device credential in a file, but the file store is not writable: %w", err)
		}
		return deviceStorageProbe{mode: "file"}, nil
	}

	keyringErr := keyringCanary()
	if keyringErr == nil {
		return deviceStorageProbe{mode: "keyring"}, nil
	}
	if errors.Is(keyringErr, errDesktopKeyringSessionUnavailable) ||
		errors.Is(keyringErr, errDesktopKeyringUnavailable) {
		if profile := activeServerProfile(); profile != defaultServerProfileName &&
			!deviceFileBackendMarkerExists() {
			return deviceStorageProbe{}, fmt.Errorf("no desktop keyring reachable here (%v), and this install has not switched to file storage; pairing profile %q would hide the default profile's keyring credential. Re-run with --credential-store=file to switch this install explicitly, or pair from the desktop session", keyringErr, profile)
		}
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
	if errors.Is(keyringErr, errDesktopKeyringLocked) ||
		errors.Is(keyringErr, errDesktopKeyringInitializationRequired) {
		return deviceStorageProbe{}, fmt.Errorf("%w\nIf no one ever unlocks a desktop session on this machine (VM, server, autologin): rerun setup with --service, or run: ha-nova pair --credential-store=file", keyringErr)
	}
	return deviceStorageProbe{}, keyringErr
}

func keyringStorageCanary() error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return keyringStorageCanaryWithPolicy(ctx, SecretStoreForbidUI)
}

func keyringStorageCanaryWithPolicy(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) error {
	if err := deviceCredentialPreflightWithContext(ctx); err != nil {
		return err
	}
	user := secretUser()
	if err := secretKeyringSetWithPolicy(
		ctx,
		deviceCredentialProbeService,
		user,
		"probe",
		ui,
	); err != nil {
		return err
	}
	followupUI := ui
	if ui == SecretStoreAllowUI {
		followupUI = SecretStoreForbidUI
	}
	if _, err := secretKeyringGetWithPolicy(
		ctx,
		deviceCredentialProbeService,
		user,
		followupUI,
	); err != nil {
		_ = secretKeyringDeleteWithPolicy(
			ctx,
			deviceCredentialProbeService,
			user,
			followupUI,
		)
		return err
	}
	if err := secretKeyringDeleteWithPolicy(
		ctx,
		deviceCredentialProbeService,
		user,
		followupUI,
	); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

// fileStorageCanary proves the file backend can actually store credentials.
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
	for _, service := range []string{
		activeDeviceCredentialService(),
		activeDeviceCredentialPendingService(),
	} {
		path, err := deviceSecretFilePath(service)
		if err != nil {
			return err
		}
		regular, err := canaryPathRegularOrAbsent(path)
		if err != nil {
			return err
		}
		if !regular {
			continue
		}
		file, err := os.OpenFile(path, os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("existing credential file %s is not overwritable: %w", filepath.Base(path), err)
		}
		_ = file.Close()
	}
	markerPath, err := deviceFileBackendMarkerPath()
	if err != nil {
		return err
	}
	regular, err := canaryPathRegularOrAbsent(markerPath)
	if err != nil {
		return err
	}
	if !regular {
		if err := writeDeviceFileBackendMarker(); err != nil {
			return fmt.Errorf("file-backend marker path is not writable: %w", err)
		}
		if err := removeDeviceResiduePath(markerPath); err != nil {
			return fmt.Errorf(
				"remove file-backend marker canary: %w",
				err,
			)
		}
	}
	return nil
}

func rollbackFailedDeviceFileBackendMarkerWrite() error {
	path, err := deviceFileBackendMarkerPath()
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("inspect failed file-backend marker write: %w", err)
	case !info.Mode().IsRegular():
		return nil
	default:
		return removeDeviceResiduePath(path)
	}
}

func canaryPathRegularOrAbsent(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	return true, nil
}
