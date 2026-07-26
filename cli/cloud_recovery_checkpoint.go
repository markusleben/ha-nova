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
	problem := cloudProblemForError(cause)
	hold := cloudRecoveryHoldForProblem(problem)
	resetVerifiedStorageOnly := false
	if hold == nil &&
		problem.Code == cloudProblemSecureStorage &&
		problem.Remediation == cloudRemediationUnlockStorage {
		hold = &cloudRecoveryHold{
			Code:        cloudProblemSecureStorage,
			Remediation: cloudRemediationVerifyState,
		}
		resetVerifiedStorageOnly = true
	}
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
		resetVerifiedStorage :=
			cloudRecoveryHoldCanResetStorageVerification(
				cfg.Cloud.RecoveryHold,
				hold,
			)
		if !resetVerifiedStorage &&
			(replaceHold == nil ||
				*cfg.Cloud.RecoveryHold != *replaceHold ||
				!cloudRecoveryHoldClearsAfterUnlock(replaceHold)) {
			return cloudRecoveryCheckpointSkippedStale, fmt.Errorf(
				"server profile %q already has a different recovery hold",
				expected.profileName,
			)
		}
	} else if resetVerifiedStorageOnly {
		return cloudRecoveryCheckpointNotApplicable, nil
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

func writeCloudRecoveryHoldRaw(
	paths runtimePaths,
	doc *configDocument,
	profileName string,
	profileRaw json.RawMessage,
	hold *cloudRecoveryHold,
) error {
	var rawHold json.RawMessage
	if hold != nil {
		var err error
		rawHold, err = json.Marshal(hold)
		if err != nil {
			return err
		}
	}
	return writeCloudLifecycleFieldRaw(
		paths,
		doc,
		profileName,
		profileRaw,
		"recovery_hold",
		rawHold,
	)
}

func writeCloudLifecycleFieldRaw(
	paths runtimePaths,
	doc *configDocument,
	profileName string,
	profileRaw json.RawMessage,
	field string,
	value json.RawMessage,
) error {
	if doc == nil || len(doc.source) == 0 {
		return errors.New(
			"Cloud lifecycle checkpoint requires an exact config generation",
		)
	}
	switch field {
	case "recovery_hold",
		"device_revocation_completed",
		"authorization_revocation_completed":
	default:
		return fmt.Errorf("unsupported Cloud lifecycle checkpoint field %q", field)
	}
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
	if value == nil {
		delete(lifecycle, field)
	} else {
		lifecycle[field] = value
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
		return writeJSONFileIfUnchanged(
			paths.ConfigFile,
			top,
			0o600,
			doc.source,
		)
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
	return writeJSONFileIfUnchanged(
		paths.ConfigFile,
		top,
		0o600,
		doc.source,
	)
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
