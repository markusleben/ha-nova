package main

import (
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
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	waitForParentRelease(*parentPID)
	paths, err := detectPaths()
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	preflight := collectUninstallPreflight(paths)
	status, err := beginWindowsUninstallStatus(paths, uninstallModeFromFlag(*purge), installSourceBundle)
	if err != nil {
		printHumanErr("cannot persist Windows uninstall recovery state: %s", err)
		return 1
	}
	report := &uninstallReport{}
	if err := finalizeWindowsUninstall(paths, report, uninstallModeFromFlag(*purge), status); err != nil {
		report.printDetails()
		printHumanErr("%s", err)
		return 1
	}
	applyUninstallPreflightNotes(report, preflight)
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
	yes := fs.Bool("yes", false, "skip confirmation")
	purge := fs.Bool("purge", false, "remove config and token")
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}

	state := loadStateOrDefault(paths)
	channels := inspectInstallChannels(paths, state)
	source := channels.CurrentSource
	mode := uninstallModeFromFlag(*purge)
	recoveryMode := false
	if channels.Conflict {
		printHumanErr("%s", installChannelConflictMessage(channels, "ha-nova uninstall"))
		return 1
	}
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
	if runtime.GOOS == "windows" && source == installSourceBundle {
		printUninstallPreflightNotes(os.Stdout, preflight)
		if err := launchWindowsUninstall(paths, mode); err != nil {
			printHumanErr("cannot finish Windows uninstall: %s", err)
			return 1
		}
		printHumanInfo("HA NOVA uninstall continues in the background on Windows.")
		printHumanInfo("It is safe to close this terminal now.")
		return 0
	}
	if runtime.GOOS == "windows" && source == installSourceWinget {
		printUninstallPreflightNotes(os.Stdout, preflight)
		if err := launchWindowsWingetUninstall(mode); err != nil {
			printHumanErr("cannot finish Windows winget uninstall: %s", err)
			return 1
		}
		printHumanInfo("HA NOVA uninstall continues in the background on Windows.")
		printHumanInfo("It is safe to close this terminal now.")
		return 0
	}

	report := &uninstallReport{}
	if source == installSourceWinget {
		if err := runWingetUninstall(); err != nil {
			printHumanErr("winget uninstall failed: %s", err)
			return 1
		}
		report.addRemoved("winget package " + wingetPackageID)
	} else if source == installSourceBundle {
		if err := removeBundleRuntime(paths, report); err != nil {
			printHumanErr("%s", err)
			return 1
		}
	}

	if err := finalizeLocalUninstall(paths, state, report, mode); err != nil {
		printHumanErr("%s", err)
		return 1
	}

	applyUninstallPreflightNotes(report, preflight)
	if report.print() {
		printHumanInfo("HA NOVA removed")
	}
	return 0
}

func runInternalWingetUninstall(_ runtimePaths, args []string) int {
	fs := flag.NewFlagSet("internal-winget-uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	parentPID := fs.Int("parent-pid", 0, "parent pid")
	selfPath := fs.String("self-path", "", "temp helper path")
	purge := fs.Bool("purge", false, "remove config and token")
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	waitForParentRelease(*parentPID)
	paths, err := detectPaths()
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	preflight := collectUninstallPreflight(paths)
	mode := uninstallModeFromFlag(*purge)
	status, err := beginWindowsUninstallStatus(paths, mode, installSourceWinget)
	if err != nil {
		printHumanErr("cannot persist Windows uninstall recovery state: %s", err)
		return 1
	}
	report := &uninstallReport{}
	if err := finalizeLocalUninstallWithProgress(paths, loadStateOrDefault(paths), report, mode, func(step string) error {
		return updateWindowsUninstallStatusProgress(paths, status)
	}); err != nil {
		report.printDetails()
		printHumanErr("%s", failWindowsUninstallStatus(paths, status, normalizeUninstallFailureStep(err), err))
		return 1
	}
	if err := updateWindowsUninstallStatusProgress(paths, status); err != nil {
		printHumanErr("cannot persist Windows uninstall recovery state: %s", err)
		return 1
	}
	if err := runWingetUninstall(); err != nil {
		printHumanErr("winget uninstall failed: %s", failWindowsUninstallStatus(paths, status, "winget_runtime_cleanup", err))
		return 1
	}
	report.addRemoved("winget package " + wingetPackageID)
	applyUninstallPreflightNotes(report, preflight)
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

func launchWindowsUninstall(paths runtimePaths, mode uninstallMode) error {
	tempHelper := filepath.Join(os.TempDir(), "ha-nova-uninstall-"+strconv.Itoa(os.Getpid())+".exe")
	if err := copyFile(filepath.Join(paths.InstallRoot, publicBinaryName()), tempHelper); err != nil {
		return err
	}
	args := []string{"internal-uninstall", "--parent-pid", strconv.Itoa(os.Getpid()), "--self-path", tempHelper}
	if mode == uninstallModePurge {
		args = append(args, "--purge")
	}
	return launchWindowsDetachedHelper(tempHelper, args...)
}

func launchWindowsWingetUninstall(mode uninstallMode) error {
	tempHelper, err := stageWindowsLifecycleHelper("ha-nova-winget-uninstall-")
	if err != nil {
		return err
	}
	args := []string{"internal-winget-uninstall", "--parent-pid", strconv.Itoa(os.Getpid()), "--self-path", tempHelper}
	if mode == uninstallModePurge {
		args = append(args, "--purge")
	}
	return launchWindowsDetachedHelper(tempHelper, args...)
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

func finalizeWindowsUninstall(paths runtimePaths, report *uninstallReport, mode uninstallMode, status *windowsUninstallStatus) error {
	state := loadStateOrDefault(paths)
	if err := finalizeLocalUninstallWithProgress(paths, state, report, mode, func(step string) error {
		return updateWindowsUninstallStatusProgress(paths, status)
	}); err != nil {
		return failWindowsUninstallStatus(paths, status, normalizeUninstallFailureStep(err), err)
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

func finalizeLocalUninstall(paths runtimePaths, state installState, report *uninstallReport, mode uninstallMode) error {
	return finalizeLocalUninstallWithProgress(paths, state, report, mode, nil)
}

func finalizeLocalUninstallWithProgress(paths runtimePaths, state installState, report *uninstallReport, mode uninstallMode, beforeStep func(string) error) error {
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
		if err := applyUninstallTokenPolicy(report); err != nil {
			return fmt.Errorf("failed to remove relay auth token: %w", err)
		}
		if err := removeDirIfEmptyWithReport(paths.ConfigDir, report); err != nil {
			return fmt.Errorf("failed to remove managed config directory: %w", err)
		}
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
		if normalizeUninstallMode(string(requestedMode)) != requiredMode {
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

func stageWindowsLifecycleHelper(prefix string) (string, error) {
	tempHelper := filepath.Join(os.TempDir(), prefix+strconv.Itoa(os.Getpid())+".exe")
	currentExe, err := executablePathForInstallSource()
	if err != nil {
		return "", err
	}
	if err := copyFile(currentExe, tempHelper); err != nil {
		return "", err
	}
	return tempHelper, nil
}
