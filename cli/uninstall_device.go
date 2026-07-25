package main

import (
	"fmt"
	"strings"
)

// Hook for tests.
var revokeSelfDeviceV1ForUninstall = revokeSelfDeviceV1

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
func purgeDeviceCredentialWithReport(
	secureBaseURL, spkiPin string,
	report *uninstallReport,
	relayExpectedGone bool,
) error {
	return purgeProfileDeviceCredentialWithReport(profilePurgeTarget{
		name:          activeServerProfile(),
		secureBaseURL: secureBaseURL,
		spkiPin:       spkiPin,
	}, report, relayExpectedGone)
}

// purgeAllDeviceCredentialsWithReport iterates EVERY server profile: revoke per
// profile against that profile's pinned endpoint, delete every namespaced slot,
// then sweep the remaining slot files, the machine-wide file-backend marker,
// and the (then empty) secrets dir.
func purgeAllDeviceCredentialsWithReport(
	targets []profilePurgeTarget,
	report *uninstallReport,
	removedRelays uninstallRelayRemovalEvidence,
) error {
	for _, target := range targets {
		if err := purgeProfileDeviceCredentialWithReport(
			target,
			report,
			removedRelays.matches(
				target.name,
				target.relayInstanceID,
			),
		); err != nil {
			return err
		}
	}
	return removeAllDeviceFileStorageResidue()
}

func purgeProfileDeviceCredentialWithReport(
	target profilePurgeTarget,
	report *uninstallReport,
	relayExpectedGone bool,
) error {
	if _, err := resumeKeyringDeviceCredentialCleanup(); err != nil {
		return fmt.Errorf(
			"finish device credential migration cleanup before purging server %q: %w",
			target.name,
			err,
		)
	}
	slotState, err := inspectProfilePurgeSlotState(target)
	if err != nil {
		return err
	}
	if err := validateRequiredProfilePurgeSlots(
		target,
		slotState,
	); err != nil {
		return err
	}
	currentService := deviceCredentialServiceForProfile(target.name)
	pendingService := deviceCredentialPendingServiceForProfile(target.name)
	pendingUnreadable := false
	pending, pendingExists, pendingErr :=
		readPendingCredentialRecordFromService(pendingService)
	credential, currentExists, currentErr :=
		readCredentialSlot(currentService)
	if pendingErr == nil {
		slotState.pendingExists = pendingExists
		slotState.pendingLocal = pendingExists &&
			pending.Source == pendingDeviceCredentialSourceLocal
	}
	if currentErr == nil {
		slotState.currentExists = currentExists
	}
	if err := validateRequiredProfilePurgeSlots(
		target,
		slotState,
	); err != nil {
		return err
	}
	if pendingErr != nil {
		// A malformed pending value cannot be authenticated to a Relay. Preserve
		// the previous cleanup behavior, but make the unrevoked deletion visible.
		pendingUnreadable = true
	} else if pendingExists &&
		pending.Source == pendingDeviceCredentialSourceLocal {
		pendingBaseURL := strings.TrimSpace(target.pendingSecureBaseURL)
		pendingPin := strings.TrimSpace(target.pendingSpkiPin)
		switch {
		case pendingBaseURL != "" && pendingPin != "":
			if err := revokeSelfDeviceV1ForUninstall(
				pendingBaseURL,
				pendingPin,
				pending.Credential,
			); err == nil {
				report.addNote(
					"Revoked the interrupted pending device pairing on the relay.",
				)
			} else if !relayExpectedGone {
				report.addNote(fmt.Sprintf(
					"Could not reach the relay to revoke the interrupted pending device pairing (device id %s). Remove it on the NOVA page in Home Assistant.",
					deviceCredentialID(pending.Credential),
				))
			}
		case pendingBaseURL != "" || pendingPin != "":
			return fmt.Errorf(
				"pending device endpoint for server %q is incomplete; refusing to delete a possibly active credential",
				target.name,
			)
		}
	}
	// Cloud pending credentials are revoked by the Cloud teardown before this
	// local sweep. A local pending without an endpoint was never activated.
	if err := secretDelete(
		pendingService,
	); err != nil {
		return fmt.Errorf(
			"remove pending device credential for server %q: %w",
			target.name,
			err,
		)
	}
	if pendingUnreadable {
		report.addNote(
			"The stored pending device credential was unreadable and was removed without revoking.",
		)
	}

	slotLabel := "Device credential (secure storage)"
	if target.name != defaultServerProfileName {
		slotLabel = fmt.Sprintf("Device credential (secure storage, server %q)", target.name)
	}

	if currentErr != nil {
		// The slot exists but is unreadable/malformed: removing it needs no
		// parse, and staying silent would leave a stale secret behind.
		deleteErr := secretDelete(currentService)
		if deleteErr == nil {
			report.addRemoved(slotLabel)
			if relayExpectedGone {
				report.addNote("The stored device credential was unreadable and was removed without revoking.")
			} else {
				report.addNote("The stored device credential was unreadable and was removed without revoking. Check the NOVA page in Home Assistant for a stale device entry.")
			}
		} else {
			report.addNote("The stored device credential is unreadable and could not be removed from secure storage; remove it manually once secure storage works again.")
			return fmt.Errorf(
				"remove unreadable device credential for server %q: %w",
				target.name,
				deleteErr,
			)
		}
		return removeDeviceFileStorageResidueForProfile(target.name)
	}
	if !currentExists {
		return removeDeviceFileStorageResidueForProfile(target.name)
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

	if err := secretDelete(currentService); err != nil {
		return fmt.Errorf(
			"remove device credential for server %q: %w",
			target.name,
			err,
		)
	}
	report.addRemoved(slotLabel)
	switch {
	case revoked:
		report.addNote("Revoked this device's pairing on the relay.")
	case relayExpectedGone:
		// The App (and its device registry) is already gone; nothing to revoke.
	case secureBaseURL != "":
		report.addNote(fmt.Sprintf("Could not reach the relay to revoke this device's pairing (device id %s). Remove it on the NOVA page in Home Assistant.", deviceCredentialID(credential)))
	}
	return removeDeviceFileStorageResidueForProfile(target.name)
}

func readPendingCredentialRecordFromService(
	service string,
) (pendingDeviceCredentialRecord, bool, error) {
	value, err := secretGet(service)
	if err != nil {
		if err == errSecretNotFound {
			return pendingDeviceCredentialRecord{}, false, nil
		}
		return pendingDeviceCredentialRecord{}, false, err
	}
	record, err := decodePendingDeviceCredentialRecord(value)
	if err != nil {
		return pendingDeviceCredentialRecord{}, false, err
	}
	return record, true, nil
}

func deviceCredentialID(credential string) string {
	if parsed := parseDeviceCredential(credential); parsed != nil {
		return parsed.deviceID
	}
	return "unknown"
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
