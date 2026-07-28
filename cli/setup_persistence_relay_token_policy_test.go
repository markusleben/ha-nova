package main

import (
	"errors"
	"testing"
)

func TestInteractiveSetupRollbackUsesTokenDiscoveredDuringPersistence(
	t *testing.T,
) {
	originalSnapshot := snapshotRelayAuthTokenForSetupPersistence
	originalWrite := writeRelayAuthTokenForSetupPersistence
	originalRestore := restoreRelayAuthTokenForSetupPersistence
	originalSaveConfig := saveConfigForSetupPersistence
	snapshotRelayAuthTokenForSetupPersistence = func(
		string,
		bool,
	) (string, bool, error) {
		return "foreign-previous-token", true, nil
	}
	writeRelayAuthTokenForSetupPersistence = func(string) error {
		return nil
	}
	var restoredToken string
	var restoredExists, restoredChanged bool
	restoreRelayAuthTokenForSetupPersistence = func(
		token string,
		exists, changed bool,
	) error {
		restoredToken = token
		restoredExists = exists
		restoredChanged = changed
		return nil
	}
	saveConfigForSetupPersistence = func(
		runtimePaths,
		runtimeConfig,
	) error {
		return errors.New("test config failure")
	}
	t.Cleanup(func() {
		snapshotRelayAuthTokenForSetupPersistence = originalSnapshot
		writeRelayAuthTokenForSetupPersistence = originalWrite
		restoreRelayAuthTokenForSetupPersistence = originalRestore
		saveConfigForSetupPersistence = originalSaveConfig
	})

	err := persistInteractiveSetupStateUnlocked(
		runtimePaths{
			ConfigDir:  t.TempDir(),
			ConfigFile: t.TempDir() + "/config.json",
			StateFile:  t.TempDir() + "/state.json",
		},
		runtimeConfig{},
		&installState{},
		"",
		false,
		"new-token",
	)
	if err == nil ||
		restoredToken != "foreign-previous-token" ||
		!restoredExists ||
		!restoredChanged {
		t.Fatalf(
			"rollback err=%v token=%q exists=%v changed=%v",
			err,
			restoredToken,
			restoredExists,
			restoredChanged,
		)
	}
}
