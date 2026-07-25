package main

import (
	"errors"
	"fmt"
)

var saveConfigForSetupPersistence = saveConfig
var saveStateForSetupPersistence = saveState
var writeRelayAuthTokenForSetupPersistence = writeRelayAuthToken

func persistInteractiveSetupState(paths runtimePaths, cfg runtimeConfig, state *installState, previousToken string, hadPreviousToken bool, token string, lifecycleMarker ...[]byte) error {
	return persistInteractiveSetupStateWithMode(paths, cfg, state, previousToken, hadPreviousToken, token, false, lifecycleMarker...)
}

func persistInteractiveSetupStateWithMode(paths runtimePaths, cfg runtimeConfig, state *installState, previousToken string, hadPreviousToken bool, token string, retireDevice bool, lifecycleMarker ...[]byte) error {
	return withSetupLifecycleLock(paths, lifecycleMarker, func() error {
		previousDeviceConfig := cfg
		if retireDevice {
			if err := ensureDeviceRetirementProfileID(
				paths,
				&cfg,
			); err != nil {
				return fmt.Errorf(
					"ensure server profile identity before device retirement: %w",
					err,
				)
			}
			var prepareErr error
			previousDeviceConfig, prepareErr =
				prepareDeviceCredentialRetirement(&cfg)
			if prepareErr != nil {
				return prepareErr
			}
			if err := writeDeviceCredentialRetirementCheckpoint(
				paths,
				previousDeviceConfig,
			); err != nil {
				return err
			}
		}
		if err := persistInteractiveSetupStateUnlocked(paths, cfg, state, previousToken, hadPreviousToken, token); err != nil {
			if retireDevice {
				return settleFailedDeviceRetirementPersistence(
					paths,
					previousDeviceConfig,
					err,
				)
			}
			return err
		}
		if retireDevice {
			checkpoint, exists, err :=
				readDeviceCredentialRetirementCheckpoint(paths)
			if err != nil || !exists {
				if err == nil {
					err = errors.New(
						"device retirement checkpoint disappeared",
					)
				}
				return err
			}
			revocationStarted, err :=
				completeCheckpointedDeviceCredentialRetirement(
					paths,
					previousDeviceConfig,
					checkpoint,
				)
			if err != nil {
				if revocationStarted {
					return setupPersistenceError(
						"cannot finish retired device credential cleanup",
						err,
						nil,
					)
				}
				restoreErr := saveConfigForSetupPersistence(
					paths,
					previousDeviceConfig,
				)
				if restoreErr == nil {
					restoreErr =
						clearDeviceCredentialRetirementCheckpoint(paths)
				}
				return setupPersistenceError(
					"cannot retire paired device credential",
					err,
					restoreErr,
				)
			}
		}
		return nil
	})
}

func persistInteractiveSetupStateUnlocked(paths runtimePaths, cfg runtimeConfig, state *installState, previousToken string, hadPreviousToken bool, token string) error {
	configSnapshot, hadConfigSnapshot, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		return fmt.Errorf("cannot snapshot config: %s", err)
	}
	stateSnapshot, hadStateSnapshot, err := readOptionalFile(paths.StateFile)
	if err != nil {
		return fmt.Errorf("cannot snapshot state: %s", err)
	}

	tokenChanged := token != previousToken
	if tokenChanged {
		if err := writeRelayAuthTokenForSetupPersistence(token); err != nil {
			return relayAuthTokenSetupSaveError(err)
		}
	}
	if err := saveConfigForSetupPersistence(paths, cfg); err != nil {
		rollbackErr := errors.Join(
			restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged),
			restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot),
			restoreOptionalFile(paths.StateFile, stateSnapshot, hadStateSnapshot),
		)
		return setupPersistenceError("cannot save config", err, rollbackErr)
	}

	state.Version = localVersion(paths)
	state.InstallSource = detectInstallSource(paths, *state)
	if err := mergeLatestSetupState(paths, state); err != nil {
		rollbackErr := errors.Join(
			restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged),
			restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot),
			restoreOptionalFile(paths.StateFile, stateSnapshot, hadStateSnapshot),
		)
		return setupPersistenceError("cannot merge current state", err, rollbackErr)
	}
	if err := saveStateForSetupPersistence(paths, *state); err != nil {
		rollbackErr := errors.Join(
			restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged),
			restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot),
			restoreOptionalFile(paths.StateFile, stateSnapshot, hadStateSnapshot),
		)
		return setupPersistenceError("cannot save state", err, rollbackErr)
	}
	return nil
}

func setupPersistenceError(context string, cause, rollbackErr error) error {
	if rollbackErr != nil {
		return fmt.Errorf("%s: %w; rollback incomplete: %v", context, cause, rollbackErr)
	}
	return fmt.Errorf("%s: %w", context, cause)
}

func saveConfigBeforeDeviceRetirement(paths runtimePaths, cfg runtimeConfig, save func(runtimePaths, runtimeConfig) error) (runtimeConfig, error) {
	if err := ensureDeviceRetirementProfileID(paths, &cfg); err != nil {
		return cfg, fmt.Errorf(
			"ensure server profile identity before device retirement: %w",
			err,
		)
	}
	previousDeviceConfig, err := prepareDeviceCredentialRetirement(&cfg)
	if err != nil {
		return cfg, err
	}
	if err := writeDeviceCredentialRetirementCheckpoint(
		paths,
		previousDeviceConfig,
	); err != nil {
		return previousDeviceConfig, err
	}
	if err := save(paths, cfg); err != nil {
		return cfg, settleFailedDeviceRetirementPersistence(
			paths,
			previousDeviceConfig,
			err,
		)
	}
	checkpoint, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths)
	if err != nil || !exists {
		if err == nil {
			err = errors.New("device retirement checkpoint disappeared")
		}
		return cfg, err
	}
	revocationStarted, err :=
		completeCheckpointedDeviceCredentialRetirement(
			paths,
			previousDeviceConfig,
			checkpoint,
		)
	if err != nil {
		if revocationStarted {
			return cfg, setupPersistenceError(
				"cannot finish retired device credential cleanup",
				err,
				nil,
			)
		}
		restoreErr := save(paths, previousDeviceConfig)
		if restoreErr == nil {
			restoreErr = clearDeviceCredentialRetirementCheckpoint(paths)
		}
		return previousDeviceConfig, setupPersistenceError(
			"cannot retire paired device credential",
			err,
			restoreErr,
		)
	}
	return cfg, nil
}

func ensureDeviceRetirementProfileID(
	paths runtimePaths,
	cfg *runtimeConfig,
) error {
	if cfg.ProfileID == "" {
		existing, err := loadSelectedRuntimeConfigUnchecked(paths)
		if err == nil && existing.ProfileID != "" {
			cfg.ProfileID = existing.ProfileID
		} else if err != nil && fileExists(paths.ConfigFile) {
			return fmt.Errorf(
				"resolve existing server profile identity: %w",
				err,
			)
		}
	}
	return ensureProfileID(cfg)
}

func settleFailedDeviceRetirementPersistence(
	paths runtimePaths,
	previous runtimeConfig,
	cause error,
) error {
	current, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err == nil && deviceRetirementEndpointsEqual(current, previous) {
		return errors.Join(
			cause,
			clearDeviceCredentialRetirementCheckpoint(paths),
		)
	}
	// Unknown/cleared config state keeps the checkpoint. It is the only durable
	// source for the exact pinned retry after a failed config restoration.
	return cause
}
