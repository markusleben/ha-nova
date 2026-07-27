package main

import (
	"bytes"
	"errors"
	"fmt"
	"time"
)

func markCloudRecoveryStorageVerifiedAtSnapshot(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	hold cloudRecoveryHold,
) (runtimeConfig, error) {
	if !cloudRecoveryHoldClearsAfterUnlock(&hold) {
		return runtimeConfig{}, errors.New(
			"Cloud recovery hold does not support storage verification",
		)
	}
	if hold.StorageVerified {
		return runtimeConfig{}, errors.New(
			"Cloud recovery storage is already verified",
		)
	}
	verified := hold
	verified.StorageVerified = true
	return transitionCloudRecoveryHoldAtSnapshot(
		paths,
		expected,
		hold,
		&verified,
		false,
	)
}

func clearCloudRecoveryHoldAtSnapshot(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	hold cloudRecoveryHold,
) (runtimeConfig, error) {
	return transitionCloudRecoveryHoldAtSnapshot(
		paths,
		expected,
		hold,
		nil,
		true,
	)
}

func transitionCloudRecoveryHoldAtSnapshot(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	hold cloudRecoveryHold,
	next *cloudRecoveryHold,
	requireReady bool,
) (runtimeConfig, error) {
	if err := validateCloudRecoveryHold(next); err != nil {
		return runtimeConfig{}, err
	}
	release, acquired := acquireAutoRepairLockUntil(
		paths,
		time.Now().Add(cloudRecoveryCheckpointLockTimeout),
	)
	if !acquired {
		return runtimeConfig{}, fmt.Errorf(
			"another HA NOVA client update is still in progress",
		)
	}
	defer release()

	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return runtimeConfig{}, err
	}
	currentRaw, err := cloudRecoveryProfileRaw(
		doc,
		expected.profileName,
	)
	if err != nil {
		return runtimeConfig{}, err
	}
	cfg, ok := doc.flatProfile(expected.profileName)
	if !ok ||
		cfg.ProfileID != expected.profileID ||
		!bytes.Equal(
			bytes.TrimSpace(currentRaw),
			bytes.TrimSpace(expected.profileRaw),
		) {
		return runtimeConfig{}, fmt.Errorf(
			"server profile %q changed before recovery hold transition",
			expected.profileName,
		)
	}
	if cfg.Cloud == nil || cfg.Cloud.RecoveryHold == nil {
		return runtimeConfig{}, errors.New(
			"Cloud recovery hold disappeared before verification completed",
		)
	}
	if *cfg.Cloud.RecoveryHold != hold {
		return runtimeConfig{}, errors.New(
			"Cloud recovery hold changed before verification completed",
		)
	}
	if !cloudRecoveryHoldClearsAfterUnlock(
		cfg.Cloud.RecoveryHold,
	) {
		return runtimeConfig{}, errors.New(
			"Cloud recovery hold requires explicit cleanup",
		)
	}
	if requireReady && !cfg.Cloud.ready() {
		return runtimeConfig{}, errors.New(
			"Cloud recovery hold cannot be cleared before current Cloud access is ready",
		)
	}
	if err := writeCloudRecoveryHoldRaw(
		paths,
		doc,
		expected.profileName,
		currentRaw,
		next,
	); err != nil {
		return runtimeConfig{}, err
	}
	lifecycle := *cfg.Cloud
	lifecycle.RecoveryHold = next
	cfg.Cloud = &lifecycle
	return cfg, nil
}
