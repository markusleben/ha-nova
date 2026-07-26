package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func runCloudUnlockCommand(paths runtimePaths, args []string) int {
	options, err := parseCloudCommandFlags("unlock", args)
	if errors.Is(err, errHelpShown) {
		return 0
	}
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if !cloudInteractivePromptSessionForSetup() {
		printHumanErr(
			"cloud unlock requires an interactive desktop session: use a local, non-elevated graphical desktop terminal (not SSH, sudo/root, or WSL)",
		)
		return 1
	}
	snapshot, preProfile, err := loadCloudUnlockConfig(paths, options)
	if err != nil {
		renderCloudFailure(os.Stdout, paths, err)
		return 1
	}
	cfg := snapshot.Config
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx = withCloudSecretAccessHolder(ctx)
	if err := preflightCloudUnlockDeviceAccess(ctx, cfg, preProfile); err != nil {
		err = checkpointCloudUnlockFailure(paths, snapshot, preProfile, err)
		renderCloudFailure(os.Stdout, paths, err)
		return 1
	}
	if preProfile || (cfg.ProfileID == "" && cfg.Cloud == nil) {
		if !cloudRemoteFeatureAvailable() {
			printHumanInfo(
				"Device credential storage was checked. Cloud transport is disabled in this build.",
			)
			printHumanInfo(
				"No Cloud checkpoint was saved, so no Cloud cleanup is needed. Local-only setup remains available.",
			)
			return 0
		}
		printHumanInfo(
			"Device credential storage is unlocked. No Cloud checkpoint was saved.",
		)
		printHumanInfo(
			"OAuth storage has no profile-scoped slot yet and will be checked during guided Cloud setup.",
		)
		printHumanInfo(
			"Start guided Cloud setup with: %s",
			cloudFreshAddCommand(),
		)
		return 0
	}
	if cfg.Cloud != nil &&
		cfg.Cloud.authorizationCleanupPending() {
		store, err := newCloudSecretStoreForCLI(cfg.ProfileID)
		if err != nil {
			err = checkpointCloudUnlockFailure(
				paths,
				snapshot,
				preProfile,
				err,
			)
			renderCloudFailure(os.Stdout, paths, err)
			return 1
		}
		if err := PreflightOAuthSecretStore(
			ctx,
			store,
			SecretStoreAllowUI,
		); err != nil {
			err = checkpointCloudUnlockFailure(
				paths,
				snapshot,
				preProfile,
				err,
			)
			renderCloudFailure(os.Stdout, paths, err)
			return 1
		}
		cfg, _, err = markCloudUnlockStorageVerified(
			paths,
			snapshot,
			cfg,
			true,
		)
		if err != nil {
			renderCloudFailure(os.Stdout, paths, err)
			return 1
		}
		printHumanInfo(
			"Native secure storage is unlocked. Remote Cloud revocation is already complete.",
		)
		printHumanInfo(
			"Finish verified local cleanup with: %s",
			cloudRemoveCommand(),
		)
		return 0
	}
	ctx, err = preflightCloudSecretAccessSession(
		ctx,
		cloudCoordinatorForSetup,
		cfg,
		cloudSecretPreflightUnlock,
	)
	if err != nil {
		err = checkpointCloudUnlockFailure(paths, snapshot, preProfile, err)
		renderCloudFailure(os.Stdout, paths, err)
		return 1
	}
	cfg, verifiedStorageHold, err := markCloudUnlockStorageVerified(
		paths,
		snapshot,
		cfg,
		cfg.Cloud != nil &&
			(cfg.Cloud.deviceCleanupPending() ||
				!cloudRemoteFeatureAvailable() ||
				!cfg.Cloud.ready()),
	)
	if err != nil {
		renderCloudFailure(os.Stdout, paths, err)
		return 1
	}
	if cfg.Cloud != nil && cfg.Cloud.deviceCleanupPending() {
		printHumanInfo(
			"Native secure storage is unlocked. Cloud device revocation is complete; OAuth authorization revocation still remains.",
		)
		printHumanInfo(
			"Continue verified remote revocation and local cleanup with: %s",
			cloudRemoveCommand(),
		)
		return 0
	}
	if cfg.Cloud != nil &&
		cfg.Cloud.RecoveryHold != nil &&
		!cloudRecoveryHoldClearsAfterUnlock(cfg.Cloud.RecoveryHold) {
		printHumanInfo(
			"Native secure storage is unlocked, but Cloud recovery remains paused for security review.",
		)
		printHumanInfo(
			"Verified cleanup remains available with: %s",
			cloudRemoveCommand(),
		)
		return 0
	}
	if !cloudRemoteFeatureAvailable() {
		if verifiedStorageHold != nil {
			printHumanInfo(
				"Native secure storage is verified. Cloud health cannot be checked in this build.",
			)
			printHumanInfo(
				"Verified cleanup remains available with: %s",
				cloudRemoveCommand(),
			)
			return 0
		}
		printHumanInfo(
			"Native secure storage is unlocked for cleanup; Cloud transport remains disabled in this build.",
		)
		if cfg.Cloud != nil {
			printHumanInfo(
				"Verified cleanup remains available with: %s",
				cloudRemoveCommand(),
			)
		}
		return 0
	}
	if cfg.Cloud == nil {
		printHumanInfo(
			"Native secure storage is unlocked. Start guided Cloud setup with: %s",
			cloudFreshAddCommand(),
		)
		return 0
	}
	if cfg.Cloud.ready() {
		snapshot, healthErr := loadAndVerifyCloudHealthWithCheckpoint(
			ctx,
			paths,
			verifyCloudDeviceHealthForCommand,
			verifiedStorageHold,
		)
		if healthErr != nil {
			renderCloudFailure(os.Stdout, paths, healthErr)
			return 1
		}
		if verifiedStorageHold != nil {
			expected, expectationErr := snapshot.recoveryExpectation()
			if expectationErr != nil {
				renderCloudFailure(os.Stdout, paths, expectationErr)
				return 1
			}
			cfg, err = clearCloudRecoveryHoldAtSnapshot(
				paths,
				expected,
				*verifiedStorageHold,
			)
			if err != nil {
				renderCloudFailure(os.Stdout, paths, err)
				return 1
			}
			printVerifiedStorageHoldCleared()
		}
		printHumanInfo("Native secure storage is unlocked and Cloud access is ready.")
		return 0
	}
	if verifiedStorageHold != nil {
		printHumanInfo(
			"Native secure storage is verified, but Cloud access is not ready for a health check.",
		)
		printHumanInfo(
			"Verified cleanup remains available with: %s",
			cloudRemoveCommand(),
		)
		return 0
	}
	if cfg.Cloud.Current != nil {
		printHumanInfo(
			"Native secure storage is unlocked; a Cloud update is waiting at %q.",
			cfg.Cloud.State,
		)
	} else {
		printHumanInfo(
			"Native secure storage is unlocked; Cloud setup is waiting at %q.",
			cfg.Cloud.State,
		)
	}
	printHumanInfo("Resume with: %s", cloudResumeCommand(cfg))
	return 0
}

func printVerifiedStorageHoldCleared() {
	printHumanInfo(
		"Native secure storage was verified; the recovery safety hold was cleared.",
	)
}

func checkpointCloudUnlockFailure(
	paths runtimePaths,
	snapshot cloudManagementSnapshot,
	preProfile bool,
	cause error,
) error {
	if cause == nil || preProfile {
		return cause
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		return cause
	}
	_, checkpointErr := checkpointCloudRecoveryHold(
		paths,
		expected,
		cause,
	)
	if checkpointErr != nil {
		return errors.Join(
			cause,
			fmt.Errorf(
				"persist Cloud recovery safety hold: %w",
				checkpointErr,
			),
		)
	}
	return cause
}

func markCloudUnlockStorageVerified(
	paths runtimePaths,
	snapshot cloudManagementSnapshot,
	cfg runtimeConfig,
	shouldMark bool,
) (runtimeConfig, *cloudRecoveryHold, error) {
	if cfg.Cloud == nil || cfg.Cloud.RecoveryHold == nil {
		return cfg, nil, nil
	}
	hold := *cfg.Cloud.RecoveryHold
	if !cloudRecoveryHoldClearsAfterUnlock(&hold) {
		return cfg, &hold, nil
	}
	if !shouldMark || hold.StorageVerified {
		return cfg, &hold, nil
	}
	expected, err := snapshot.recoveryExpectation()
	if err != nil {
		return runtimeConfig{}, nil, err
	}
	cfg, err = markCloudRecoveryStorageVerifiedAtSnapshot(
		paths,
		expected,
		hold,
	)
	if err != nil {
		return runtimeConfig{}, nil, err
	}
	return cfg, cfg.Cloud.RecoveryHold, nil
}

func loadCloudManagementConfig(paths runtimePaths) (runtimeConfig, error) {
	snapshot, err := loadCloudManagementSnapshot(paths)
	return snapshot.Config, err
}

func printCloudCommandProblem(err error) *cloudProblem {
	problem := cloudProblemForCommandError(err)
	printHumanErr("%s", problem)
	if problem.Remediation == cloudRemediationUnlockStorage {
		printHumanInfo(
			"From a local, non-elevated graphical desktop terminal, run: %s",
			cloudUnlockCommand(),
		)
	}
	return problem
}

func cloudProblemForCommandError(err error) *cloudProblem {
	return cloudProblemForError(err)
}

func verifyCloudDeviceHealth(ctx context.Context, cfg runtimeConfig) error {
	selection, err := resolveCloudRelayTransport(ctx, cfg)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(selection.BaseURL, "/")+"/health",
		nil,
	)
	if err != nil {
		return newCloudError(CloudErrInvalidInput, "build Cloud health request", err)
	}
	request.Header.Set("Authorization", "Bearer "+selection.Credential)
	request.Header.Set("Accept", "application/json")
	response, err := selection.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := readCloudResponse(
		response.Body,
		cloudLocalDiscoveryMaxBytes,
		"read Cloud health response",
	)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return newCloudHTTPError(
			CloudErrDeviceRejected,
			"verify Cloud device",
			response.StatusCode,
			false,
		)
	}
	return validateCloudRelayHealthIdentity(body, cfg.RelayInstanceID)
}
