package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func runInternalUninstall(_ runtimePaths, args []string) int {
	fs := flag.NewFlagSet("internal-uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	parentPID := fs.Int("parent-pid", 0, "parent pid")
	selfPath := fs.String("self-path", "", "temp helper path")
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
	report := &uninstallReport{}
	if err := finalizeWindowsUninstall(paths, report); err != nil {
		report.printDetails()
		printHumanErr("%s", err)
		return 1
	}
	applyUninstallPreflightNotes(report, preflight)
	if report.print() {
		printHumanInfo("HA NOVA removed")
		printHumanWarn("If PowerShell is still waiting now, press Ctrl+C once to return to a fresh prompt.")
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
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	renderUninstallPreflight(os.Stdout)
	if !*yes && isInteractiveTTY() {
		answer, err := promptLine("Remove HA NOVA completely? (yes/no)", "no")
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "yes" {
			printHumanInfo("Uninstall cancelled")
			return 0
		}
	}

	preflight := collectUninstallPreflight(paths)
	state := loadStateOrDefault(paths)
	if runtime.GOOS == "windows" {
		printUninstallPreflightNotes(os.Stdout, preflight)
		if err := launchWindowsUninstall(paths); err != nil {
			printHumanErr("cannot finish Windows uninstall: %s", err)
			return 1
		}
		printHumanInfo("HA NOVA uninstall is finishing on Windows. Wait for the final 'HA NOVA removed' line below.")
		return 0
	}
	report := &uninstallReport{}
	if err := removeInstalledClientsWithReport(paths, state, report); err != nil {
		printHumanErr("failed to remove client integrations: %s", err)
		return 1
	}
	if err := removeClaudeProjectMemoryForUninstall(paths.Home, report); err != nil {
		report.addNote("Could not inspect Claude project memory: " + err.Error())
	}
	tokenDeleteErr := applyUninstallTokenPolicy(report)
	pathRemoval, pathErr := removeManagedPathWithReport(paths, state)
	if pathErr != nil {
		printHumanErr("failed to remove managed PATH entry: %s", pathErr)
		return 1
	}
	if pathRemoval != "" {
		report.addRemoved(pathRemoval)
	}
	if err := removeManagedConfigArtifacts(paths, report); err != nil {
		printHumanErr("failed to remove managed config artifacts: %s", err)
		return 1
	}
	if err := removeManagedCacheArtifacts(paths, report); err != nil {
		printHumanErr("failed to remove managed cache artifacts: %s", err)
		return 1
	}
	if err := removePathWithReport(paths.InstallRoot, report); err != nil && !isNotExist(err) {
		printHumanErr("failed to remove %s: %s", paths.InstallRoot, err)
		return 1
	}
	if err := removePathWithReport(paths.PublicBinary, report); err != nil && !isNotExist(err) {
		printHumanErr("failed to remove %s: %s", paths.PublicBinary, err)
		return 1
	}
	applyUninstallPreflightNotes(report, preflight)
	if tokenDeleteErr != nil {
		report.printDetails()
		printHumanErr("failed to remove relay auth token: %s", tokenDeleteErr)
		return 1
	}
	if report.print() {
		printHumanInfo("HA NOVA removed")
	}
	return 0
}

func launchWindowsUninstall(paths runtimePaths) error {
	tempHelper := filepath.Join(os.TempDir(), "ha-nova-uninstall-"+strconv.Itoa(os.Getpid())+".exe")
	if err := copyFile(filepath.Join(paths.InstallRoot, publicBinaryName()), tempHelper); err != nil {
		return err
	}
	cmd := buildWindowsHelperCommand(tempHelper, "internal-uninstall", "--parent-pid", strconv.Itoa(os.Getpid()), "--self-path", tempHelper)
	return cmd.Start()
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
		cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", fmt.Sprintf(`if (Get-Process -Id %d -ErrorAction SilentlyContinue) { exit 1 }`, parentPID))
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func discardInstallRoot(installRoot string) error {
	parent := filepath.Dir(installRoot)
	discardRoot := filepath.Join(parent, ".ha-nova-removed-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if _, err := os.Stat(installRoot); err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Rename(installRoot, discardRoot); err != nil {
		return err
	}
	for i := 0; i < 20; i++ {
		if err := os.RemoveAll(discardRoot); err == nil || isNotExist(err) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	if _, err := os.Stat(discardRoot); err == nil {
		return fmt.Errorf("could not remove discarded install root: %s", discardRoot)
	}
	return nil
}

func finalizeWindowsUninstall(paths runtimePaths, report *uninstallReport) error {
	state := loadStateOrDefault(paths)
	if err := removeInstalledClientsWithReport(paths, state, report); err != nil {
		return err
	}
	if err := removeClaudeProjectMemoryForUninstall(paths.Home, report); err != nil {
		report.addNote("Could not inspect Claude project memory: " + err.Error())
	}
	if _, err := os.Stat(paths.InstallRoot); err == nil {
		report.addRemoved(paths.InstallRoot)
	}
	if err := discardInstallRoot(paths.InstallRoot); err != nil {
		return err
	}
	tokenDeleteErr := applyUninstallTokenPolicy(report)
	pathRemoval, pathErr := removeManagedPathWithReport(paths, state)
	if pathErr != nil {
		return pathErr
	}
	if pathRemoval != "" {
		report.addRemoved(pathRemoval)
	}
	if err := removeManagedConfigArtifacts(paths, report); err != nil {
		return err
	}
	if err := removeManagedCacheArtifacts(paths, report); err != nil {
		return err
	}
	if err := removePathWithReport(paths.PublicBinary, report); err != nil && !isNotExist(err) {
		return err
	}
	if tokenDeleteErr != nil {
		return fmt.Errorf("failed to remove relay auth token: %w", tokenDeleteErr)
	}
	return nil
}
