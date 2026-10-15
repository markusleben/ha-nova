package main

import (
	"context"
	"strings"
)

// pinPurgeDeviceBackend holds a full purge on the file backend for its whole
// run when this session cannot reach the keyring. The returned func releases
// the pin.
//
// File-backed install (marker present): the purge deletes the slot files and
// then the marker; the final absence proofs then re-decide the backend and
// read the keyring. Where the keyring is reachable that stays as is (it can
// hold a credential stranded by a keyring-to-file switch made while it was
// locked). Where it is not, every headless purge failed at its last step, so
// the purge stays on files and notes the unchecked keyring.
//
// Unpaired service install (no marker): `setup --service` routes device
// credentials to files, but the marker lands only together with a device
// credential, so a relay-token-only install (standalone relay) has none. The
// config is the proof instead: an explicit service token file plus no recorded
// device endpoint, pending endpoint, removal checkpoint, pending retirement,
// or Cloud link means no device credential was ever activated. Any pairing
// evidence keeps the fail-closed keyring path.
func pinPurgeDeviceBackend(
	paths runtimePaths,
	targets []profilePurgeTarget,
	cloudTargets []cloudPurgeTarget,
	retirementProfiles []string,
	report *uninstallReport,
) func() {
	if !deviceSecretFileBacked() &&
		!unpairedServiceInstall(paths, targets, cloudTargets, retirementProfiles) {
		return func() {}
	}
	if !deviceKeyringUnreachable() {
		return func() {}
	}
	previous := deviceCredentialFileModePinned
	deviceCredentialFileModePinned = true
	report.addNote("Secure storage is unavailable here, so the keyring was not checked for leftover HA NOVA device credentials. If this machine was once set up from a desktop session, remove leftover 'ha-nova.device-credential' keyring entries there.")
	return func() { deviceCredentialFileModePinned = previous }
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
