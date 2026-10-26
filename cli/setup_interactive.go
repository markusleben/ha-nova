package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
)

var resolveHAURLBaseForSetup = resolveHomeAssistantURLBase
var probeHTTPForSetup = probeHTTP
var fetchRelayHealthForSetup = fetchRelayHealth
var probeRelayWSPingForSetup = probeRelayWSPing
var readRelayAuthTokenForSetup = readRelayAuthToken

func printRelayTokenStorageSetupWarning(err error) {
	switch {
	case isDesktopKeyringSessionUnavailableError(err):
		printHumanWarn("%s", setupT("setup.storage.warn_session_unavailable"))
	case isDesktopKeyringUnavailableError(err):
		printHumanWarn("%s", setupT("setup.storage.warn_unavailable"))
	case isDesktopKeyringInitializationRequiredError(err):
		printHumanWarn("%s", setupT("setup.storage.warn_not_initialized"))
	case isDesktopKeyringLockedError(err):
		printHumanWarn("%s", setupT("setup.storage.warn_locked"))
	case isDesktopKeyringSetupRequiredError(err):
		printHumanWarn("%s", setupT("setup.storage.warn_not_ready"))
	case err != nil:
		printHumanWarn("%s", relayAuthTokenProblemMessage(err))
	}
}

func maybeHandleInteractiveSetupCurrentState(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, current setupState, overrideApplied, serviceMode bool, lifecycleMarker ...[]byte) (bool, int) {
	return maybeHandleInteractiveSetupCurrentStateLinkPaused(reader, out, paths, cfg, current, overrideApplied, serviceMode, nil, lifecycleMarker...)
}

// maybeHandleInteractiveSetupCurrentStateLinkPaused: linkPaused (Link
// unfinished at the start) skips the Link and add-server offers and ends
// on that outcome, which it shows, instead of the done banner.
func maybeHandleInteractiveSetupCurrentStateLinkPaused(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, current setupState, overrideApplied, serviceMode bool, linkPaused func(), lifecycleMarker ...[]byte) (bool, int) {
	if !(current.ConfigOK || current.TokenOK || current.RelayOK || current.WSOK || current.SkillsOK) {
		return false, 0
	}

	renderSetupHeader(out)
	renderSetupStatusSummary(out, current)
	if cfg.Cloud != nil && !cloudRemoteFeatureAvailable() {
		fmt.Fprintln(out, "  "+setupT("setup.link.unavailable_build"))
		renderCloudCheckpointActions(out, paths, cfg, false)
		return true, 1
	}
	if current.IsComplete() {
		if overrideApplied {
			if err := ensureProfileIdentityForSetup(paths, &cfg); err != nil {
				printHumanErr("%s", setupT("setup.error.profile_identity", "error", err))
				return true, 1
			}
			if err := saveSetupConfigWithLifecycle(paths, cfg, lifecycleMarker...); err != nil {
				printHumanErr("%s", setupT("setup.error.save_config", "error", err))
				return true, 1
			}
		}
		var cloudAttempted, addAttempted bool
		if linkPaused == nil {
			var cloudCode int
			cfg, cloudAttempted, cloudCode = maybeOfferCloudForCompletedSetup(reader, out, paths, cfg, serviceMode, lifecycleMarker...)
			if cloudAttempted && cloudCode != 0 {
				return true, cloudCode
			}
			if cloudAttempted && cfg.Cloud != nil && !cfg.Cloud.ready() {
				return true, cloudCode
			}
			// Offered before completeSetupLifecycle so the add flow's config
			// saves run under the same lifecycle marker as this setup pass.
			var addCode int
			addAttempted, addCode = maybeOfferAddServerForCompletedSetup(reader, out, paths, serviceMode, lifecycleMarker...)
			if addAttempted && addCode != 0 {
				return true, addCode
			}
		}
		if err := completeSetupLifecycle(paths, lifecycleMarker...); err != nil {
			printHumanErr("%s", setupT("setup.error.finalize_lifecycle", "error", err))
			return true, 1
		}
		// Asked before the banner; the banner clears the screen, so the
		// outcome line prints after it.
		if !serviceMode && slices.Contains(loadStateOrDefault(paths).InstalledClients, "claude") && isInteractiveTTY() && writerSupportsTTYForSetup(out) {
			if note := offerClaudeRules(reader, out, paths); note != "" { // optional; never fails setup
				defer fmt.Fprintln(out, "  "+note)
			}
		}
		// Same suppression as the done banner below: after a ran add flow
		// its own closing lines are the message — never bury a cancelled or
		// partial add under a success banner.
		if linkPaused != nil {
			linkPaused()
			return true, 0
		}
		if !addAttempted && cloudAttempted && cfg.Cloud.ready() &&
			cfg.RoutePolicy == routePolicyAutomatic {
			renderSetupCloudFallbackReadyBanner(out, nil)
		}
		// After a ran add flow (success or cancel) its own closing lines are
		// the message — the already-done banner would bury a cancelled or
		// partial add under "Everything is already set up!".
		if !addAttempted {
			if !(cloudAttempted && cfg.Cloud.ready() && cfg.RoutePolicy == routePolicyAutomatic) {
				renderSetupAlreadyDoneBanner(out, !serviceMode && legacyAccessSwitchable(cfg, probePairingV1ForSetup))
			}
			if !serviceMode {
				if err := printCompletedSetupOverview(out, paths, cfg); err != nil {
					printHumanErr("%s", err)
					return true, 1
				}
			}
		}
		return true, 0
	}
	if summary := current.SkipSummary(); summary != "" {
		fmt.Fprintf(out, "  %s\n", setupT("setup.status.already_done", "steps", summary))
	}
	if writerSupportsTTYForSetup(out) {
		_, err := promptWizardLineFromReader(reader, out, setupT("setup.prompt.continue"), "")
		if err == errSetupExit {
			renderSetupCancelledNote(out)
			return true, 0
		}
		if err != nil && err != errSetupBack {
			printHumanErr("%s", err)
			return true, 1
		}
	}
	return false, 0
}

func saveSetupConfigWithLifecycle(paths runtimePaths, cfg runtimeConfig, lifecycleMarker ...[]byte) error {
	return withSetupLifecycleLock(paths, lifecycleMarker, func() error {
		return saveConfig(paths, cfg)
	})
}

func saveSetupConfigWithLifecycleUnlocked(
	paths runtimePaths,
	cfg runtimeConfig,
	lifecycleMarker ...[]byte,
) error {
	if err := ensureSetupLifecycleCurrent(paths, lifecycleMarker...); err != nil {
		return err
	}
	if err := saveConfig(paths, cfg); err != nil {
		return err
	}
	return refreshSetupConfigSnapshot(paths, lifecycleMarker)
}

func promptLineFromReader(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	return promptLineWithOptions(reader, out, label, defaultValue, false)
}

func generateRelayToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func maskSecretHint(value string) string {
	if len(value) <= 8 {
		return "***"
	}
	return "***" + value[len(value)-4:]
}

func persistInteractiveSetupStateWithRecovery(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, state *installState, previousToken string, hadPreviousToken bool, token string, recovery *setupSecureStorageRecoveryState, lifecycleMarker ...[]byte) error {
	return persistInteractiveSetupStateWithRecoveryMode(reader, out, paths, cfg, state, previousToken, hadPreviousToken, token, recovery, false, lifecycleMarker...)
}

func persistInteractiveSetupStateWithRecoveryMode(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, state *installState, previousToken string, hadPreviousToken bool, token string, recovery *setupSecureStorageRecoveryState, retireDevice bool, lifecycleMarker ...[]byte) error {
	err := persistInteractiveSetupStateWithMode(paths, cfg, state, previousToken, hadPreviousToken, token, retireDevice, lifecycleMarker...)
	if err == nil {
		return nil
	}
	if !isDesktopKeyringSetupRequiredError(err) || recovery == nil || recovery.saveRetryAttempted || !setupSecureStorageRecoveryAvailableNow(err) {
		return err
	}

	result, recoveryErr := runSetupSecureStorageRecoveryFlow(reader, out, err, recovery, setupSecureStorageRecoverySaveRetryAttempt)
	if recoveryErr != nil {
		if recoveryErr == errSetupExit || recoveryErr == errSetupBack {
			return relayAuthTokenSetupSaveError(err)
		}
		return recoveryErr
	}
	if result != setupSecureStorageRecoveryRecovered {
		return relayAuthTokenSetupSaveError(err)
	}

	return persistInteractiveSetupStateWithMode(paths, cfg, state, previousToken, hadPreviousToken, token, retireDevice, lifecycleMarker...)
}
