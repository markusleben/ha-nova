package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

type cloudSetupRequest struct {
	ProfileName                string
	Config                     runtimeConfig
	PersistPendingMetadata     func(cloudConnectionMetadata) error
	ClearPendingAuthorization  func(string) error
	AdvancePendingLifecycle    func(cloudLifecycleState) error
	CheckpointDeviceActivation func(string) error
	ClearDeviceActivation      func() error
	CheckpointDeviceBinding    func(string) error
}

type cloudSetupResult struct {
	Current         cloudConnectionMetadata
	RelayInstanceID string
}

// cloudSetupCoordinator deliberately exposes only the existing-device path.
// The completed local-install wizard therefore cannot accidentally request a
// second pairing code. Implementations first persist non-secret pending
// metadata, then create its native-store secret, advance token_stored,
// cloud_verified, and device_bound_or_paired, and finally promote that exact
// generation before returning success. They must compare the locally
// authenticated Relay instance with Cloud discovery before binding or
// promotion. The wizard owns the committed/retiring_previous/ready config
// writes. Explicit preflight owns any native device- and OAuth-store prompts;
// AddAwayWithExistingDevice must use no-UI secret-store operations afterward.
type cloudSetupCoordinator interface {
	Available() bool
	Preflight(context.Context, string) error
	AddAwayWithExistingDevice(context.Context, cloudSetupRequest) (cloudSetupResult, error)
}

type cloudSetupRetirer interface {
	RetirePrevious(context.Context, string) error
}

type unavailableCloudSetupCoordinator struct{}

func (unavailableCloudSetupCoordinator) Available() bool {
	return false
}

func (unavailableCloudSetupCoordinator) Preflight(context.Context, string) error {
	return cloudAdapterUnavailableProblem()
}

func (unavailableCloudSetupCoordinator) AddAwayWithExistingDevice(context.Context, cloudSetupRequest) (cloudSetupResult, error) {
	return cloudSetupResult{}, cloudAdapterUnavailableProblem()
}

var cloudCoordinatorForSetup cloudSetupCoordinator = unavailableCloudSetupCoordinator{}

var cloudSetupPromptEligible = func(out io.Writer) bool {
	return nativeSecretPromptSessionAvailable(out)
}

var reusableLocalDeviceForCloudSetup = func(cfg runtimeConfig) (bool, error) {
	_, _, _, deviceMode, err := relayFunctionalTransportForDoctor(cfg)
	return deviceMode, err
}

func maybeOfferCloudForCompletedSetup(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	cfg runtimeConfig,
	serviceMode bool,
	lifecycleMarker ...[]byte,
) (runtimeConfig, bool, int) {
	if problem := cloudRecoveryHoldProblem(cfg); problem != nil {
		renderCloudRecoveryGuidance(out, cfg, problem)
		return cfg, true, 1
	}
	coordinator := cloudCoordinatorForSetup
	if cfg.Cloud != nil &&
		(!cloudRemoteFeatureAvailable() ||
			coordinator == nil ||
			!coordinator.Available()) {
		fmt.Fprintln(
			out,
			"  Home Assistant Cloud access is unavailable because this build or platform has Cloud setup disabled.",
		)
		renderCloudCheckpointActions(out, paths, cfg, false)
		return cfg, true, 1
	}
	if coordinator == nil || !coordinator.Available() || cfg.Cloud.ready() {
		return cfg, false, 0
	}
	if serviceMode || !cloudSetupPromptEligible(out) {
		fmt.Fprintln(out, "  Home Assistant Cloud remote access (Beta) is available only from an interactive desktop session.")
		fmt.Fprintln(out, "  Your working local connection was not changed.")
		if hybridCloudSetupPending(cfg) {
			renderCloudCheckpointActions(out, paths, cfg, true)
			return cfg, true, 1
		}
		return cfg, false, 0
	}
	if cfg.Cloud != nil &&
		(cfg.Cloud.State == cloudStateCommitted ||
			cfg.Cloud.State == cloudStateRetiringPrevious) {
		resumeCtx, cancelResume := context.WithTimeout(
			context.Background(),
			2*time.Minute,
		)
		defer cancelResume()
		resumeCtx = withCloudSecretAccessHolder(resumeCtx)
		var resumed bool
		err := withClientMutationLock(paths, func() error {
			var preflightErr error
			resumeCtx, preflightErr = preflightCloudSecretAccessSession(
				resumeCtx,
				coordinator,
				cfg,
				cloudSecretPreflightSetup,
			)
			if preflightErr != nil {
				return preflightErr
			}
			var err error
			resumed, err = resumeCommittedCloudSetup(
				resumeCtx,
				coordinator,
				paths,
				&cfg,
				func(value runtimeConfig) error {
					return saveSetupConfigWithLifecycleUnlocked(
						paths,
						value,
						lifecycleMarker...,
					)
				},
			)
			return err
		})
		if err != nil {
			renderCloudFailure(out, paths, err)
			return cfg, true, 1
		}
		if resumed {
			printHumanInfo("Home Assistant Cloud access is ready. Local access stays preferred.")
			return cfg, true, 0
		}
	}
	if cfg.Cloud != nil && cfg.Cloud.Current != nil {
		// A reconnect/rotation owns this state. The add-away wizard must not
		// replace a still-usable current credential.
		renderCloudCheckpointActions(out, paths, cfg, true)
		return cfg, true, 0
	}
	localDeviceReady, err := reusableLocalDeviceForCloudSetup(cfg)
	if err != nil {
		fmt.Fprintln(out, "  Home Assistant Cloud access needs the existing paired-device credential.")
		renderCloudFailure(out, paths, err)
		if hybridCloudSetupPending(cfg) {
			return cfg, true, 1
		}
		return cfg, false, 0
	}
	if !localDeviceReady {
		if hybridCloudSetupPending(cfg) {
			renderCloudCheckpointActions(out, paths, cfg, true)
			return cfg, true, 1
		}
		return cfg, false, 0
	}

	fmt.Fprintln(out, "  Away-from-home access (Beta)")
	resuming := hybridCloudSetupPending(cfg) &&
		cfg.Cloud != nil &&
		cfg.Cloud.Current == nil
	if resuming {
		fmt.Fprintln(out, "  A previous Home Assistant Cloud setup is ready to resume.")
	} else {
		fmt.Fprintln(out, "  Use your Home Assistant Cloud URL when local access is unavailable.")
	}
	fmt.Fprintln(out, "  OAuth credentials stay in this computer's native secure storage.")
	prompt := "Add Home Assistant Cloud access now?"
	defaultYes := false
	if resuming {
		prompt = "Resume Home Assistant Cloud setup now?"
		defaultYes = true
	}
	enable, err := promptWizardYesNoFromReader(
		reader,
		out,
		prompt,
		defaultYes,
	)
	switch {
	case errors.Is(err, errSetupExit), errors.Is(err, errSetupBack):
		if resuming {
			renderCloudCheckpointActions(out, paths, cfg, true)
			return cfg, true, 0
		}
		return cfg, false, 0
	case err != nil:
		printHumanErr("%v", err)
		return cfg, true, 1
	case !enable:
		if resuming {
			renderCloudCheckpointActions(out, paths, cfg, true)
			return cfg, true, 0
		}
		return cfg, false, 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	ctx = withCloudSecretAccessHolder(ctx)
	err = withClientMutationLock(paths, func() error {
		save := func(value runtimeConfig) error {
			return saveSetupConfigWithLifecycleUnlocked(
				paths,
				value,
				lifecycleMarker...,
			)
		}
		updated, connectErr := connectExistingDeviceToCloud(
			ctx,
			paths,
			cfg,
			coordinator,
			false,
			save,
		)
		cfg = updated
		return connectErr
	})
	if err != nil {
		renderCloudFailure(out, paths, err)
		return cfg, true, 1
	}
	printHumanInfo("Home Assistant Cloud access is ready. Local access stays preferred.")
	return cfg, true, 0
}

func resumeCommittedCloudSetup(
	ctx context.Context,
	coordinator cloudSetupCoordinator,
	paths runtimePaths,
	cfg *runtimeConfig,
	save cloudConfigSaver,
	lifecycleMarker ...[]byte,
) (resumed bool, resultErr error) {
	if save == nil {
		save = func(value runtimeConfig) error {
			return saveSetupConfigWithLifecycle(paths, value, lifecycleMarker...)
		}
	}
	defer func() {
		resultErr = persistCloudRecoveryHoldForError(
			cfg,
			resultErr,
			save,
		)
	}()
	if cfg.Cloud == nil {
		return false, nil
	}
	if err := rejectCloudSetupDuringDeviceRevocation(*cfg); err != nil {
		return false, err
	}
	if problem := cloudRecoveryHoldProblem(*cfg); problem != nil {
		return false, problem
	}
	switch cfg.Cloud.State {
	case cloudStateCommitted:
		if cfg.Cloud.Current == nil || cfg.Cloud.Pending == nil ||
			cfg.Cloud.Current.CredentialGeneration != cfg.Cloud.Pending.CredentialGeneration {
			return false, errors.New("committed Cloud generations do not match")
		}
		cfg.Cloud.Pending = nil
		cfg.Cloud.State = cloudStateRetiringPrevious
		if err := save(*cfg); err != nil {
			return false, err
		}
		fallthrough
	case cloudStateRetiringPrevious:
		if err := retirePreviousCloudAuthorization(ctx, coordinator, cfg.ProfileID); err != nil {
			return false, err
		}
		cfg.Cloud.State = cloudStateReady
		if cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
			cfg.RoutePolicy = routePolicyAutomatic
		} else {
			cfg.RoutePolicy = routePolicyCloud
		}
		if err := save(*cfg); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func pendingCloudLifecycleRank(state cloudLifecycleState) (int, bool) {
	switch state {
	case cloudStateAuthorizing:
		return 0, true
	case cloudStateTokenStored:
		return 1, true
	case cloudStateCloudVerified:
		return 2, true
	case cloudStateDeviceBoundOrPaired:
		return 3, true
	default:
		return 0, false
	}
}

func retirePreviousCloudAuthorization(
	ctx context.Context,
	coordinator cloudSetupCoordinator,
	profileID string,
) error {
	retirer, ok := coordinator.(cloudSetupRetirer)
	if !ok {
		return nil
	}
	return retirer.RetirePrevious(ctx, profileID)
}

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
