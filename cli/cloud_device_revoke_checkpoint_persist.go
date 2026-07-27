package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// checkpointCloudDeviceRevocationUnlocked persists the exact device ids whose
// remote revocations completed. Callers hold the client mutation lock. The
// profile snapshot prevents a concurrent or stale cleanup from authorizing
// deletion of a newly written device credential.
func checkpointCloudDeviceRevocationUnlocked(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	checkpoint cloudDeviceRevocationCheckpoint,
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
			"missing Cloud device revocation profile snapshot",
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
			"server profile %q changed before device revocation checkpoint",
			expected.profileName,
		)
	}
	if cfg.Cloud == nil {
		return runtimeConfig{}, expected, errors.New(
			"Cloud lifecycle disappeared before device revocation checkpoint",
		)
	}
	if cfg.Cloud.DeviceRevocationCompleted != nil {
		if *cfg.Cloud.DeviceRevocationCompleted == checkpoint {
			return cfg, expected, nil
		}
		return runtimeConfig{}, expected, errors.New(
			"a different Cloud device revocation checkpoint already exists",
		)
	}
	lifecycle := *cfg.Cloud
	checkpointCopy := checkpoint
	lifecycle.DeviceRevocationCompleted = &checkpointCopy
	if err := validateCloudLifecycle(lifecycle); err != nil {
		return runtimeConfig{}, expected, fmt.Errorf(
			"validate Cloud device revocation checkpoint: %w",
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
		"device_revocation_completed",
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

func cloudProfileRawAfterCheckpointWrite(
	path string,
	profileName string,
) ([]byte, error) {
	doc, err := loadConfigDocument(path)
	if err != nil {
		return nil, err
	}
	return cloudRecoveryProfileRaw(doc, profileName)
}

func rejectCloudSetupDuringDeviceRevocation(cfg runtimeConfig) error {
	if cfg.Cloud == nil || !cfg.Cloud.cleanupPending() {
		return nil
	}
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: cfg.Cloud.cleanupPendingDetail() +
			"; reconnect only after cleanup",
	}
}
