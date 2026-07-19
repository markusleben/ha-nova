package main

import (
	"fmt"
	"strings"
)

// Hook for tests.
var revokeSelfDeviceV1ForUninstall = revokeSelfDeviceV1

// purgeDeviceCredentialWithReport revokes this device's pairing on the relay
// and removes both local credential slots (current + pending). Full purge only —
// standard uninstall keeps the pairing so a reinstall reconnects instantly.
//
// Revocation is best-effort: revokeSelfDeviceV1 already treats HTTP 401 as
// success (a lost earlier response means the relay no longer knows us). An
// unreachable relay never blocks the local wipe; it leaves a note with the
// device id so the owner can remove the entry on the NOVA page.
// relayExpectedGone marks a run whose guided teardown already removed the App:
// its device registry died with the App's data, so the NOVA-page hint makes no
// sense there. The revoke itself is still attempted — see below.
func purgeDeviceCredentialWithReport(secureBaseURL, spkiPin string, report *uninstallReport, relayExpectedGone bool) {
	// The pending slot is purely local (never activated): just drop it.
	_ = deletePendingDeviceCredential()
	// File-backed installs also leave the storage-mode marker; drop it (and the
	// now-empty secrets dir) so a later reinstall re-probes cleanly rather than
	// inheriting a stale file-mode decision.
	defer removeDeviceFileStorageResidue()

	credential, ok, err := readDeviceCredential()
	if err != nil {
		// The slot exists but is unreadable/malformed: removing it needs no
		// parse, and staying silent would leave a stale secret behind.
		if deleteDeviceCredential() == nil {
			report.addRemoved("Device credential (secure storage)")
			if relayExpectedGone {
				report.addNote("The stored device credential was unreadable and was removed without revoking.")
			} else {
				report.addNote("The stored device credential was unreadable and was removed without revoking. Check the NOVA page in Home Assistant for a stale device entry.")
			}
		} else {
			report.addNote("The stored device credential is unreadable and could not be removed from secure storage; remove it manually once secure storage works again.")
		}
		return
	}
	if !ok {
		return
	}

	// The revoke is always attempted (cheap and fails fast against a dead
	// relay): a teardown the user believed complete may not have removed the
	// App, and skipping the revoke would strand an ACTIVE device entry.
	revoked := false
	secureBaseURL = strings.TrimSpace(secureBaseURL)
	spkiPin = strings.TrimSpace(spkiPin)
	if secureBaseURL != "" && spkiPin != "" {
		revoked = revokeSelfDeviceV1ForUninstall(secureBaseURL, spkiPin, credential) == nil
	}

	if deleteDeviceCredential() == nil {
		report.addRemoved("Device credential (secure storage)")
	}
	switch {
	case revoked:
		report.addNote("Revoked this device's pairing on the relay.")
	case relayExpectedGone:
		// The App (and its device registry) is already gone; nothing to revoke.
	case secureBaseURL != "":
		deviceID := "unknown"
		if parsed := parseDeviceCredential(credential); parsed != nil {
			deviceID = parsed.deviceID
		}
		report.addNote(fmt.Sprintf("Could not reach the relay to revoke this device's pairing (device id %s). Remove it on the NOVA page in Home Assistant.", deviceID))
	}
}

// deviceCredentialExistsForUninstall reports whether this install holds an
// activated device credential (standard uninstall keeps it and says so).
func deviceCredentialExistsForUninstall() bool {
	_, ok, err := readDeviceCredential()
	return err == nil && ok
}
