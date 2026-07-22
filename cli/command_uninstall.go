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
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	paths, err := detectPaths()
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	status, err := beginWindowsUninstallStatus(paths, uninstallModeFromFlag(*purge), installSourceBundle)
	if err != nil {
		printHumanErr("cannot persist Windows uninstall recovery state: %s", err)
		return 1
	}
	stopHeartbeat := startWindowsUninstallHeartbeat(paths, status)
	defer stopHeartbeat()
	waitForParentReleaseForUninstall(*parentPID)
	preflight := collectUninstallPreflight(paths)
	report := &uninstallReport{}
	if err := finalizeWindowsUninstall(paths, report, uninstallModeFromFlag(*purge), status, *teardownDone); err != nil {
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
	purge := fs.Bool("purge", false, "also remove config, state, and the relay auth token (retains one opaque census stop marker)")
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
	if recovery := inspectWindowsUninstallStatus(paths); recovery.Kind != windowsUninstallStatusKindNone {
		if exitCode := handleWindowsUninstallRecovery(recovery, mode); exitCode != 0 {
			return exitCode
		}
		recoveryMode = recovery.Kind == windowsUninstallStatusKindInterrupted || recovery.Kind == windowsUninstallStatusKindFailed || recovery.Kind == windowsUninstallStatusKindCorrupt
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

	preflight := collectUninstallPreflight(paths)
	teardown := teardownNotOffered
	if !*yes && !recoveryMode && isInteractiveTTY() {
		outcome, err := maybeOfferGuidedTeardown(bufio.NewReader(os.Stdin), os.Stdout, preflight, defaultTeardownDeps())
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
		if outcome == teardownCancelled {
			printHumanInfo("Uninstall cancelled — nothing was removed.")
			return 0
		}
		teardown = outcome
		if teardown == teardownCompleted {
			preflight.relayStillRunning = false
		}
	}
	if runtime.GOOS == "windows" && source == installSourceBundle {
		if teardown == teardownCompleted {
			printTeardownCompletedNotes(os.Stdout, mode)
		} else {
			printUninstallPreflightNotes(os.Stdout, preflight)
		}
		if err := launchWindowsUninstall(paths, mode, teardown == teardownCompleted); err != nil {
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
	if err := finalizeLocalUninstall(paths, state, report, mode, teardown == teardownCompleted); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if source == installSourceBundle {
		if err := removeBundleRuntime(paths, report); err != nil {
			printHumanErr("%s", err)
			return 1
		}
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

func launchWindowsUninstall(paths runtimePaths, mode uninstallMode, teardownDone bool) error {
	tempHelper := filepath.Join(os.TempDir(), "ha-nova-uninstall-"+strconv.Itoa(os.Getpid())+".exe")
	if err := copyFile(filepath.Join(paths.InstallRoot, publicBinaryName()), tempHelper); err != nil {
		return err
	}
	statusTicks := windowsUninstallStatusMarkerTicks(paths.UninstallStatusFile)
	args := []string{"internal-uninstall", "--parent-pid", strconv.Itoa(os.Getpid()), "--self-path", tempHelper}
	if mode == uninstallModePurge {
		args = append(args, "--purge")
	}
	if teardownDone {
		args = append(args, "--teardown-done")
	}
	return launchWindowsDetachedHelperWithEnv(tempHelper, paths.UninstallStatusFile, statusTicks, helperInstallRootEnv(paths.InstallRoot), args...)
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

func finalizeWindowsUninstall(paths runtimePaths, report *uninstallReport, mode uninstallMode, status *windowsUninstallStatus, relayAlreadyRemoved bool) error {
	state := loadStateOrDefault(paths)
	if err := finalizeLocalUninstallWithProgress(paths, state, report, mode, func(step string) error {
		return updateWindowsUninstallStatusProgress(paths, status)
	}, relayAlreadyRemoved); err != nil {
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

func finalizeLocalUninstall(paths runtimePaths, state installState, report *uninstallReport, mode uninstallMode, relayAlreadyRemoved bool) error {
	return finalizeLocalUninstallWithProgress(paths, state, report, mode, nil, relayAlreadyRemoved)
}

func finalizeLocalUninstallWithProgress(paths runtimePaths, state installState, report *uninstallReport, mode uninstallMode, beforeStep func(string) error, relayAlreadyRemoved bool) error {
	relayTokenFile := ""
	var purgeTargets []profilePurgeTarget
	if mode == uninstallModePurge {
		// Read the raw config document: token-file cleanup must not depend on
		// setup completeness (loadConfig fails when relay_base_url is missing,
		// which would silently skip service token file removal on purge).
		// EVERY profile's secure endpoint is captured here too — config_cleanup
		// removes config.json before token_cleanup runs the device revokes, and
		// each profile's device entry lives on ITS relay.
		if doc, err := loadConfigDocument(paths.ConfigFile); err == nil {
			// Literal default profile: the legacy service token is
			// default-profile-only, regardless of where default_server points.
			if cfg, ok := doc.flatProfile(defaultServerProfileName); ok {
				relayTokenFile = strings.TrimSpace(cfg.RelayTokenFile)
			}
			for _, name := range doc.profileNames() {
				cfg, ok := doc.flatProfile(name)
				if !ok {
					continue
				}
				purgeTargets = append(purgeTargets, profilePurgeTarget{
					name:          name,
					secureBaseURL: strings.TrimSpace(cfg.RelaySecureBaseURL),
					spkiPin:       strings.TrimSpace(cfg.RelaySpkiPin),
				})
			}
		}
		if len(purgeTargets) == 0 {
			// Config gone or unreadable: still clear the active profile's slots.
			purgeTargets = append(purgeTargets, profilePurgeTarget{name: activeServerProfile()})
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
		if beforeStep != nil {
			if err := beforeStep("token_cleanup"); err != nil {
				return fmt.Errorf("failed before token_cleanup: %w", err)
			}
		}
		purgeAllDeviceCredentialsWithReport(purgeTargets, report, relayAlreadyRemoved)
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
