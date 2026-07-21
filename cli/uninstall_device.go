package main

import (
	"fmt"
	"strings"
)

// Hook for tests.
var revokeSelfDeviceV1ForUninstall = revokeSelfDeviceV1

// profilePurgeTarget names one server profile's credential slots plus the
// pinned endpoint its revoke must go to — each profile's device entry lives on
// ITS relay, never on a sibling's.
type profilePurgeTarget struct {
	name          string
	secureBaseURL string
	spkiPin       string
}

// purgeDeviceCredentialWithReport revokes ONE profile's pairing on its relay
// and removes both of that profile's local credential slots (current +
// pending). Full purge only — standard uninstall keeps the pairing so a
// reinstall reconnects instantly. Defaults to the active server profile.
//
// Revocation is best-effort: revokeSelfDeviceV1 already treats HTTP 401 as
// success (a lost earlier response means the relay no longer knows us). An
// unreachable relay never blocks the local wipe; it leaves a note with the
// device id so the owner can remove the entry on the NOVA page.
// relayExpectedGone marks a run whose guided teardown already removed the App:
// its device registry died with the App's data, so the NOVA-page hint makes no
// sense there. The revoke itself is still attempted — see below.
func purgeDeviceCredentialWithReport(secureBaseURL, spkiPin string, report *uninstallReport, relayExpectedGone bool) {
	purgeProfileDeviceCredentialWithReport(profilePurgeTarget{
		name:          activeServerProfile(),
		secureBaseURL: secureBaseURL,
		spkiPin:       spkiPin,
	}, report, relayExpectedGone)
}

// purgeAllDeviceCredentialsWithReport iterates EVERY server profile: revoke per
// profile against that profile's pinned endpoint, delete every namespaced slot,
// then sweep the remaining slot files, the machine-wide file-backend marker,
// and the (then empty) secrets dir.
func purgeAllDeviceCredentialsWithReport(targets []profilePurgeTarget, report *uninstallReport, relayExpectedGone bool) {
	for _, target := range targets {
		purgeProfileDeviceCredentialWithReport(target, report, relayExpectedGone)
	}
	removeAllDeviceFileStorageResidue()
}

func purgeProfileDeviceCredentialWithReport(target profilePurgeTarget, report *uninstallReport, relayExpectedGone bool) {
	currentService := deviceCredentialServiceForProfile(target.name)
	// The pending slot is purely local (never activated): just drop it.
	_ = secretDelete(deviceCredentialPendingServiceForProfile(target.name))
	// File-backed installs also leave the storage-mode marker; drop it (and the
	// now-empty secrets dir) once NO profile's slots remain, so a later reinstall
	// re-probes cleanly rather than inheriting a stale file-mode decision.
	defer removeDeviceFileStorageResidueForProfile(target.name)

	slotLabel := "Device credential (secure storage)"
	if target.name != defaultServerProfileName {
		slotLabel = fmt.Sprintf("Device credential (secure storage, server %q)", target.name)
	}

	credential, ok, err := readCredentialSlot(currentService)
	if err != nil {
		// The slot exists but is unreadable/malformed: removing it needs no
		// parse, and staying silent would leave a stale secret behind.
		if secretDelete(currentService) == nil {
			report.addRemoved(slotLabel)
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
	secureBaseURL := strings.TrimSpace(target.secureBaseURL)
	spkiPin := strings.TrimSpace(target.spkiPin)
	if secureBaseURL != "" && spkiPin != "" {
		revoked = revokeSelfDeviceV1ForUninstall(secureBaseURL, spkiPin, credential) == nil
	}

	if secretDelete(currentService) == nil {
		report.addRemoved(slotLabel)
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
// activated device credential in ANY server profile (standard uninstall keeps
// them and says so).
func deviceCredentialExistsForUninstall() bool {
	for _, profile := range credentialProfileNames() {
		if _, ok, err := readCredentialSlot(deviceCredentialServiceForProfile(profile)); err == nil && ok {
			return true
		}
	}
	return false
}
