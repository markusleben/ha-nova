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
			previousDeviceConfig = prepareDeviceCredentialRetirement(&cfg)
		}
		if err := persistInteractiveSetupStateUnlocked(paths, cfg, state, previousToken, hadPreviousToken, token); err != nil {
			return err
		}
		if retireDevice {
			finalizeDeviceCredentialRetirement(previousDeviceConfig)
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
	previousDeviceConfig := prepareDeviceCredentialRetirement(&cfg)
	if err := save(paths, cfg); err != nil {
		return cfg, err
	}
	finalizeDeviceCredentialRetirement(previousDeviceConfig)
	return cfg, nil
}
