package main

import (
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
