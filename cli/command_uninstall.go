package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type uninstallMode string

const (
	uninstallModeStandard uninstallMode = "standard"
	uninstallModePurge    uninstallMode = "purge"
)

func runInternalUninstall(_ runtimePaths, args []string) int {
	fs := flag.NewFlagSet("internal-uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	parentPID := fs.Int("parent-pid", 0, "parent pid")
	selfPath := fs.String("self-path", "", "temp helper path")
	purge := fs.Bool("purge", false, "remove config and token")
	teardownDone := fs.Bool("teardown-done", false, "server-side teardown already completed by the parent")
	teardownRelayInstanceID := fs.String(
		"teardown-relay-instance-id",
		"",
		"exact Relay identity removed by guided teardown",
	)
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	paths, err := detectPaths()
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	removedRelays, err := windowsUninstallHelperTeardownEvidence(
		*teardownDone,
		*teardownRelayInstanceID,
	)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	status, err := beginWindowsUninstallStatusWithTeardown(
		paths,
		uninstallModeFromFlag(*purge),
		installSourceBundle,
		*teardownDone,
		removedRelays,
	)
	if err != nil {
		printHumanErr("cannot persist Windows uninstall recovery state: %s", err)
		return 1
	}
	stopHeartbeat := startWindowsUninstallHeartbeat(paths, status)
	defer stopHeartbeat()
	waitForParentReleaseForUninstall(*parentPID)
	preflight := collectUninstallPreflight(paths)
	report := &uninstallReport{}
	if err := finalizeWindowsUninstall(
		paths,
		report,
		uninstallModeFromFlag(*purge),
		status,
		removedRelays,
	); err != nil {
		report.printDetails()
		printHumanErr("%s", err)
		return 1
	}
	if *teardownDone {
		for _, note := range teardownCompletedNoteLines(uninstallModeFromFlag(*purge)) {
			report.addNote(note)
		}
	} else {
		applyUninstallPreflightNotes(report, preflight)
	}
	if report.print() {
		printHumanInfo("HA NOVA removed")
	}
	if err := finishWindowsUninstallStatus(paths, status); err != nil {
		printHumanWarn("could not clear Windows uninstall recovery state: %s", err)
	}
	if err := scheduleWindowsSelfDeleteForUninstall(*selfPath); err != nil {
		printHumanWarn("could not schedule uninstall helper cleanup: %s", err)
	}
	return 0
}

func runUninstall(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "skip confirmation prompts (prints the Home Assistant cleanup checklist instead)")
	purge := fs.Bool("purge", false, "also remove config, state, and the relay auth token (retains two opaque safety markers)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova uninstall [--yes] [--purge]") {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}

	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	source := detectInstallSource(paths, state)
	mode := uninstallModeFromFlag(*purge)
	recoveryMode := false
	recoveredTeardown := false
	var recoveredRemovedRelays uninstallRelayRemovalEvidence
	if recovery := inspectWindowsUninstallStatus(paths); recovery.Kind != windowsUninstallStatusKindNone {
		if exitCode := handleWindowsUninstallRecovery(recovery, mode); exitCode != 0 {
			return exitCode
		}
		recoveryMode = recovery.Kind == windowsUninstallStatusKindInterrupted || recovery.Kind == windowsUninstallStatusKindFailed || recovery.Kind == windowsUninstallStatusKindCorrupt
		if recovery.Kind == windowsUninstallStatusKindInterrupted ||
			recovery.Kind == windowsUninstallStatusKindFailed {
			recoveredTeardown, recoveredRemovedRelays, err =
				windowsUninstallTeardownEvidence(recovery.Status)
			if err != nil {
				printHumanErr(
					"Windows uninstall recovery marker has invalid guided-teardown evidence: %s",
					err,
				)
				return 1
			}
		}
	}

	renderUninstallPreflight(os.Stdout, paths, source)
	if !*yes && !recoveryMode && isInteractiveTTY() {
		selectedMode, proceed, err := promptUninstallMode(mode)
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
		if !proceed {
			printHumanInfo("Uninstall cancelled")
			return 0
		}
		mode = selectedMode
	}

	guidedSession := !*yes && !recoveryMode && isInteractiveTTY()
	if guidedSession {
		if err := prepareUninstallBeforeGuidedTeardown(
			paths,
			mode,
		); err != nil {
			printHumanErr("%s", err)
			return 1
		}
	}
	preflight := collectUninstallPreflight(paths)
	teardown := teardownNotOffered
	if guidedSession {
		outcome, err := maybeOfferGuidedTeardown(bufio.NewReader(os.Stdin), os.Stdout, preflight, defaultTeardownDeps())
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
		if outcome == teardownCancelled {
			printHumanInfo("The remaining uninstall was cancelled. Any recovery cleanup or Home Assistant steps already completed remain in effect.")
			return 0
		}
		teardown = outcome
		if teardown == teardownCompleted {
			preflight.relayStillRunning = false
		}
	}
	if runtime.GOOS == "windows" && source == installSourceBundle {
		teardownDone := teardown == teardownCompleted
		removedRelays := uninstallRelayRemovalEvidenceFromPreflight(
			preflight,
			teardownDone,
		)
		if recoveryMode {
			teardownDone = recoveredTeardown
			removedRelays = recoveredRemovedRelays
		}
		if teardownDone {
			printTeardownCompletedNotes(os.Stdout, mode)
		} else {
			printUninstallPreflightNotes(os.Stdout, preflight)
		}
		if err := launchWindowsUninstall(
			paths,
			mode,
			teardownDone,
			removedRelays,
		); err != nil {
			printHumanErr("cannot finish Windows uninstall: %s", err)
			return 1
		}
		printHumanInfo("HA NOVA uninstall continues in the background on Windows.")
		printHumanInfo("It is safe to close this terminal now.")
		return 0
	}
	if channelChecksUseWindowsPlatform() && source == installSourceLegacyWindowsPackage {
		printUninstallPreflightNotes(os.Stdout, preflight)
		printHumanErr("Legacy private/test Windows package installs are no longer supported for in-place `ha-nova uninstall`.")
		printHumanWarn("Remove the old HA NOVA app in Installed Apps / App Installer.")
		printHumanWarn("Then reinstall with the supported Windows path:")
		printHumanWarn("  irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex")
		return 1
	}

	report := &uninstallReport{}
	configDirExisted := fileExists(paths.ConfigDir)
	cleanupConfigDir := false
	var configCleanupErr error
	releaseMutation, acquired := acquireAutoRepairLockWithFinalizer(paths, func() {
		if !cleanupConfigDir {
			return
		}
		if !configDirExisted {
			configCleanupErr = os.Remove(paths.ConfigDir)
			if isNotExist(configCleanupErr) {
				configCleanupErr = nil
			}
			return
		}
		configCleanupErr = removeDirIfEmptyWithReport(paths.ConfigDir, report)
	})
	if !acquired {
		printHumanErr("another HA NOVA client update is already in progress")
		return 1
	}
	state = loadStateOrDefault(paths)
	removedRelays := uninstallRelayRemovalEvidenceFromPreflight(
		preflight,
		teardown == teardownCompleted,
	)
	if err := finalizeLocalUninstallWithProgressUnlocked(
		paths,
		state,
		report,
		mode,
		nil,
		removedRelays,
	); err != nil {
		releaseMutation()
		printHumanErr("%s", err)
		return 1
	}
	if source == installSourceBundle {
		if err := removeBundleRuntime(paths, report); err != nil {
			releaseMutation()
			printHumanErr("%s", err)
			return 1
		}
	}
	cleanupConfigDir = true
	releaseMutation()
	if configCleanupErr != nil {
		printHumanErr("failed to remove managed config directory: %s", configCleanupErr)
		return 1
	}

	if teardown == teardownCompleted {
		for _, note := range teardownCompletedNoteLines(mode) {
			report.addNote(note)
		}
	} else {
		applyUninstallPreflightNotes(report, preflight)
	}
	if report.print() {
		printHumanInfo("HA NOVA removed")
	}
	return 0
}

func launchWindowsUninstall(
	paths runtimePaths,
	mode uninstallMode,
	teardownDone bool,
	removedRelays uninstallRelayRemovalEvidence,
) error {
	if _, err := windowsUninstallRelayRemovalRefFromEvidence(
		teardownDone,
		removedRelays,
	); err != nil {
		return err
	}
	status, err := beginWindowsUninstallStatusWithTeardown(
		paths,
		mode,
		installSourceBundle,
		teardownDone,
		removedRelays,
	)
	if err != nil {
		return fmt.Errorf(
			"cannot persist Windows uninstall recovery state: %w",
			err,
		)
	}
	tempHelper := filepath.Join(os.TempDir(), "ha-nova-uninstall-"+strconv.Itoa(os.Getpid())+".exe")
	if err := copyFile(filepath.Join(paths.InstallRoot, publicBinaryName()), tempHelper); err != nil {
		return failWindowsUninstallStatus(
			paths,
			status,
			"bundle_runtime_cleanup",
			err,
		)
	}
	statusTicks := windowsUninstallStatusMarkerTicks(paths.UninstallStatusFile)
	args := []string{"internal-uninstall", "--parent-pid", strconv.Itoa(os.Getpid()), "--self-path", tempHelper}
	if mode == uninstallModePurge {
		args = append(args, "--purge")
	}
	if teardownDone {
		args = append(args, "--teardown-done")
	}
	if relayInstanceID, exists := removedRelays[defaultServerProfileName]; exists && strings.TrimSpace(relayInstanceID) != "" {
		args = append(
			args,
			"--teardown-relay-instance-id",
			strings.TrimSpace(relayInstanceID),
		)
	}
	if err := launchWindowsDetachedHelperWithEnv(
		tempHelper,
		paths.UninstallStatusFile,
		statusTicks,
		helperInstallRootEnv(paths.InstallRoot),
		args...,
	); err != nil {
		return failWindowsUninstallStatus(
			paths,
			status,
			"bundle_runtime_cleanup",
			err,
		)
	}
	return nil
}

func scheduleWindowsSelfDelete(path string) error {
	if runtime.GOOS != "windows" || strings.TrimSpace(path) == "" {
		return nil
	}
	cmd := buildWindowsCleanupCommand(path)
	return cmd.Start()
}

func waitForParentRelease(parentPID int) {
	if parentPID <= 0 || runtime.GOOS != "windows" {
		time.Sleep(2 * time.Second)
		return
	}
	for i := 0; i < 60; i++ {
		cmd := buildWindowsHiddenPowerShellCommand(fmt.Sprintf(`if (Get-Process -Id %d -ErrorAction SilentlyContinue) { exit 1 }`, parentPID))
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func discardInstallRoot(installRoot string) error {
	if _, err := os.Stat(installRoot); err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	for i := 0; i < 20; i++ {
		if err := os.RemoveAll(installRoot); err == nil || isNotExist(err) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	if _, err := os.Stat(installRoot); err == nil {
		return fmt.Errorf("could not remove install root: %s", installRoot)
	}
	return nil
}

func finalizeWindowsUninstall(
	paths runtimePaths,
	report *uninstallReport,
	mode uninstallMode,
	status *windowsUninstallStatus,
	removedRelays uninstallRelayRemovalEvidence,
) error {
	state := loadStateOrDefault(paths)
	configDirExisted := fileExists(paths.ConfigDir)
	cleanupConfigDir := false
	var configCleanupErr error
	releaseMutation, acquired := acquireAutoRepairLockWithFinalizer(paths, func() {
		if !cleanupConfigDir {
			return
		}
		if !configDirExisted {
			configCleanupErr = os.Remove(paths.ConfigDir)
			if isNotExist(configCleanupErr) {
				configCleanupErr = nil
			}
			return
		}
		configCleanupErr = removeDirIfEmptyWithReport(paths.ConfigDir, report)
	})
	if !acquired {
		return failWindowsUninstallStatus(paths, status, "client_integrations", fmt.Errorf("another HA NOVA client update is already in progress"))
	}
	mutationReleased := false
	releaseMutationOnce := func() {
		if !mutationReleased {
			releaseMutation()
			mutationReleased = true
		}
	}
	defer releaseMutationOnce()
	state = loadStateOrDefault(paths)
	if err := finalizeLocalUninstallWithProgressUnlocked(
		paths,
		state,
		report,
		mode,
		func(step string) error {
			return updateWindowsUninstallStatusProgress(paths, status)
		},
		removedRelays,
	); err != nil {
		return failWindowsUninstallStatus(paths, status, normalizeUninstallFailureStep(err), err)
	}
	if err := removeLegacyWindowsPackageResidueForUninstall(paths, report); err != nil {
		report.addNote("Could not remove older private/test Windows package residue: " + err.Error())
	}
	if err := updateWindowsUninstallStatusProgress(paths, status); err != nil {
		return fmt.Errorf("cannot persist Windows uninstall recovery state: %w", err)
	}
	if _, err := os.Stat(paths.InstallRoot); err == nil {
		report.addRemoved(paths.InstallRoot)
	}
	if err := discardInstallRoot(paths.InstallRoot); err != nil {
		return failWindowsUninstallStatus(paths, status, "bundle_runtime_cleanup", err)
	}
	if err := removePathWithReport(paths.PublicBinary, report); err != nil && !isNotExist(err) {
		return failWindowsUninstallStatus(paths, status, "bundle_runtime_cleanup", err)
	}
	cleanupConfigDir = true
	releaseMutationOnce()
	if configCleanupErr != nil {
		return failWindowsUninstallStatus(paths, status, "config_cleanup", configCleanupErr)
	}
	return nil
}

func uninstallModeFromFlag(purge bool) uninstallMode {
	if purge {
		return uninstallModePurge
	}
	return uninstallModeStandard
}

func promptUninstallMode(defaultMode uninstallMode) (uninstallMode, bool, error) {
	defaultValue := "1"
	if defaultMode == uninstallModePurge {
		defaultValue = "2"
	}
	answer, err := promptLine("Choose uninstall mode: 1) Standard remove  2) Full purge  3) Cancel", defaultValue)
	if err != nil {
		return uninstallModeStandard, false, err
	}
	switch strings.TrimSpace(strings.ToLower(answer)) {
	case "", "1", "standard", "standard remove":
		return uninstallModeStandard, true, nil
	case "2", "purge", "full", "full purge":
		return uninstallModePurge, true, nil
	default:
		return uninstallModeStandard, false, nil
	}
}

func finalizeLocalUninstall(paths runtimePaths, state installState, report *uninstallReport, mode uninstallMode, teardownCompleted bool) error {
	return finalizeLocalUninstallWithProgress(paths, state, report, mode, nil, teardownCompleted)
}

func finalizeLocalUninstallWithProgress(paths runtimePaths, state installState, report *uninstallReport, mode uninstallMode, beforeStep func(string) error, teardownCompleted bool) error {
	configDirExisted := fileExists(paths.ConfigDir)
	cleanupConfigDir := false
	var configCleanupErr error
	releaseMutation, acquired := acquireAutoRepairLockWithFinalizer(paths, func() {
		if !cleanupConfigDir {
			return
		}
		if !configDirExisted {
			configCleanupErr = os.Remove(paths.ConfigDir)
			if isNotExist(configCleanupErr) {
				configCleanupErr = nil
			}
			return
		}
		configCleanupErr = removeDirIfEmptyWithReport(paths.ConfigDir, report)
	})
	if !acquired {
		return fmt.Errorf("another HA NOVA client update is already in progress")
	}
	state = loadStateOrDefault(paths)
	removedRelays := uninstallRelayRemovalEvidenceFromPreflight(
		collectUninstallPreflight(paths),
		teardownCompleted,
	)
	err := finalizeLocalUninstallWithProgressUnlocked(
		paths,
		state,
		report,
		mode,
		beforeStep,
		removedRelays,
	)
	cleanupConfigDir = err == nil
	releaseMutation()
	if err == nil {
		err = configCleanupErr
	}
	return err
}

func finalizeLocalUninstallWithProgressUnlocked(
	paths runtimePaths,
	state installState,
	report *uninstallReport,
	mode uninstallMode,
	beforeStep func(string) error,
	removedRelays uninstallRelayRemovalEvidence,
) error {
	relayTokenFile := ""
	var purgeTargets []profilePurgeTarget
	var purgeInventory fullPurgeInventory
	retirementProfiles, err :=
		deviceCredentialRetirementCheckpointProfiles(paths)
	if err != nil {
		return err
	}
	if mode != uninstallModePurge && len(retirementProfiles) > 0 {
		return fmt.Errorf(
			"device credential retirement is pending for server %q; run `%s` to finish it before uninstalling, or use `ha-nova uninstall --purge`",
			retirementProfiles[0],
			deviceRetirementSetupCommand(retirementProfiles[0]),
		)
	}
	if mode == uninstallModePurge {
		// Validate the whole raw Cloud identity set before any external revoke
		// or local deletion. Duplicate profile IDs must fail closed globally.
		cloudTargets, err := collectCloudPurgeTargets(
			paths.ConfigFile,
		)
		if err != nil {
			return fmt.Errorf(
				"failed to inspect Home Assistant Cloud authorization: %w",
				err,
			)
		}
		purgeTargets, err = collectProfilePurgeTargets(paths)
		if err != nil {
			return fmt.Errorf(
				"failed to inspect device credential cleanup: %w",
				err,
			)
		}
		if err := validateProfilePurgeTargets(purgeTargets); err != nil {
			return fmt.Errorf(
				"failed to validate device credential cleanup: %w",
				err,
			)
		}
		purgeInventory = newFullPurgeInventory(
			purgeTargets,
			cloudTargets,
		)
		if err := settleDeviceCredentialRetirementsForPurge(
			paths,
			report,
		); err != nil {
			return fmt.Errorf(
				"failed to settle pending device retirement: %w",
				err,
			)
		}
		if beforeStep != nil {
			if err := beforeStep("token_cleanup"); err != nil {
				return fmt.Errorf("failed before token_cleanup: %w", err)
			}
		}
		if err := purgeCloudAuthorizationsForUninstall(
			paths,
			report,
		); err != nil {
			return fmt.Errorf(
				"failed to revoke Home Assistant Cloud authorization: %w",
				err,
			)
		}
		// Cloud device revocation can durably checkpoint and then delete a
		// profile's native slot. Refresh the targets so the local sweep sees
		// that exact profile+slot proof instead of treating the now-absent
		// credential as unexplained state loss.
		purgeTargets, err = collectProfilePurgeTargets(paths)
		if err != nil {
			return fmt.Errorf(
				"failed to refresh device credential cleanup: %w",
				err,
			)
		}
		if err := validateProfilePurgeTargets(purgeTargets); err != nil {
			return fmt.Errorf(
				"failed to validate checkpointed device credential cleanup: %w",
				err,
			)
		}
		// Read the raw config document: token-file cleanup must not depend on
		// setup completeness (loadConfig fails when relay_base_url is missing,
		// which would silently skip service token file removal on purge).
		if doc, err := loadConfigDocument(paths.ConfigFile); err == nil {
			// Literal default profile: the legacy service token is
			// default-profile-only, regardless of where default_server points.
			if cfg, ok := doc.flatProfile(defaultServerProfileName); ok {
				relayTokenFile = strings.TrimSpace(cfg.RelayTokenFile)
			}
		}
		// Native-store deletion can have an ambiguous outcome. Keep config.json
		// as the durable retry target until every profile's device slots have
		// been confirmed absent.
		if err := purgeAllDeviceCredentialsWithReport(
			purgeTargets,
			report,
			removedRelays,
		); err != nil {
			return fmt.Errorf(
				"failed to remove device credentials: %w",
				err,
			)
		}
		if err := purgeInventory.captureFinalConfig(paths); err != nil {
			return err
		}
	}
	if beforeStep != nil {
		if err := beforeStep("client_integrations"); err != nil {
			return fmt.Errorf("failed before client_integrations: %w", err)
		}
	}
	if err := removeInstalledClientsWithReport(paths, state, report); err != nil {
		return fmt.Errorf("failed to remove client integrations: %w", err)
	}
	if err := removeClaudeProjectMemoryForUninstall(paths.Home, report); err != nil {
		report.addNote("Could not inspect Claude project memory: " + err.Error())
	}
	if beforeStep != nil {
		if err := beforeStep("path_cleanup"); err != nil {
			return fmt.Errorf("failed before path_cleanup: %w", err)
		}
	}
	pathRemoval, pathErr := removeManagedPathWithReport(paths, state)
	if pathErr != nil {
		return fmt.Errorf("failed to remove managed PATH entry: %w", pathErr)
	}
	if pathRemoval != "" {
		report.addRemoved(pathRemoval)
	}
	if beforeStep != nil {
		if err := beforeStep("config_cleanup"); err != nil {
			return fmt.Errorf("failed before config_cleanup: %w", err)
		}
	}
	if mode == uninstallModePurge {
		currentTargets, err :=
			purgeInventory.verifyFinalConfigAndTargets(paths)
		if err != nil {
			return err
		}
		if err := requirePurgedDeviceCredentialsAbsent(
			currentTargets,
		); err != nil {
			return fmt.Errorf(
				"device credentials reappeared before config cleanup: %w",
				err,
			)
		}
	}
	if err := removeManagedConfigArtifacts(paths, report, mode == uninstallModePurge); err != nil {
		return fmt.Errorf("failed to remove managed config artifacts: %w", err)
	}
	if beforeStep != nil {
		if err := beforeStep("cache_cleanup"); err != nil {
			return fmt.Errorf("failed before cache_cleanup: %w", err)
		}
	}
	if err := removeManagedCacheArtifacts(paths, report); err != nil {
		return fmt.Errorf("failed to remove managed cache artifacts: %w", err)
	}
	if mode == uninstallModePurge {
		tokenFileHandled := false
		if relayTokenFile != "" {
			var err error
			tokenFileHandled, err = applyUninstallServiceTokenFilePolicy(paths, relayTokenFile, report)
			if err != nil {
				return fmt.Errorf("failed to remove relay auth token: %w", err)
			}
		}
		if tokenFileHandled {
			restoreSuppression := withRelayAuthTokenFileSuppressed()
			applyUninstallKeyringTokenBestEffort(report)
			restoreSuppression()
		}
		if !tokenFileHandled {
			restoreSuppression := func() {}
			if relayTokenFile != "" {
				// The configured token file lies outside the managed config
				// directory; the boundary check above deliberately left it
				// alone. Never delete user-managed files — clean only the OS
				// keyring copy.
				restoreSuppression = withRelayAuthTokenFileSuppressed()
				report.addNote(fmt.Sprintf("Kept the relay token file outside the HA NOVA config directory: %s", relayTokenFile))
			}
			if err := applyUninstallTokenPolicy(report); err != nil {
				restoreSuppression()
				return fmt.Errorf("failed to remove relay auth token: %w", err)
			}
			restoreSuppression()
		}
		if err := removeDirIfEmptyWithReport(paths.ConfigDir, report); err != nil {
			return fmt.Errorf("failed to remove managed config directory: %w", err)
		}
	} else if cloudConfigurationExistsForUninstall(paths.ConfigFile) &&
		deviceCredentialExistsForUninstall() {
		report.addNote("Kept Home Assistant connection config, this device's pairing, and its Cloud authorization. Use 'ha-nova uninstall --purge' to remove and revoke them too.")
	} else if cloudConfigurationExistsForUninstall(paths.ConfigFile) {
		report.addNote("Kept Home Assistant connection config and its Cloud authorization. Use 'ha-nova uninstall --purge' to remove and revoke them too.")
	} else if deviceCredentialExistsForUninstall() {
		report.addNote("Kept Home Assistant connection config and this device's pairing. Use 'ha-nova uninstall --purge' to remove and revoke them too.")
	} else if fileExists(paths.ConfigFile) || relayAuthTokenExistsForUninstall() {
		report.addNote("Kept Home Assistant connection config and stored relay token. Use 'ha-nova uninstall --purge' to remove them too.")
	}
	return nil
}

func handleWindowsUninstallRecovery(recovery windowsUninstallStatusInspection, requestedMode uninstallMode) int {
	switch recovery.Kind {
	case windowsUninstallStatusKindRunning:
		printHumanErr("%s", recovery.Summary)
		printHumanWarn("Wait for the background uninstall to finish, then run `ha-nova doctor` if it does not complete.")
		return 1
	case windowsUninstallStatusKindInterrupted, windowsUninstallStatusKindFailed, windowsUninstallStatusKindCorrupt:
		requiredMode := normalizeUninstallMode(recovery.Status.Mode)
		if recovery.Kind == windowsUninstallStatusKindCorrupt {
			requiredMode = uninstallModeStandard
		}
		requestedMode = normalizeUninstallMode(string(requestedMode))
		if requestedMode != requiredMode {
			printHumanErr("%s", recovery.Summary)
			printHumanWarn("Recovery: run `%s`.", recovery.RecoveryCommand)
			return 1
		}
		printHumanWarn("%s", recovery.Summary)
		printHumanInfo("Retrying Windows uninstall recovery.")
		return 0
	default:
		return 0
	}
}

func normalizeUninstallFailureStep(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "client integrations"):
		return "client_integrations"
	case strings.Contains(message, "PATH entry"):
		return "path_cleanup"
	case strings.Contains(message, "config artifacts"), strings.Contains(message, "config directory"):
		return "config_cleanup"
	case strings.Contains(message, "cache artifacts"):
		return "cache_cleanup"
	case strings.Contains(message, "relay auth token"):
		return "token_cleanup"
	default:
		return ""
	}
}

func removeBundleRuntime(paths runtimePaths, report *uninstallReport) error {
	if err := removePathWithReport(paths.InstallRoot, report); err != nil && !isNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", paths.InstallRoot, err)
	}
	if err := removePathWithReport(paths.PublicBinary, report); err != nil && !isNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", paths.PublicBinary, err)
	}
	return nil
}

func relayAuthTokenExistsForUninstall() bool {
	token, err := readRelayAuthTokenForUninstall()
	return err == nil && strings.TrimSpace(token) != ""
}
