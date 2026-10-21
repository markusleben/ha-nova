package main

import (
	"errors"
	"fmt"
	"os"
)

func ensureProfileIdentityForSetup(paths runtimePaths, cfg *runtimeConfig) error {
	if cfg.ProfileID != "" {
		return validateProfileID(cfg.ProfileID)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err == nil {
		name, selectErr := resolveSelectedServerProfile(doc)
		if selectErr != nil {
			return selectErr
		}
		if stored, ok := doc.flatProfile(name); ok && stored.ProfileID != "" {
			cfg.ProfileID = stored.ProfileID
			return validateProfileID(cfg.ProfileID)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing server profile identity: %w", err)
	}
	return ensureProfileID(cfg)
}

func validateCloudSetupResult(cfg runtimeConfig, result cloudSetupResult) error {
	if err := rejectCloudSetupDuringDeviceRevocation(cfg); err != nil {
		return err
	}
	if problem := cloudRecoveryHoldProblem(cfg); problem != nil {
		return problem
	}
	if err := validateCurrentCloudConnectionMetadata(result.Current); err != nil {
		return &cloudProblem{
			Code:        cloudProblemAuthorization,
			Remediation: cloudRemediationSignIn,
			Detail:      fmt.Sprintf("Cloud setup returned invalid connection metadata: %v", err),
		}
	}
	if !validIdentifier(result.RelayInstanceID, 256) {
		return &cloudProblem{
			Code:        cloudProblemIdentityMismatch,
			Remediation: cloudRemediationSecurityStop,
			Detail:      "the NOVA Relay instance identity is missing or invalid",
		}
	}
	if cfg.RelayInstanceID != "" && cfg.RelayInstanceID != result.RelayInstanceID {
		return &cloudProblem{
			Code:        cloudProblemIdentityMismatch,
			Remediation: cloudRemediationSecurityStop,
			Detail:      "the local and Cloud NOVA Relay instance identities do not match",
		}
	}
	if cfg.Cloud == nil || cfg.Cloud.Pending == nil ||
		cfg.Cloud.State != cloudStateDeviceBoundOrPaired ||
		cfg.Cloud.Pending.CredentialGeneration != result.Current.CredentialGeneration {
		return &cloudProblem{
			Code:        cloudProblemAuthorization,
			Remediation: cloudRemediationSignIn,
			Detail:      "Cloud credential promotion did not match the saved pending generation",
		}
	}
	return nil
}
