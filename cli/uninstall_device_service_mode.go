package main

import (
	"context"
	"strings"
)

// pinPurgeDeviceBackend decides the device-credential backend ONCE for a full
// purge and holds it for the whole run. The returned func releases the pin.
//
// File-backed install (marker present): the purge deletes the slot files and
// then the marker. Without the pin, the final absence proofs re-decide the
// backend after the marker is gone and consult the OS keyring — which on a
// headless machine is unreachable, so every headless purge failed at its last
// step. The keyring was never this install's device store.
//
// Unpaired service install (no marker): `setup --service` routes device
// credentials to files, but the marker lands only together with a device
// credential, so a relay-token-only install (standalone relay) has none. The
// config is the proof instead: an explicit service token file plus no recorded
// device endpoint, pending endpoint, removal checkpoint, pending retirement, or
// Cloud link means no device credential was ever activated. Only then, and
// only while the keyring is unreachable, the purge runs file-backed (exactly
// what a paired service install does) and notes the unchecked keyring. Any
// pairing evidence keeps the fail-closed keyring path.
func pinPurgeDeviceBackend(
	paths runtimePaths,
	targets []profilePurgeTarget,
	cloudTargets []cloudPurgeTarget,
	retirementProfiles []string,
	report *uninstallReport,
) func() {
	previous := deviceCredentialFileModePinned
	release := func() { deviceCredentialFileModePinned = previous }
	if deviceSecretFileBacked() {
		deviceCredentialFileModePinned = true
		return release
	}
	if !unpairedServiceInstallWithoutKeyring(
		paths,
		targets,
		cloudTargets,
		retirementProfiles,
	) {
		return func() {}
	}
	deviceCredentialFileModePinned = true
	report.addNote("Secure storage is unavailable here, and this service-mode install never paired a device, so the keyring was not checked for device credentials. If this machine was once set up from a desktop session, remove leftover 'ha-nova.device-credential' keyring entries there.")
	return release
}

func unpairedServiceInstallWithoutKeyring(
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
