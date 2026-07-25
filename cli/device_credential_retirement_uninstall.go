package main

import (
	"fmt"
	"os"
	"strings"
)

const deviceCredentialRetirementFilePrefix = ".device-retirement."

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
	relayAlreadyRemoved bool,
) error {
	profiles, err := deviceCredentialRetirementCheckpointProfiles(paths)
	if err != nil || len(profiles) == 0 {
		return err
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return fmt.Errorf(
			"cannot safely settle pending device retirement without config: %w",
			err,
		)
	}
	originalProfile := activeServerProfile()
	defer setActiveServerProfile(originalProfile)
	for _, profile := range profiles {
		cfg, exists := doc.flatProfile(profile)
		if !exists {
			return fmt.Errorf(
				"device retirement checkpoint references missing server profile %q",
				profile,
			)
		}
		checkpoint, exists, err :=
			readDeviceCredentialRetirementCheckpointForProfile(paths, profile)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if cfg.ProfileID != checkpoint.ProfileID {
			return fmt.Errorf(
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
			if err := clearDeviceCredentialRetirementCheckpoint(paths); err != nil {
				return err
			}
			continue
		}
		restoredRevoked := endpointsEqual &&
			checkpoint.Phase == deviceCredentialRetirementRevoked &&
			cfg.RelayInstanceID == checkpoint.RelayInstanceID
		if !deviceRetirementEndpointsCleared(cfg) && !restoredRevoked ||
			cfg.Cloud != nil ||
			strings.TrimSpace(cfg.RelayInstanceID) != "" &&
				!restoredRevoked {
			return fmt.Errorf(
				"device retirement checkpoint does not match server profile %q",
				profile,
			)
		}
		if err := validateDeviceCredentialRetirementBindings(
			checkpoint,
		); err != nil {
			return fmt.Errorf(
				"validate pending device retirement for server %q: %w",
				profile,
				err,
			)
		}
		if relayAlreadyRemoved &&
			checkpoint.Phase == deviceCredentialRetirementPrepared {
			checkpoint, err = markDeviceCredentialRetirementRevoked(
				paths,
				checkpoint,
			)
			if err != nil {
				return err
			}
		}
		if _, err := completeCheckpointedDeviceCredentialRetirement(
			paths,
			previous,
			checkpoint,
		); err != nil {
			return fmt.Errorf(
				"settle pending device retirement for server %q: %w",
				profile,
				err,
			)
		}
		if report != nil {
			report.addNote(fmt.Sprintf(
				"Finished the pending device retirement for server %q.",
				profile,
			))
		}
	}
	return nil
}
