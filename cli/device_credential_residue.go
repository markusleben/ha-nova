package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Profile-aware residue cleanup for the file-backed device-credential slots.
// Split from device_credential_storage.go (router/probe/canary) per the
// <~400 LOC file guideline.

var deviceResidueRemove = os.Remove
var deviceResidueReadDir = os.ReadDir
var deviceResidueLstat = os.Lstat

// isCurrentDeviceCredentialSlotService reports whether service names a CURRENT
// (active) credential slot of ANY profile — the writes that commit the file
// backend and lay down the machine-wide marker.
func isCurrentDeviceCredentialSlotService(service string) bool {
	if service == deviceCredentialService {
		return true
	}
	prefix := deviceCredentialService + "."
	if !strings.HasPrefix(service, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(service, prefix)
	if suffix == "" || suffix == "probe" || suffix == "pending" || strings.HasPrefix(suffix, "pending.") {
		return false
	}
	return true
}

// removeDeviceFileStorageResidueForProfile removes ONE profile's file-backed
// credential slots. The machine-wide marker (and the secrets dir) go only when
// NO profile's slot files remain — the marker describes the machine, and
// removing it while another server's files exist would break the invariant
// "credential file exists IFF marker exists" and strand that server's pairing.
// Slots are deleted DIRECTLY (by path), not through the marker-routed deleters:
// a headless pairing interrupted before promotion leaves a pending FILE with no
// marker, so routed deletes would go to the keyring and leave the orphan file
// (and a non-empty secrets dir) behind.
func removeDeviceFileStorageResidueForProfile(profile string) error {
	if _, err := resumeKeyringDeviceCredentialCleanup(); err != nil {
		return fmt.Errorf(
			"finish device credential migration cleanup before residue removal: %w",
			err,
		)
	}
	for _, service := range []string{
		deviceCredentialServiceForProfile(profile),
		deviceCredentialPendingServiceForProfile(profile),
	} {
		path, err := deviceSecretFilePath(service)
		if err != nil {
			return fmt.Errorf("resolve device credential residue %s: %w", service, err)
		}
		if err := removeDeviceResiduePath(path); err != nil {
			return fmt.Errorf("remove device credential residue %s: %w", service, err)
		}
	}
	remaining, err := deviceCredentialSlotFilesRemain()
	if err != nil {
		return err
	}
	if !remaining {
		return removeDeviceFileStorageMarkerAndDir()
	}
	return nil
}

// removeAllDeviceFileStorageResidue clears EVERY profile's slot files, the
// marker, and the (then empty) secrets dir. Full-purge only: a leftover marker
// would otherwise make a fresh reinstall inherit file mode without re-probing.
func removeAllDeviceFileStorageResidue() error {
	if _, err := resumeKeyringDeviceCredentialCleanup(); err != nil {
		return fmt.Errorf(
			"finish device credential migration cleanup before residue removal: %w",
			err,
		)
	}
	dir, err := deviceSecretFileDir()
	if err != nil {
		return fmt.Errorf("resolve device credential residue directory: %w", err)
	}
	entries, err := deviceResidueReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect device credential residue directory: %w", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), deviceCredentialService) {
			path := filepath.Join(dir, entry.Name())
			if err := removeDeviceResiduePath(path); err != nil {
				return fmt.Errorf(
					"remove device credential residue %s: %w",
					entry.Name(),
					err,
				)
			}
		}
	}
	remaining, err := deviceCredentialSlotFilesRemain()
	if err != nil {
		return err
	}
	if remaining {
		return fmt.Errorf("device credential residue remains after full cleanup")
	}
	return removeDeviceFileStorageMarkerAndDir()
}

func deviceCredentialSlotFilesRemain() (bool, error) {
	dir, err := deviceSecretFileDir()
	if err != nil {
		return false, fmt.Errorf(
			"resolve device credential residue directory: %w",
			err,
		)
	}
	entries, err := deviceResidueReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf(
			"inspect device credential residue directory: %w",
			err,
		)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, deviceCredentialService) && name != deviceCredentialProbeService {
			return true, nil
		}
	}
	return false, nil
}

// removeDeviceFileStorageMarkerAndDir drops the machine-wide backend marker and
// the secrets dir (only when empty), and resets the in-process flags so the
// same run re-decides the backend.
func removeDeviceFileStorageMarkerAndDir() error {
	markerPath, err := deviceFileBackendMarkerPath()
	if err != nil {
		return fmt.Errorf("resolve device credential storage marker: %w", err)
	}
	if _, exists, err := readKeyringDeviceCredentialCleanup(); err != nil {
		return fmt.Errorf(
			"inspect device credential migration cleanup checkpoint: %w",
			err,
		)
	} else if exists {
		return fmt.Errorf(
			"device credential migration cleanup is still pending",
		)
	}
	dir, err := deviceSecretFileDir()
	if err != nil {
		return fmt.Errorf("resolve device credential residue directory: %w", err)
	}
	entries, err := deviceResidueReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(
			"inspect device credential residue directory before marker removal: %w",
			err,
		)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(markerPath) {
			return fmt.Errorf(
				"device credential residue %s remains before marker removal",
				entry.Name(),
			)
		}
	}
	for _, target := range []struct {
		label string
		path  string
	}{
		{label: "storage marker", path: markerPath},
		{label: "residue directory", path: dir},
	} {
		if err := removeDeviceResiduePath(target.path); err != nil {
			return fmt.Errorf(
				"remove device credential %s: %w",
				target.label,
				err,
			)
		}
	}
	deviceCredentialFileModeForced = false
	deviceCredentialFileModeExplicit = false
	return nil
}

func removeDeviceResiduePath(path string) error {
	if err := deviceResidueRemove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := deviceResidueLstat(path); err == nil {
		return fmt.Errorf("path still exists after removal")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("verify path absence: %w", err)
	}
	return nil
}

// requireEmptyServerCredentialNamespaces keeps rename a metadata-only
// operation. Moving a live bearer would need a crash-recoverable old/new
// namespace transaction; until that exists, every routed and raw slot on both
// names must be readable and empty.
func requireEmptyServerCredentialNamespaces(oldName, newName string) error {
	for _, profile := range []string{oldName, newName} {
		for _, service := range []string{
			deviceCredentialServiceForProfile(profile),
			deviceCredentialPendingServiceForProfile(profile),
		} {
			value, exists, err := readCredentialSlot(service)
			if err != nil {
				return fmt.Errorf(
					"cannot prove credential slot %s is empty: %w",
					service,
					err,
				)
			}
			if exists || value != "" {
				return fmt.Errorf(
					"server profile %q has stored device credentials",
					profile,
				)
			}
			rawPath, err := deviceSecretFilePath(service)
			if err != nil {
				return err
			}
			if _, err := os.Lstat(rawPath); err == nil {
				return fmt.Errorf(
					"server profile %q has a raw device credential file",
					profile,
				)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf(
					"cannot inspect raw credential slot %s: %w",
					service,
					err,
				)
			}
		}
	}
	return nil
}
