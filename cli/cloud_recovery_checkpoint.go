package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type cloudRecoveryCheckpointExpectation struct {
	profileName string
	profileID   string
	profileRaw  json.RawMessage
}

type cloudRecoveryCheckpointOutcome string

const (
	cloudRecoveryCheckpointNotApplicable  cloudRecoveryCheckpointOutcome = "not_applicable"
	cloudRecoveryCheckpointPersisted      cloudRecoveryCheckpointOutcome = "persisted"
	cloudRecoveryCheckpointAlreadyPresent cloudRecoveryCheckpointOutcome = "already_present"
	cloudRecoveryCheckpointSkippedStale   cloudRecoveryCheckpointOutcome = "skipped_stale"
)

func newCloudRecoveryCheckpointExpectation(
	profileName string,
	profileID string,
	raw json.RawMessage,
) cloudRecoveryCheckpointExpectation {
	return cloudRecoveryCheckpointExpectation{
		profileName: profileName,
		profileID:   profileID,
		profileRaw:  append(json.RawMessage(nil), raw...),
	}
}

func checkpointCloudRecoveryHold(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	cause error,
) (cloudRecoveryCheckpointOutcome, error) {
	return checkpointCloudRecoveryHoldReplacing(paths, expected, cause, nil)
}

const cloudRecoveryCheckpointLockTimeout = 2 * time.Second

func checkpointCloudRecoveryHoldReplacing(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	cause error,
	replaceHold *cloudRecoveryHold,
) (cloudRecoveryCheckpointOutcome, error) {
	var outcome cloudRecoveryCheckpointOutcome
	release, acquired := acquireAutoRepairLockUntil(
		paths,
		time.Now().Add(cloudRecoveryCheckpointLockTimeout),
	)
	if !acquired {
		return outcome, fmt.Errorf(
			"another HA NOVA client update is still in progress",
		)
	}
	defer release()
	var err error
	outcome, err = checkpointCloudRecoveryHoldReplacingUnlocked(
		paths,
		expected,
		cause,
		replaceHold,
	)
	return outcome, err
}

// checkpointCloudRecoveryHoldUnlocked is for callers already holding the
// client mutation lock. The exact profile snapshot binds the observed failure
// to its lifecycle and credential generations; a newer profile is never held.
func checkpointCloudRecoveryHoldUnlocked(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	cause error,
) (cloudRecoveryCheckpointOutcome, error) {
	return checkpointCloudRecoveryHoldReplacingUnlocked(
		paths,
		expected,
		cause,
		nil,
	)
}

func checkpointCloudRecoveryHoldReplacingUnlocked(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	cause error,
	replaceHold *cloudRecoveryHold,
) (cloudRecoveryCheckpointOutcome, error) {
	hold := cloudRecoveryHoldForProblem(cloudProblemForError(cause))
	if hold == nil {
		return cloudRecoveryCheckpointNotApplicable, nil
	}
	if err := validateServerProfileName(expected.profileName); err != nil {
		return cloudRecoveryCheckpointSkippedStale, err
	}
	if err := validateProfileID(expected.profileID); err != nil {
		return cloudRecoveryCheckpointSkippedStale, err
	}
	if len(bytes.TrimSpace(expected.profileRaw)) == 0 {
		return cloudRecoveryCheckpointSkippedStale, errors.New(
			"missing Cloud recovery profile snapshot",
		)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return cloudRecoveryCheckpointSkippedStale, err
	}
	currentRaw, err := cloudRecoveryProfileRaw(doc, expected.profileName)
	if err != nil {
		return cloudRecoveryCheckpointSkippedStale, nil
	}
	cfg, ok := doc.flatProfile(expected.profileName)
	if !ok ||
		cfg.ProfileID != expected.profileID ||
		!bytes.Equal(bytes.TrimSpace(currentRaw), bytes.TrimSpace(expected.profileRaw)) {
		return cloudRecoveryCheckpointSkippedStale, nil
	}
	if cfg.Cloud == nil && !cloudRecoveryHoldCanCreateCheckpoint(hold) {
		return cloudRecoveryCheckpointNotApplicable, nil
	}
	if cfg.Cloud != nil && cfg.Cloud.RecoveryHold != nil {
		if *cfg.Cloud.RecoveryHold == *hold {
			return cloudRecoveryCheckpointAlreadyPresent, nil
		}
		if replaceHold == nil ||
			*cfg.Cloud.RecoveryHold != *replaceHold ||
			!cloudRecoveryHoldClearsAfterUnlock(replaceHold) {
			return cloudRecoveryCheckpointSkippedStale, fmt.Errorf(
				"server profile %q already has a different recovery hold",
				expected.profileName,
			)
		}
	}
	if err := writeCloudRecoveryHoldRaw(
		paths,
		doc,
		expected.profileName,
		currentRaw,
		hold,
	); err != nil {
		return cloudRecoveryCheckpointSkippedStale, err
	}
	return cloudRecoveryCheckpointPersisted, nil
}

func clearCloudRecoveryHoldAtSnapshot(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
	hold cloudRecoveryHold,
) (runtimeConfig, error) {
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
	currentRaw, err := cloudRecoveryProfileRaw(doc, expected.profileName)
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
			"server profile %q changed before recovery hold clearance",
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
	if !cloudRecoveryHoldClearsAfterUnlock(cfg.Cloud.RecoveryHold) {
		return runtimeConfig{}, errors.New(
			"Cloud recovery hold requires explicit cleanup",
		)
	}
	if !cfg.Cloud.ready() {
		return runtimeConfig{}, errors.New(
			"Cloud recovery hold cannot be cleared before current Cloud access is ready",
		)
	}
	if err := writeCloudRecoveryHoldRaw(
		paths,
		doc,
		expected.profileName,
		currentRaw,
		nil,
	); err != nil {
		return runtimeConfig{}, err
	}
	lifecycle := *cfg.Cloud
	lifecycle.RecoveryHold = nil
	cfg.Cloud = &lifecycle
	return cfg, nil
}

func writeCloudRecoveryHoldRaw(
	paths runtimePaths,
	doc *configDocument,
	profileName string,
	profileRaw json.RawMessage,
	hold *cloudRecoveryHold,
) error {
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(profileRaw, &profile); err != nil || profile == nil {
		return fmt.Errorf("invalid server profile %q", profileName)
	}
	var lifecycle map[string]json.RawMessage
	rawCloud, hasCloud := profile["cloud"]
	if hasCloud && !bytes.Equal(bytes.TrimSpace(rawCloud), []byte("null")) {
		if err := json.Unmarshal(rawCloud, &lifecycle); err != nil ||
			lifecycle == nil {
			return fmt.Errorf("invalid Cloud lifecycle for server %q", profileName)
		}
	} else {
		lifecycle = map[string]json.RawMessage{
			"state": json.RawMessage(`"authorizing"`),
		}
	}
	if hold == nil {
		delete(lifecycle, "recovery_hold")
	} else {
		rawHold, err := json.Marshal(hold)
		if err != nil {
			return err
		}
		lifecycle["recovery_hold"] = rawHold
	}
	cloudRaw, err := json.Marshal(lifecycle)
	if err != nil {
		return err
	}
	profile["cloud"] = cloudRaw
	updatedProfile, err := json.Marshal(profile)
	if err != nil {
		return err
	}

	top := make(map[string]json.RawMessage, len(doc.top))
	for key, value := range doc.top {
		top[key] = value
	}
	if doc.servers == nil {
		for key := range top {
			delete(top, key)
		}
		for key, value := range profile {
			top[key] = value
		}
		return writeJSONFile(paths.ConfigFile, top, 0o600)
	}
	servers := make(map[string]json.RawMessage, len(doc.servers))
	for name, raw := range doc.servers {
		servers[name] = raw
	}
	servers[profileName] = updatedProfile
	top["servers"], err = json.Marshal(servers)
	if err != nil {
		return err
	}
	return writeJSONFile(paths.ConfigFile, top, 0o600)
}

func cloudRecoveryProfileRaw(
	doc *configDocument,
	profileName string,
) (json.RawMessage, error) {
	if doc == nil {
		return nil, errors.New("missing configuration document")
	}
	if doc.servers == nil {
		if profileName != defaultServerProfileName {
			return nil, fmt.Errorf("server profile %q does not exist", profileName)
		}
		raw, err := json.Marshal(doc.top)
		return raw, err
	}
	raw, ok := doc.servers[profileName]
	if !ok {
		return nil, fmt.Errorf("server profile %q does not exist", profileName)
	}
	return raw, nil
}
