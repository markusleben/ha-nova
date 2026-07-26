package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// checkpointCloudAuthorizationRevocationUnlocked persists proof that remote
// OAuth revocation completed before local secure-storage deletion starts.
// Callers hold the client mutation lock and provide the exact profile snapshot.
func checkpointCloudAuthorizationRevocationUnlocked(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	plan cloudAuthorizationCleanupPlan,
	ownerConfirmed bool,
) (
	runtimeConfig,
	cloudRecoveryCheckpointExpectation,
	error,
) {
	if err := validateServerProfileName(expected.profileName); err != nil {
		return runtimeConfig{}, expected, err
	}
	if err := validateProfileID(expected.profileID); err != nil {
		return runtimeConfig{}, expected, err
	}
	if len(bytes.TrimSpace(expected.profileRaw)) == 0 {
		return runtimeConfig{}, expected, errors.New(
			"missing Cloud authorization revocation profile snapshot",
		)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return runtimeConfig{}, expected, err
	}
	currentRaw, err := cloudRecoveryProfileRaw(doc, expected.profileName)
	if err != nil {
		return runtimeConfig{}, expected, err
	}
	cfg, ok := doc.flatProfile(expected.profileName)
	if !ok ||
		cfg.ProfileID != expected.profileID ||
		!bytes.Equal(
			bytes.TrimSpace(currentRaw),
			bytes.TrimSpace(expected.profileRaw),
		) {
		return runtimeConfig{}, expected, fmt.Errorf(
			"server profile %q changed before authorization revocation checkpoint",
			expected.profileName,
		)
	}
	if cfg.Cloud == nil {
		return runtimeConfig{}, expected, errors.New(
			"Cloud lifecycle disappeared before authorization revocation checkpoint",
		)
	}
	if existing := cfg.Cloud.AuthorizationRevocationCompleted; existing != nil {
		if err := plan.validateRevocationCheckpoint(existing); err != nil {
			return runtimeConfig{}, expected, err
		}
		return cfg, expected, nil
	}
	checkpoint := newCloudAuthorizationRevocationCheckpoint(
		plan,
		ownerConfirmed,
	)
	if checkpoint == nil {
		return cfg, expected, nil
	}
	if err := validateCloudAuthorizationRevocationCheckpoint(
		checkpoint,
	); err != nil {
		return runtimeConfig{}, expected, err
	}
	lifecycle := *cfg.Cloud
	lifecycle.AuthorizationRevocationCompleted = checkpoint
	if err := validateCloudLifecycle(lifecycle); err != nil {
		return runtimeConfig{}, expected, fmt.Errorf(
			"validate Cloud authorization revocation checkpoint: %w",
			err,
		)
	}
	rawCheckpoint, err := json.Marshal(checkpoint)
	if err != nil {
		return runtimeConfig{}, expected, err
	}
	if err := writeCloudLifecycleFieldRaw(
		paths,
		doc,
		expected.profileName,
		currentRaw,
		"authorization_revocation_completed",
		rawCheckpoint,
	); err != nil {
		return runtimeConfig{}, expected, err
	}
	cfg.Cloud = &lifecycle
	raw, err := cloudProfileRawAfterCheckpointWrite(
		paths.ConfigFile,
		expected.profileName,
	)
	if err != nil {
		return runtimeConfig{}, expected, err
	}
	return cfg, newCloudRecoveryCheckpointExpectation(
		expected.profileName,
		expected.profileID,
		raw,
	), nil
}
