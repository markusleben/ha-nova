package main

import (
	"errors"
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

// stageServerCredentialSlotMove moves a profile's credential slots (current +
// pending) to a new profile name for `ha-nova server rename`. Two layers:
// the routed slots (keyring, or files behind the marker) AND the raw file
// slots — an interrupted explicit file pairing leaves a pending FILE before
// the machine-wide marker exists, which the routed read would miss. Raw files
// move via os.Rename, never through deviceSecretFileSet (its first current
// commit would write the marker as a side effect). Any failure rolls both
// layers back; commit deletes the old routed slots.
func stageServerCredentialSlotMove(oldName, newName string) (rollback func(), commit func(), err error) {
	slots := [][2]string{
		{deviceCredentialServiceForProfile(oldName), deviceCredentialServiceForProfile(newName)},
		{deviceCredentialPendingServiceForProfile(oldName), deviceCredentialPendingServiceForProfile(newName)},
	}
	var written, obsolete []string
	var movedFiles [][2]string
	markerlessSourceExists := false
	if !deviceFileBackendMarkerExists() {
		for _, pair := range slots {
			oldPath, pathErr := deviceSecretFilePath(pair[0])
			if pathErr != nil {
				continue
			}
			if info, statErr := os.Lstat(oldPath); statErr == nil &&
				info.Mode().IsRegular() {
				markerlessSourceExists = true
				break
			}
		}
	}
	rollback = func() {
		for _, service := range written {
			_ = secretDelete(service)
		}
		for _, pair := range movedFiles {
			_ = os.Rename(pair[1], pair[0])
		}
	}
	for _, pair := range slots {
		value, exists, readErr := readCredentialSlot(pair[1])
		if readErr != nil {
			unreachable := errors.Is(
				readErr,
				errDesktopKeyringSessionUnavailable,
			) || errors.Is(readErr, errDesktopKeyringUnavailable)
			if unreachable && markerlessSourceExists {
				continue
			}
			return nil, nil, fmt.Errorf(
				"cannot prove destination credential slot %s is empty: %w",
				pair[1],
				readErr,
			)
		}
		if exists || value != "" {
			return nil, nil, fmt.Errorf(
				"destination credential slot %s is already occupied; remove the orphaned credential before renaming",
				pair[1],
			)
		}
		if !deviceFileBackendMarkerExists() {
			newPath, pathErr := deviceSecretFilePath(pair[1])
			if pathErr != nil {
				return nil, nil, pathErr
			}
			if _, statErr := os.Lstat(newPath); statErr == nil {
				return nil, nil, fmt.Errorf(
					"destination credential file %s is already occupied; remove the orphaned credential before renaming",
					newPath,
				)
			} else if !os.IsNotExist(statErr) {
				return nil, nil, fmt.Errorf(
					"cannot inspect destination credential file %s: %w",
					newPath,
					statErr,
				)
			}
		}
	}
	// Raw files FIRST: they need no keyring, so a markerless pending file is
	// preserved even on a headless machine where the routed read errors.
	if !deviceFileBackendMarkerExists() {
		for _, pair := range slots {
			oldPath, pathErr := deviceSecretFilePath(pair[0])
			if pathErr != nil {
				continue
			}
			newPath, pathErr := deviceSecretFilePath(pair[1])
			if pathErr != nil {
				continue
			}
			if info, statErr := os.Lstat(oldPath); statErr != nil || !info.Mode().IsRegular() {
				continue
			}
			if renameErr := os.Rename(oldPath, newPath); renameErr != nil {
				rollback()
				return nil, nil, fmt.Errorf("cannot move the pending credential file for %q: %w", oldName, renameErr)
			}
			movedFiles = append(movedFiles, [2]string{oldPath, newPath})
		}
	}
	for _, pair := range slots {
		value, ok, readErr := readCredentialSlot(pair[0])
		if readErr != nil {
			unreachable := errors.Is(
				readErr,
				errDesktopKeyringSessionUnavailable,
			) || errors.Is(readErr, errDesktopKeyringUnavailable)
			if unreachable && len(movedFiles) > 0 {
				printHumanWarn(
					"secure storage is not reachable here (%v); any keyring credential stored under the old name %q by an earlier desktop pairing was not moved — re-run the rename from the desktop session if one exists.",
					readErr,
					pair[0],
				)
				continue
			}
			rollback()
			return nil, nil, fmt.Errorf("cannot read the stored device credential (%s): %w — make secure storage available, then retry", pair[0], readErr)
		}
		if !ok {
			continue
		}
		if writeErr := secretSet(pair[1], value); writeErr != nil {
			rollback()
			return nil, nil, fmt.Errorf("cannot store the device credential under the new name (%s): %w", pair[1], writeErr)
		}
		written = append(written, pair[1])
		obsolete = append(obsolete, pair[0])
	}
	commit = func() {
		for _, service := range obsolete {
			if deleteErr := secretDelete(service); deleteErr != nil {
				printHumanWarn("could not remove the old credential slot %s: %v — remove it manually from secure storage.", service, deleteErr)
			}
		}
	}
	return rollback, commit, nil
}
