package main

import (
	"errors"
	"os"
)

// recoverSetupConfigAfterLoadError distinguishes a legitimate first/incomplete
// setup from a document that failed schema or semantic validation. Incomplete
// selected profiles retain their full valid state so pending local and Cloud
// transactions remain visible to the recovery guards.
func recoverSetupConfigAfterLoadError(
	paths runtimePaths,
	loadErr error,
) (runtimeConfig, error) {
	if loadErr == nil {
		return runtimeConfig{}, nil
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err == nil {
		if err := validateLoadedRuntimeConfig(&cfg); err != nil {
			return runtimeConfig{}, err
		}
		return cfg, nil
	}
	if errors.Is(err, errInvalidClientInstallID) {
		snapshot, snapshotErr := loadCloudRecoverySnapshotUnchecked(paths)
		if snapshotErr == nil && snapshot.Config.Cloud != nil {
			return snapshot.Config, nil
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		return runtimeConfig{}, nil
	}
	if errors.Is(err, errUnknownServerProfile) {
		requested, _ := requestedServerSelection()
		if requested == defaultServerProfileName {
			doc, docErr := loadConfigDocument(paths.ConfigFile)
			if docErr != nil {
				return runtimeConfig{}, loadErr
			}
			return runtimeConfig{
				ClientInstallID: doc.meta.ClientInstallID,
			}, nil
		}
	}
	return runtimeConfig{}, loadErr
}
