package main

import (
	"fmt"
	"os"
	"strings"
)

const deviceCredentialRetirementFilePrefix = ".device-retirement."

type deviceCredentialRetirementPurgeTarget struct {
	profile         string
	previous        runtimeConfig
	checkpoint      deviceCredentialRetirementCheckpoint
	clearCheckpoint bool
}

func deviceCredentialRetirementCheckpointProfiles(
	paths runtimePaths,
) ([]string, error) {
	entries, err := os.ReadDir(paths.ConfigDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"inspect pending device retirements: %w",
			err,
		)
	}
	var profiles []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, deviceCredentialRetirementFilePrefix) ||
			!strings.HasSuffix(name, ".json") {
			continue
		}
		profile := strings.TrimSuffix(
			strings.TrimPrefix(
				name,
				deviceCredentialRetirementFilePrefix,
			),
			".json",
		)
		if entry.Type().IsRegular() {
			if err := validateServerProfileName(profile); err == nil {
				profiles = append(profiles, profile)
				continue
			}
		}
		return nil, fmt.Errorf(
			"invalid device retirement checkpoint %q",
			name,
		)
	}
	return profiles, nil
}

func settleDeviceCredentialRetirementsForPurge(
	paths runtimePaths,
	report *uninstallReport,
) error {
	targets, err := collectDeviceCredentialRetirementPurgeTargets(paths)
	if err != nil {
		return err
	}
	err = executeDeviceCredentialRetirementPurgeTargets(
		paths,
		report,
		targets,
	)
	return err
}

func executeDeviceCredentialRetirementPurgeTargets(
	paths runtimePaths,
	report *uninstallReport,
	targets []deviceCredentialRetirementPurgeTarget,
) error {
	originalProfile := activeServerProfile()
	defer setActiveServerProfile(originalProfile)
	for _, target := range targets {
		setActiveServerProfile(target.profile)
		if target.clearCheckpoint {
			if err := clearDeviceCredentialRetirementCheckpoint(paths); err != nil {
				return err
			}
			continue
		}
		if _, err := completeCheckpointedDeviceCredentialRetirement(
			paths,
			target.previous,
			target.checkpoint,
		); err != nil {
			return fmt.Errorf(
				"settle pending device retirement for server %q: %w",
				target.profile,
				err,
			)
		}
		if report != nil {
			report.addNote(fmt.Sprintf(
				"Finished the pending device retirement for server %q.",
				target.profile,
			))
		}
	}
	return nil
}

// collectDeviceCredentialRetirementPurgeTargets validates every checkpoint and
// secure-store binding before the first revoke or local deletion. A corrupt
// sibling must never be discovered only after another profile was retired.
func collectDeviceCredentialRetirementPurgeTargets(
	paths runtimePaths,
) ([]deviceCredentialRetirementPurgeTarget, error) {
	profiles, err := deviceCredentialRetirementCheckpointProfiles(paths)
	if err != nil || len(profiles) == 0 {
		return nil, err
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf(
			"cannot safely settle pending device retirement without config: %w",
			err,
		)
	}
	originalProfile := activeServerProfile()
	defer setActiveServerProfile(originalProfile)
	targets := make([]deviceCredentialRetirementPurgeTarget, 0, len(profiles))
	for _, profile := range profiles {
		cfg, exists := doc.flatProfile(profile)
		if !exists {
			return nil, fmt.Errorf(
				"device retirement checkpoint references missing server profile %q",
				profile,
			)
		}
		checkpoint, exists, err :=
			readDeviceCredentialRetirementCheckpointForProfile(paths, profile)
		if err != nil {
			return nil, fmt.Errorf(
				"read pending device retirement for server %q: %w",
				profile,
				err,
			)
		}
		if !exists {
			continue
		}
		if cfg.ProfileID != checkpoint.ProfileID {
			return nil, fmt.Errorf(
				"device retirement checkpoint profile identity changed for server %q",
				profile,
			)
		}
		previous := runtimeConfig{
			ProfileID:            checkpoint.ProfileID,
			RelayInstanceID:      checkpoint.RelayInstanceID,
			RelaySecureBaseURL:   checkpoint.RelaySecureBaseURL,
			RelaySpkiPin:         checkpoint.RelaySpkiPin,
			PendingSecureBaseURL: checkpoint.PendingSecureBaseURL,
			PendingSpkiPin:       checkpoint.PendingSpkiPin,
		}
		setActiveServerProfile(profile)
		endpointsEqual := deviceRetirementEndpointsEqual(cfg, previous)
		if endpointsEqual &&
			checkpoint.Phase == deviceCredentialRetirementPrepared {
			targets = append(targets, deviceCredentialRetirementPurgeTarget{
				profile:         profile,
				previous:        previous,
				checkpoint:      checkpoint,
				clearCheckpoint: true,
			})
			continue
		}
		restoredRevoked := endpointsEqual &&
			checkpoint.Phase == deviceCredentialRetirementRevoked &&
			cfg.RelayInstanceID == checkpoint.RelayInstanceID
		if !deviceRetirementEndpointsCleared(cfg) && !restoredRevoked ||
			cfg.Cloud != nil ||
			strings.TrimSpace(cfg.RelayInstanceID) != "" &&
				!restoredRevoked {
			return nil, fmt.Errorf(
				"device retirement checkpoint does not match server profile %q",
				profile,
			)
		}
		if err := validateDeviceCredentialRetirementBindings(
			checkpoint,
		); err != nil {
			return nil, relayAuthTokenSetupOperationError(
				fmt.Sprintf(
					"validate pending device retirement for server %q",
					profile,
				),
				err,
			)
		}
		targets = append(targets, deviceCredentialRetirementPurgeTarget{
			profile:    profile,
			previous:   previous,
			checkpoint: checkpoint,
		})
	}
	return targets, nil
}
