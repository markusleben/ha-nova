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
func removeDeviceFileStorageResidueForProfile(profile string) {
	_ = deviceSecretFileDelete(deviceCredentialServiceForProfile(profile))
	_ = deviceSecretFileDelete(deviceCredentialPendingServiceForProfile(profile))
	if !deviceCredentialSlotFilesRemain() {
		removeDeviceFileStorageMarkerAndDir()
	}
}

// removeAllDeviceFileStorageResidue clears EVERY profile's slot files, the
// marker, and the (then empty) secrets dir. Full-purge only: a leftover marker
// would otherwise make a fresh reinstall inherit file mode without re-probing.
func removeAllDeviceFileStorageResidue() {
	if dir, err := deviceSecretFileDir(); err == nil {
		if entries, readErr := os.ReadDir(dir); readErr == nil {
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), deviceCredentialService) {
					_ = os.Remove(filepath.Join(dir, entry.Name()))
				}
			}
		}
	}
	removeDeviceFileStorageMarkerAndDir()
}

func deviceCredentialSlotFilesRemain() bool {
	dir, err := deviceSecretFileDir()
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, deviceCredentialService) && name != deviceCredentialProbeService {
			return true
		}
	}
	return false
}

// removeDeviceFileStorageMarkerAndDir drops the machine-wide backend marker and
// the secrets dir (only when empty), and resets the in-process flags so the
// same run re-decides the backend.
func removeDeviceFileStorageMarkerAndDir() {
	if path, err := deviceFileBackendMarkerPath(); err == nil {
		_ = os.Remove(path)
	}
	if dir, err := deviceSecretFileDir(); err == nil {
		_ = os.Remove(dir) // removes only when now empty
	}
	deviceCredentialFileModeForced = false
	deviceCredentialFileModeExplicit = false
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
	rollback = func() {
		for _, service := range written {
			_ = secretDelete(service)
		}
		for _, pair := range movedFiles {
			_ = os.Rename(pair[1], pair[0])
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
			// Keyring REACHABILITY errors on a headless machine are tolerated as
			// soon as any markerless raw file moved (the interrupted file-pairing
			// case this exists for): the routed layer cannot be moved from here
			// for ANY slot, and a keyring credential from an earlier DESKTOP
			// pairing needs the desktop session — the warning says so. Every
			// other error (e.g. a malformed stored credential) stays fatal.
			unreachable := errors.Is(readErr, errDesktopKeyringSessionUnavailable) || errors.Is(readErr, errDesktopKeyringUnavailable)
			if unreachable && len(movedFiles) > 0 {
				printHumanWarn("secure storage is not reachable here (%v); any keyring credential stored under the old name %q by an earlier desktop pairing was not moved — re-run the rename from the desktop session if one exists.", readErr, pair[0])
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
