package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// cloudRecoveryHold is non-secret durable safety state. It survives process
// restarts so an uncertain write or identity stop cannot degrade into generic
// add/reconnect guidance on the next command.
type cloudRecoveryHold struct {
	Code            cloudProblemCode `json:"code"`
	Remediation     cloudRemediation `json:"remediation"`
	StorageVerified bool             `json:"storage_verified,omitempty"`
}

func (hold *cloudRecoveryHold) UnmarshalJSON(data []byte) error {
	type recoveryHoldFields cloudRecoveryHold
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var fields recoveryHoldFields
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("cloud recovery hold contains trailing JSON")
		}
		return err
	}
	parsed := cloudRecoveryHold(fields)
	if err := validateCloudRecoveryHold(&parsed); err != nil {
		return err
	}
	*hold = parsed
	return nil
}

func validateCloudRecoveryHold(hold *cloudRecoveryHold) error {
	if hold == nil {
		return nil
	}
	switch hold.Remediation {
	case cloudRemediationSecurityStop:
		if hold.StorageVerified {
			return errors.New(
				"security-stop recovery hold cannot verify storage",
			)
		}
		if hold.Code != cloudProblemAuthorization &&
			hold.Code != cloudProblemIdentityMismatch {
			return fmt.Errorf(
				"invalid security-stop recovery hold code %q",
				hold.Code,
			)
		}
	case cloudRemediationVerifyState:
		if hold.Code != cloudProblemSecureStorage &&
			hold.Code != cloudProblemUnavailable {
			return fmt.Errorf(
				"invalid verify-state recovery hold code %q",
				hold.Code,
			)
		}
		if hold.StorageVerified &&
			hold.Code != cloudProblemSecureStorage {
			return errors.New(
				"only a secure-storage recovery hold can verify storage",
			)
		}
	default:
		return fmt.Errorf(
			"invalid cloud recovery hold remediation %q",
			hold.Remediation,
		)
	}
	return nil
}

func cloudRecoveryHoldForProblem(
	problem *cloudProblem,
) *cloudRecoveryHold {
	if !cloudProblemBlocksMutationRecovery(problem) {
		return nil
	}
	hold := &cloudRecoveryHold{
		Code:        problem.Code,
		Remediation: problem.Remediation,
	}
	if validateCloudRecoveryHold(hold) != nil {
		return nil
	}
	return hold
}

func cloudRecoveryHoldCanCreateCheckpoint(
	hold *cloudRecoveryHold,
) bool {
	if hold == nil {
		return false
	}
	if hold.Remediation == cloudRemediationVerifyState {
		return true
	}
	// Authorization uncertainty can follow a persisted profile identity before
	// the lifecycle itself is saved. Identity conflicts discovered before that
	// checkpoint must leave the existing local setup untouched.
	return hold.Remediation == cloudRemediationSecurityStop &&
		hold.Code == cloudProblemAuthorization
}

func cloudProblemFromRecoveryHold(
	hold *cloudRecoveryHold,
) *cloudProblem {
	if hold == nil {
		return nil
	}
	if validateCloudRecoveryHold(hold) != nil {
		return nil
	}
	detail := "Cloud recovery is paused until the saved state is explicitly cleaned up"
	if hold.Remediation == cloudRemediationSecurityStop {
		detail = "Cloud recovery is paused for security review; remove the saved Cloud access before starting another authorization"
	} else if hold.Code == cloudProblemSecureStorage {
		if hold.StorageVerified {
			detail = "native secure storage was verified; remove the saved Cloud state before continuing"
		} else {
			detail = "a secure-storage update had an uncertain outcome; verify native secure storage before continuing"
		}
	} else {
		detail = "a Cloud request had an uncertain outcome; verify or remove the saved state before continuing"
	}
	return &cloudProblem{
		Code:        hold.Code,
		Remediation: hold.Remediation,
		Detail:      detail,
	}
}

func cloudRecoveryHoldProblem(cfg runtimeConfig) *cloudProblem {
	if cfg.Cloud == nil {
		return nil
	}
	return cloudProblemFromRecoveryHold(cfg.Cloud.RecoveryHold)
}

func persistCloudRecoveryHoldForError(
	cfg *runtimeConfig,
	cause error,
	save cloudConfigSaver,
) error {
	if cause == nil {
		return nil
	}
	problem := cloudProblemForError(cause)
	hold := cloudRecoveryHoldForProblem(problem)
	if hold == nil || cfg == nil || save == nil ||
		validateProfileID(cfg.ProfileID) != nil {
		return cause
	}
	if cfg.Cloud == nil && !cloudRecoveryHoldCanCreateCheckpoint(hold) {
		return cause
	}
	if err := setCloudRecoveryHold(cfg, hold, save); err != nil {
		return errors.Join(
			cause,
			fmt.Errorf("persist Cloud recovery hold: %w", err),
		)
	}
	return cause
}

func setCloudRecoveryHold(
	cfg *runtimeConfig,
	hold *cloudRecoveryHold,
	save cloudConfigSaver,
) error {
	if cfg == nil {
		return nil
	}
	if err := validateCloudRecoveryHold(hold); err != nil {
		return err
	}
	if err := validateProfileID(cfg.ProfileID); err != nil {
		return err
	}
	previousLifecycle := cfg.Cloud
	if cfg.Cloud == nil {
		cfg.Cloud = &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
		}
	}
	existing := cfg.Cloud.RecoveryHold
	if existing != nil {
		if *existing == *hold {
			return nil
		}
		return fmt.Errorf(
			"refusing to replace recovery hold %q/%q with %q/%q",
			existing.Code,
			existing.Remediation,
			hold.Code,
			hold.Remediation,
		)
	}
	lifecycle := *cfg.Cloud
	lifecycle.RecoveryHold = hold
	cfg.Cloud = &lifecycle
	if err := save(*cfg); err != nil {
		cfg.Cloud = previousLifecycle
		return err
	}
	return nil
}

func cloudRecoveryHoldClearsAfterUnlock(
	hold *cloudRecoveryHold,
) bool {
	return hold != nil &&
		hold.Code == cloudProblemSecureStorage &&
		hold.Remediation == cloudRemediationVerifyState
}

func cloudRecoveryHoldNeedsUnlockForConfig(
	cfg runtimeConfig,
) bool {
	if cfg.Cloud == nil ||
		!cloudRecoveryHoldClearsAfterUnlock(cfg.Cloud.RecoveryHold) {
		return false
	}
	if !cfg.Cloud.RecoveryHold.StorageVerified {
		return true
	}
	if validateClientInstallID(cfg.ClientInstallID) != nil {
		return false
	}
	return cfg.Cloud.ready() && cloudRemoteFeatureAvailable()
}

func cloudRecoveryHoldCanResetStorageVerification(
	existing *cloudRecoveryHold,
	replacement *cloudRecoveryHold,
) bool {
	if existing == nil || replacement == nil ||
		!existing.StorageVerified ||
		replacement.StorageVerified {
		return false
	}
	unverified := *existing
	unverified.StorageVerified = false
	return unverified == *replacement &&
		cloudRecoveryHoldClearsAfterUnlock(replacement)
}
