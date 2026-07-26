package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

func remoteOnlyCloudSetup(cfg runtimeConfig) bool {
	return cfg.Cloud != nil &&
		strings.TrimSpace(cfg.HAHost) == "" &&
		strings.TrimSpace(cfg.HAURL) == "" &&
		strings.TrimSpace(cfg.RelayBaseURL) == "" &&
		strings.TrimSpace(cfg.RelaySecureBaseURL) == "" &&
		strings.TrimSpace(cfg.RelaySpkiPin) == ""
}

func recoverRemoteOnlyCloudSetupConfig(
	raw runtimeConfig,
) (runtimeConfig, bool) {
	if !remoteOnlyCloudSetup(raw) {
		return runtimeConfig{}, false
	}
	recovered := raw
	if err := validateLoadedRuntimeConfig(&recovered); err != nil {
		return runtimeConfig{}, false
	}
	return recovered, true
}

func resumeInteractiveCloudOnlySetup(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	cfg runtimeConfig,
	state *installState,
	target string,
	selectedClients, skippedClients []string,
	lifecycleMarker ...[]byte,
) int {
	if problem := cloudRecoveryHoldProblem(cfg); problem != nil {
		renderCloudRecoveryGuidance(out, cfg, problem)
		return 1
	}
	if err := requireCloudRemoteFeatureForSetup(); err != nil {
		printHumanErr("%s", err)
		renderCloudRecoveryGuidance(
			out,
			cfg,
			cloudAdapterUnavailableProblem(),
		)
		return 1
	}
	coordinator, ok := cloudCoordinatorForSetup.(cloudRemoteSetupCoordinator)
	if !ok {
		printHumanErr("%s", cloudAdapterUnavailableProblem())
		return 1
	}
	ctx, cancel := newInteractiveCloudSetupContext()
	defer cancel()

	if cfg.Cloud != nil &&
		(cfg.Cloud.State == cloudStateCommitted ||
			cfg.Cloud.State == cloudStateRetiringPrevious) {
		err := withClientMutationLock(paths, func() error {
			if err := preflightCloudSecretAccess(
				ctx,
				coordinator,
				cfg,
				cloudSecretPreflightSetup,
			); err != nil {
				return err
			}
			_, err := resumeCommittedCloudSetup(
				ctx,
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
			return 1
		}
	}

	if !cfg.Cloud.ready() {
		configSnapshot, hadConfig, snapshotErr := readOptionalFile(
			paths.ConfigFile,
		)
		if snapshotErr != nil {
			renderCloudFailure(out, paths, snapshotErr)
			return 1
		}
		origin, err := cloudOriginForSetupResume(ctx, reader, out, cfg)
		if err == errSetupExit || err == errSetupBack {
			handlePausedCloudOwnerPairing(out, paths, err)
			return 0
		}
		if err != nil {
			renderCloudFailure(out, paths, err)
			return 1
		}
		pairingCode := cloudOnlyPairingCodePrompt(reader, out)
		err = withPausableClientMutationLock(
			paths,
			func(mutation *pausableClientMutationLock) error {
				if err := ensureOptionalFileSnapshotCurrent(
					paths.ConfigFile,
					configSnapshot,
					hadConfig,
				); err != nil {
					return err
				}
				save := func(value runtimeConfig) error {
					if err := mutation.requireHeld(); err != nil {
						return err
					}
					return saveSetupConfigWithLifecycleUnlocked(
						paths,
						value,
						lifecycleMarker...,
					)
				}
				reconnect := cfg.Cloud != nil && cfg.Cloud.Current != nil
				updated, connectErr := connectRemoteToCloud(
					ctx,
					paths,
					cfg,
					coordinator,
					origin,
					mutation.pairingCodeProvider(pairingCode),
					reconnect,
					save,
				)
				cfg = updated
				return connectErr
			},
		)
		if err != nil {
			if handlePausedCloudOwnerPairing(out, paths, err) {
				return 0
			}
			renderCloudFailure(out, paths, err)
			return 1
		}
	} else if err := preflightCloudSecretAccess(
		ctx,
		coordinator,
		cfg,
		cloudSecretPreflightSetup,
	); err != nil {
		renderCloudFailure(out, paths, err)
		return 1
	}

	if _, err := loadAndVerifyCloudHealthWithCheckpoint(
		ctx,
		paths,
		verifyCloudDeviceHealthForSetup,
		nil,
	); err != nil {
		renderCloudFailure(out, paths, err)
		return 1
	}
	return finishInteractiveCloudOnlySetup(
		reader,
		out,
		paths,
		state,
		target,
		selectedClients,
		skippedClients,
		lifecycleMarker...,
	)
}

func cloudOriginForSetupResume(
	ctx context.Context,
	reader *bufio.Reader,
	out io.Writer,
	cfg runtimeConfig,
) (CloudOrigin, error) {
	if cfg.Cloud != nil && cfg.Cloud.Pending != nil {
		return cloudOriginFromMetadata(*cfg.Cloud.Pending)
	}
	defaultOrigin := ""
	if cfg.Cloud != nil && cfg.Cloud.Current != nil {
		defaultOrigin = cfg.Cloud.Current.Origin
	}
	renderSetupHeader(out)
	renderSetupParagraph(
		out,
		"Resume Home Assistant Cloud setup with the remote URL shown under Settings > Home Assistant Cloud.",
		"OAuth credentials will stay only in this computer's native secure storage.",
	)
	return promptValidatedCloudRemoteOrigin(
		ctx,
		reader,
		out,
		defaultOrigin,
		NetCloudCNAMEResolver{},
	)
}

func cloudOnlyPairingCodePrompt(
	reader *bufio.Reader,
	out io.Writer,
) cloudRemotePairingCodeProvider {
	return func(prompt cloudRemotePairingPrompt) (string, error) {
		return promptRemoteOwnerPairingCode(
			reader,
			out,
			prompt,
		)
	}
}

func finishInteractiveCloudOnlySetup(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	state *installState,
	target string,
	selectedClients, skippedClients []string,
	lifecycleMarker ...[]byte,
) int {
	if target == "all" && len(skippedClients) > 0 {
		fmt.Fprintf(
			out,
			"  Skipping until installed: %s\n",
			strings.Join(skippedClients, ", "),
		)
	}
	if err := runSetupStepWithFeedback(
		out,
		fmt.Sprintf("Setting up HA NOVA for %s...", setupClientLabel(target)),
		func() error {
			return withClientMutationLock(paths, func() error {
				if err := installClientsAndSaveStateUnlocked(
					paths,
					state,
					selectedClients,
					saveState,
					lifecycleMarker...,
				); err != nil {
					return err
				}
				return completeSetupLifecycleUnlocked(paths, lifecycleMarker...)
			})
		},
	); err != nil {
		printHumanErr("client installation failed: %s", err)
		return 1
	}
	renderSetupCompleteBanner(out, selectedClients)
	askCensusIfEligible(paths, "setup", reader, out)
	return 0
}
