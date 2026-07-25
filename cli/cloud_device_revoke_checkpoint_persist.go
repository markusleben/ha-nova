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
	cfg.Cloud = &lifecycle
	top, err := doc.withProfilePreservingSiblings(expected.profileName, cfg)
	if err != nil {
		return runtimeConfig{}, expected, err
	}
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		return runtimeConfig{}, expected, err
	}
	raw, err := cloudProfileRawFromDocumentMap(
		top,
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

func cloudProfileRawFromDocumentMap(
	top map[string]json.RawMessage,
	profileName string,
) (json.RawMessage, error) {
	rawServers, hasServers := top["servers"]
	if !hasServers {
		return json.Marshal(top)
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(rawServers, &servers); err != nil {
		return nil, err
	}
	raw, ok := servers[profileName]
	if !ok {
		return nil, fmt.Errorf(
			"server profile %q disappeared after device revocation checkpoint",
			profileName,
		)
	}
	return raw, nil
}

func rejectCloudSetupDuringDeviceRevocation(cfg runtimeConfig) error {
	if cfg.Cloud == nil || cfg.Cloud.DeviceRevocationCompleted == nil {
		return nil
	}
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: "Cloud device revocation already completed; finish Cloud " +
			"cleanup before reconnecting this profile",
	}
}
