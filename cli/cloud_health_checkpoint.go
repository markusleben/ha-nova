package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type cloudManagementSnapshot struct {
	Config      runtimeConfig
	ProfileName string
	ProfileRaw  json.RawMessage
}

func loadCloudManagementSnapshot(
	paths runtimePaths,
) (cloudManagementSnapshot, error) {
	return loadCloudSnapshot(paths, true)
}

func loadCloudRecoverySnapshotUnchecked(
	paths runtimePaths,
) (cloudManagementSnapshot, error) {
	return loadCloudSnapshot(paths, false)
}

func loadCloudSnapshot(
	paths runtimePaths,
	validateManagement bool,
) (cloudManagementSnapshot, error) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return cloudManagementSnapshot{}, fmt.Errorf(
				"cannot read HA NOVA server configuration %s: %w; restore or repair the file before retrying",
				paths.ConfigFile,
				err,
			)
		}
		return cloudManagementSnapshot{}, fmt.Errorf(
			"HA NOVA is not set up yet. Run: ha-nova setup: %w",
			err,
		)
	}
	if validateManagement {
		if err := validateSupportedConfigDocument(doc); err != nil {
			return cloudManagementSnapshot{}, err
		}
	} else if err := validateSupportedConfigSchema(doc); err != nil {
		return cloudManagementSnapshot{}, err
	}
	if err := validateExistingServerProfileIDs(doc.servers); err != nil {
		return cloudManagementSnapshot{}, fmt.Errorf(
			"invalid server profile identities: %w",
			err,
		)
	}
	name, err := resolveSelectedServerProfile(doc)
	if err != nil {
		return cloudManagementSnapshot{}, fmt.Errorf(
			"invalid selected server profile: %w",
			err,
		)
	}
	if err := validateServerProfileName(name); err != nil {
		return cloudManagementSnapshot{}, fmt.Errorf(
			"invalid selected server profile: %w",
			err,
		)
	}
	cfg, ok := doc.flatProfile(name)
	if !ok {
		return cloudManagementSnapshot{}, fmt.Errorf(
			"server profile %q does not exist",
			name,
		)
	}
	setActiveServerProfile(name)
	if cfg.ProfileID != "" {
		if err := validateProfileID(cfg.ProfileID); err != nil {
			return cloudManagementSnapshot{}, err
		}
	}
	if validateManagement {
		if err := validateLoadedRuntimeConfig(&cfg); err != nil {
			return cloudManagementSnapshot{}, fmt.Errorf(
				"invalid server profile %q: %w",
				name,
				err,
			)
		}
	}
	raw, err := cloudRecoveryProfileRaw(doc, name)
	if err != nil {
		return cloudManagementSnapshot{}, err
	}
	return cloudManagementSnapshot{
		Config:      cfg,
		ProfileName: name,
		ProfileRaw:  append(json.RawMessage(nil), raw...),
	}, nil
}

func (snapshot cloudManagementSnapshot) recoveryExpectation() (cloudRecoveryCheckpointExpectation, error) {
	if err := validateProfileID(snapshot.Config.ProfileID); err != nil {
		return cloudRecoveryCheckpointExpectation{}, err
	}
	return newCloudRecoveryCheckpointExpectation(
		snapshot.ProfileName,
		snapshot.Config.ProfileID,
		snapshot.ProfileRaw,
	), nil
}

func verifyCloudHealthAtSnapshot(
	ctx context.Context,
	paths runtimePaths,
	snapshot cloudManagementSnapshot,
	verify func(context.Context, runtimeConfig) error,
	replaceHold *cloudRecoveryHold,
) error {
	if verify == nil {
		return fmt.Errorf("Cloud health verifier is unavailable")
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		return err
	}
	release, acquired := acquireAutoRepairLockUntil(
		paths,
		time.Now().Add(cloudRecoveryCheckpointLockTimeout),
	)
	if !acquired {
		return fmt.Errorf(
			"another HA NOVA client update is still in progress",
		)
	}
	defer release()
	current, err := cloudHealthSnapshotCurrent(paths, expected)
	if err != nil {
		return cloudHealthSnapshotChangedProblem(err)
	}
	if !current {
		return cloudHealthSnapshotChangedProblem(nil)
	}
	if replaceHold != nil {
		lifecycle := snapshot.Config.Cloud
		if lifecycle == nil ||
			!lifecycle.ready() ||
			lifecycle.RecoveryHold == nil ||
			*lifecycle.RecoveryHold != *replaceHold ||
			!cloudRecoveryHoldClearsAfterUnlock(replaceHold) {
			return fmt.Errorf(
				"Cloud recovery hold can only be cleared after ready current Cloud health is verified",
			)
		}
	}
	verifyErr := verify(ctx, snapshot.Config)
	if verifyErr != nil &&
		cloudRecoveryHoldForProblem(cloudProblemForError(verifyErr)) == nil {
		return verifyErr
	}
	if verifyErr != nil {
		outcome, holdErr := checkpointCloudRecoveryHoldReplacingUnlocked(
			paths,
			expected,
			verifyErr,
			replaceHold,
		)
		if holdErr != nil {
			current, currentErr := cloudHealthSnapshotCurrent(
				paths,
				expected,
			)
			if currentErr != nil || !current {
				return cloudHealthSnapshotChangedProblem(errors.Join(
					verifyErr,
					holdErr,
					currentErr,
				))
			}
			return errorsJoinRecoveryCheckpoint(verifyErr, holdErr)
		}
		if outcome == cloudRecoveryCheckpointSkippedStale {
			return cloudHealthSnapshotChangedProblem(verifyErr)
		}
		return verifyErr
	}
	current, err = cloudHealthSnapshotCurrent(paths, expected)
	if err != nil {
		return cloudHealthSnapshotChangedProblem(err)
	}
	if !current {
		return cloudHealthSnapshotChangedProblem(nil)
	}
	return nil
}

func loadAndVerifyCloudHealthWithCheckpoint(
	ctx context.Context,
	paths runtimePaths,
	verify func(context.Context, runtimeConfig) error,
	replaceHold *cloudRecoveryHold,
) (cloudManagementSnapshot, error) {
	snapshot, err := loadCloudManagementSnapshot(paths)
	if err != nil {
		return cloudManagementSnapshot{}, err
	}
	if replaceHold != nil {
		current := snapshot.Config.Cloud
		if current == nil ||
			current.RecoveryHold == nil ||
			*current.RecoveryHold != *replaceHold {
			return cloudManagementSnapshot{}, fmt.Errorf(
				"Cloud recovery hold changed before verification started",
			)
		}
	}
	return snapshot, verifyCloudHealthAtSnapshot(
		ctx,
		paths,
		snapshot,
		verify,
		replaceHold,
	)
}

func errorsJoinRecoveryCheckpoint(cause, checkpointErr error) error {
	return errors.Join(
		cause,
		fmt.Errorf(
			"persist Cloud recovery safety hold: %w",
			checkpointErr,
		),
	)
}

func cloudHealthSnapshotCurrent(
	paths runtimePaths,
	expected cloudRecoveryCheckpointExpectation,
) (bool, error) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return false, err
	}
	raw, err := cloudRecoveryProfileRaw(doc, expected.profileName)
	if err != nil {
		return false, nil
	}
	cfg, ok := doc.flatProfile(expected.profileName)
	return ok &&
		cfg.ProfileID == expected.profileID &&
		bytes.Equal(
			bytes.TrimSpace(raw),
			bytes.TrimSpace(expected.profileRaw),
		), nil
}

func cloudHealthSnapshotChangedProblem(cause error) error {
	return &cloudProblem{
		Code:        cloudProblemIdentityMismatch,
		Remediation: cloudRemediationSecurityStop,
		Detail:      "the server profile changed during Cloud health verification; no result was accepted",
		Cause:       cause,
	}
}
