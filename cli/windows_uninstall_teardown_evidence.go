package main

import (
	"fmt"
	"os"
)

type windowsUninstallRelayRemovalRef struct {
	ProfileName     string `json:"profile_name"`
	RelayInstanceID string `json:"relay_instance_id"`
}

func beginWindowsUninstallStatusWithTeardown(
	paths runtimePaths,
	mode uninstallMode,
	installSource string,
	teardownCompleted bool,
	removedRelays uninstallRelayRemovalEvidence,
) (*windowsUninstallStatus, error) {
	status := &windowsUninstallStatus{
		SchemaVersion: windowsUninstallStatusSchemaVersion,
		Operation:     windowsUninstallStatusOperation,
		Status:        windowsUninstallStatusRunning,
		Mode:          string(mode),
		InstallSource: normalizeInstallSource(installSource),
		HelperPID:     os.Getpid(),
		StartedAt:     windowsUninstallStatusNow().UTC(),
		LastUpdatedAt: windowsUninstallStatusNow().UTC(),
		InstallRoot:   paths.InstallRoot,
	}
	if err := setWindowsUninstallTeardownEvidence(
		status,
		teardownCompleted,
		removedRelays,
	); err != nil {
		return nil, err
	}
	if err := writeWindowsUninstallStatus(paths, *status); err != nil {
		return nil, err
	}
	return status, nil
}

func windowsUninstallHelperTeardownEvidence(
	teardownCompleted bool,
	relayInstanceID string,
) (uninstallRelayRemovalEvidence, error) {
	if relayInstanceID != "" && !teardownCompleted {
		return nil, fmt.Errorf(
			"Windows uninstall Relay-removal identity requires --teardown-done",
		)
	}
	if relayInstanceID == "" {
		return nil, nil
	}
	if !validIdentifier(relayInstanceID, 256) {
		return nil, fmt.Errorf(
			"Windows uninstall guided-teardown Relay identity is invalid",
		)
	}
	return uninstallRelayRemovalEvidence{
		defaultServerProfileName: relayInstanceID,
	}, nil
}

func setWindowsUninstallTeardownEvidence(
	status *windowsUninstallStatus,
	teardownCompleted bool,
	removedRelays uninstallRelayRemovalEvidence,
) error {
	if status == nil {
		return fmt.Errorf("Windows uninstall recovery status is missing")
	}
	ref, err := windowsUninstallRelayRemovalRefFromEvidence(
		teardownCompleted,
		removedRelays,
	)
	if err != nil {
		return err
	}
	status.GuidedTeardownCompleted = teardownCompleted
	status.GuidedTeardownRelayRemoval = ref
	return nil
}

func windowsUninstallRelayRemovalRefFromEvidence(
	teardownCompleted bool,
	removedRelays uninstallRelayRemovalEvidence,
) (*windowsUninstallRelayRemovalRef, error) {
	if len(removedRelays) == 0 {
		return nil, nil
	}
	if !teardownCompleted {
		return nil, fmt.Errorf(
			"Windows uninstall Relay-removal evidence requires a completed guided teardown",
		)
	}
	if len(removedRelays) != 1 {
		return nil, fmt.Errorf(
			"Windows guided teardown must identify exactly one removed Relay",
		)
	}
	relayInstanceID, exists := removedRelays[defaultServerProfileName]
	if !exists {
		return nil, fmt.Errorf(
			"Windows guided teardown Relay-removal evidence must reference server profile %q",
			defaultServerProfileName,
		)
	}
	ref := &windowsUninstallRelayRemovalRef{
		ProfileName:     defaultServerProfileName,
		RelayInstanceID: relayInstanceID,
	}
	if err := validateWindowsUninstallRelayRemovalRef(ref); err != nil {
		return nil, err
	}
	return ref, nil
}

func validateWindowsUninstallTeardownEvidence(
	status windowsUninstallStatus,
) error {
	ref := status.GuidedTeardownRelayRemoval
	if ref == nil {
		return nil
	}
	if !status.GuidedTeardownCompleted {
		return fmt.Errorf(
			"Windows uninstall recovery marker has Relay-removal evidence without a completed guided teardown",
		)
	}
	return validateWindowsUninstallRelayRemovalRef(ref)
}

func validateWindowsUninstallRelayRemovalRef(
	ref *windowsUninstallRelayRemovalRef,
) error {
	if ref == nil {
		return nil
	}
	if ref.ProfileName != defaultServerProfileName {
		return fmt.Errorf(
			"Windows uninstall recovery marker has unsupported guided-teardown server profile %q",
			ref.ProfileName,
		)
	}
	if err := validateServerProfileName(ref.ProfileName); err != nil {
		return fmt.Errorf(
			"Windows uninstall recovery marker has invalid guided-teardown server profile: %w",
			err,
		)
	}
	if !validIdentifier(ref.RelayInstanceID, 256) {
		return fmt.Errorf(
			"Windows uninstall recovery marker has invalid guided-teardown Relay identity",
		)
	}
	return nil
}

func windowsUninstallTeardownEvidence(
	status windowsUninstallStatus,
) (bool, uninstallRelayRemovalEvidence, error) {
	if err := validateWindowsUninstallTeardownEvidence(status); err != nil {
		return false, nil, err
	}
	if status.GuidedTeardownRelayRemoval == nil {
		return status.GuidedTeardownCompleted, nil, nil
	}
	ref := status.GuidedTeardownRelayRemoval
	return status.GuidedTeardownCompleted, uninstallRelayRemovalEvidence{
		ref.ProfileName: ref.RelayInstanceID,
	}, nil
}
