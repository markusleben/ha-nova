package main

import (
	"context"
	"errors"
	"os"
)

func loadCloudUnlockConfig(
	paths runtimePaths,
	options cloudCommandFlags,
) (cloudManagementSnapshot, bool, error) {
	snapshot, err := loadCloudManagementSnapshot(paths)
	if err == nil {
		return snapshot, false, nil
	}
	if errors.Is(err, errInvalidClientInstallID) {
		recovery, recoveryErr :=
			loadCloudRecoverySnapshotUnchecked(paths)
		if recoveryErr == nil &&
			recovery.Config.Cloud != nil &&
			cloudRecoveryHoldProblem(recovery.Config) != nil {
			return recovery, false, nil
		}
	}
	if options.server == "" ||
		(!errors.Is(err, os.ErrNotExist) &&
			!errors.Is(err, errUnknownServerProfile)) {
		return cloudManagementSnapshot{}, false, err
	}
	// An explicit --server is safe device-slot intent even before a Cloud
	// profile exists. Do not persist synthetic config merely to show the native
	// keyring prompt; the original add command remains the creation authority.
	setActiveServerProfile(options.server)
	return cloudManagementSnapshot{}, true, nil
}

func preflightCloudUnlockDeviceAccess(
	ctx context.Context,
	cfg runtimeConfig,
	preProfile bool,
) error {
	if preProfile || cfg.Cloud == nil {
		return preflightWritableCloudDeviceAccess(
			ctx,
			cfg.RelayInstanceID,
			true,
			SecretStoreAllowUI,
		)
	}
	return preflightCloudDeviceAccess(
		ctx,
		cfg.RelayInstanceID,
		true,
		SecretStoreAllowUI,
	)
}
