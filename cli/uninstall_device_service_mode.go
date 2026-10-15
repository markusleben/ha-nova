package main

import (
	"context"
	"fmt"
	"strings"
)

// pinPurgeDeviceBackend fixes the device-credential backend for a full purge
// (both the guided preflight and the purge itself). The returned func releases
// it.
//
// File-backed install (marker present): the marker is held on disk until the
// final proofs passed (releaseHeldDeviceFileBackendMarker), so every read,
// delete, proof, and retry of this purge stays on files. Before, the purge
// dropped the marker first; its proofs then read the keyring, and a failed
// purge's retry re-decided to the keyring and deleted credentials there.
// Leftover keyring credentials are checked read-only at the end
// (requireNoStrandedKeyringDeviceCredentials).
//
// Unpaired service install (no marker): `setup --service` routes device
// credentials to files, but the marker lands only together with a device
// credential, so a relay-token-only install (standalone relay) has none. The
// config is the proof instead: an explicit service token file plus no recorded
// device endpoint, pending endpoint, removal checkpoint, pending retirement,
// or Cloud link means no device credential was ever activated. Only while the
// keyring is unreachable, the purge then runs on files with a note. Any
// pairing evidence keeps the fail-closed keyring path.
func pinPurgeDeviceBackend(
	paths runtimePaths,
	targets []profilePurgeTarget,
	cloudTargets []cloudPurgeTarget,
	retirementProfiles []string,
	report *uninstallReport,
) func() {
	previousHeld, previousPinned :=
		deviceFileBackendMarkerHeld, deviceCredentialFileModePinned
	release := func() {
		deviceFileBackendMarkerHeld = previousHeld
		deviceCredentialFileModePinned = previousPinned
	}
	deviceFileBackendMarkerHeld = true
	if deviceFileBackendMarkerExists() ||
		!unpairedServiceInstall(paths, targets, cloudTargets, retirementProfiles) ||
		!deviceKeyringUnreachable() {
		return release
	}
	deviceCredentialFileModePinned = true
	report.addNote(uncheckedKeyringNote)
	return release
}

const uncheckedKeyringNote = "Secure storage is unavailable here, so the keyring was not checked for leftover HA NOVA device credentials. If this machine was once set up from a desktop session, remove leftover 'ha-nova.device-credential' keyring entries there."

// requireNoStrandedKeyringDeviceCredentials checks, read-only, that a
// file-backed install leaves no device credential in the OS keyring (one can
// be stranded by a keyring-to-file switch made while the keyring was locked).
// It never deletes there: the keyring is not this install's store.
func requireNoStrandedKeyringDeviceCredentials(
	targets []profilePurgeTarget,
	report *uninstallReport,
) error {
	if !deviceFileBackendMarkerExists() {
		return nil
	}
	for _, target := range targets {
		for _, service := range []string{
			deviceCredentialServiceForProfile(target.name),
			deviceCredentialPendingServiceForProfile(target.name),
		} {
			_, exists, err := readKeyringDeviceSecret(service)
			if err != nil {
				report.addNote(uncheckedKeyringNote)
				return nil
			}
			if exists {
				return fmt.Errorf(
					"the OS keyring still holds a device credential for server %q (%s) from before this install switched to file storage; nothing there was deleted. Remove that keyring entry (for example in Keychain Access), and remove that device on the NOVA page in Home Assistant, then rerun `ha-nova uninstall --purge`",
					target.name,
					service,
				)
			}
		}
	}
	return nil
}

// releaseHeldDeviceFileBackendMarker drops the held backend marker and the
// (then empty) secrets dir as the purge's last device step.
func releaseHeldDeviceFileBackendMarker() error {
	deviceFileBackendMarkerHeld = false
	if err := removeDeviceFileStorageMarkerAndDir(); err != nil {
		deviceFileBackendMarkerHeld = true
		return err
	}
	return nil
}

func unpairedServiceInstall(
	paths runtimePaths,
	targets []profilePurgeTarget,
	cloudTargets []cloudPurgeTarget,
	retirementProfiles []string,
) bool {
	if len(cloudTargets) > 0 || len(retirementProfiles) > 0 {
		return false
	}
	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil || strings.TrimSpace(cfg.RelayTokenFile) == "" {
		return false
	}
	for _, target := range targets {
		if profilePurgeTargetHasDeviceEvidence(target) {
			return false
		}
	}
	return true
}

// deviceKeyringUnreachable reports whether the device-slot keyring preflight
// fails here. ponytail: macOS preflight never fails without UI, so this only
// fires on Linux sessions without a usable Secret Service.
func deviceKeyringUnreachable() bool {
	if _, ok := testSecretDir(); ok {
		return false
	}
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return deviceCredentialPreflightWithContext(ctx, SecretStoreForbidUI) != nil
}

func profilePurgeTargetHasDeviceEvidence(target profilePurgeTarget) bool {
	return target.secureBaseURL != "" ||
		target.spkiPin != "" ||
		target.pendingSecureBaseURL != "" ||
		target.pendingSpkiPin != "" ||
		target.removalCheckpointed ||
		target.observedCurrentID != "" ||
		target.observedPendingID != "" ||
		target.revokedCurrentID != "" ||
		target.revokedPendingID != ""
}
