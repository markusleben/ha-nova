package main

import "fmt"

var saveConfigForSetupPersistence = saveConfig
var saveStateForSetupPersistence = saveState
var writeRelayAuthTokenForSetupPersistence = writeRelayAuthToken

func persistInteractiveSetupState(paths runtimePaths, cfg runtimeConfig, state *installState, previousToken string, hadPreviousToken bool, token string) error {
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
			return fmt.Errorf("cannot save relay token: %s", err)
		}
	}
	if err := saveConfigForSetupPersistence(paths, cfg); err != nil {
		restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged)
		restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot)
		restoreOptionalFile(paths.StateFile, stateSnapshot, hadStateSnapshot)
		return fmt.Errorf("cannot save config: %s", err)
	}

	state.Version = localVersion(paths)
	state.InstallSource = detectInstallSource(paths, *state)
	if err := saveStateForSetupPersistence(paths, *state); err != nil {
		restoreRelayAuthToken(previousToken, hadPreviousToken, tokenChanged)
		restoreOptionalFile(paths.ConfigFile, configSnapshot, hadConfigSnapshot)
		restoreOptionalFile(paths.StateFile, stateSnapshot, hadStateSnapshot)
		return fmt.Errorf("cannot save state: %s", err)
	}
	return nil
}
