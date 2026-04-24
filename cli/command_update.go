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
)

func runUpdate(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	versionFlag := fs.String("version", "", "explicit version")
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}

	targetVersion, err := normalizeExplicitVersion(*versionFlag)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	source := detectInstallSource(paths, state)
	if source == installSourceLegacyWindowsPackage {
		printHumanErr("Legacy private/test Windows package installs are no longer supported for in-place update.")
		printHumanWarn("Remove the old HA NOVA app in Installed Apps / App Installer, then reinstall with:")
		printHumanWarn("  irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex")
		return 1
	}
	if recovery := inspectWindowsUninstallStatus(paths); recovery.Kind != windowsUninstallStatusKindNone {
		switch recovery.Kind {
		case windowsUninstallStatusKindRunning:
			printHumanErr("%s", recovery.Summary)
			printHumanWarn("Wait for the background uninstall to finish before running `ha-nova update` again.")
			return 1
		case windowsUninstallStatusKindInterrupted, windowsUninstallStatusKindFailed, windowsUninstallStatusKindCorrupt:
			printHumanErr("%s", recovery.Summary)
			printHumanWarn("Recovery: run `%s` first.", recovery.RecoveryCommand)
			return 1
		}
	}
	if targetVersion == "" {
		release, err := fetchLatestRelease(paths, true, false)
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
		targetVersion = release.Version
	}
	currentVersion := localVersion(paths)
	if currentVersion != "dev" {
		cmp, err := compareReleaseVersions(currentVersion, targetVersion)
		if err != nil {
			printHumanErr("cannot compare version v%s with target v%s: %s", currentVersion, targetVersion, err)
			return 1
		}
		if cmp >= 0 && !isStableTargetFromRC(currentVersion, targetVersion) {
			return syncInstalledClientsForCurrentVersion(paths, currentVersion, targetVersion, cmp)
		}
	}

	stageRoot, err := stageBundle(paths, targetVersion)
	if err != nil {
		printHumanErr("update failed: %s", err)
		return 1
	}

	if runtime.GOOS == "windows" {
		if err := launchWindowsReplace(paths, stageRoot); err != nil {
			printHumanErr("cannot start Windows updater: %s", err)
			return 1
		}
		printHumanInfo("Update staged. Restart your shell/client after the updater finishes.")
		return 0
	}
	defer cleanupStagedBundle(stageRoot)

	rollbackInstall, commitInstall, err := applyStagedBundleWithRollback(paths, stageRoot)
	if err != nil {
		printHumanErr("cannot apply update: %s", err)
		return 1
	}
	if err := postUpdateSync(paths); err != nil {
		if rollbackErr := rollbackInstall(); rollbackErr != nil {
			printPostUpdateSyncFailure(err)
			printHumanWarn("rollback failed: %s", rollbackErr)
			return 1
		}
		if restoreErr := postUpdateSync(paths); restoreErr != nil {
			printHumanErr("update aborted: %s", err)
			printHumanWarn("runtime rollback succeeded, but restoring client integrations failed: %s", restoreErr)
			return 1
		}
		printHumanErr("update aborted: %s", err)
		return 1
	}
	if err := commitInstall(); err != nil {
		printHumanWarn("updated to v%s, but could not remove the previous install backup: %s", localVersion(paths), err)
	}
	printHumanInfo("Updated to v%s", localVersion(paths))
	return 0
}

func runInternalReplace(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("internal-replace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stageRoot := fs.String("stage-root", "", "stage root")
	parentPID := fs.Int("parent-pid", 0, "parent pid")
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if *stageRoot == "" {
		printHumanErr("missing --stage-root")
		return 1
	}
	defer cleanupStagedBundle(*stageRoot)
	waitForParentReleaseForReplace(*parentPID)
	rollbackInstall, commitInstall, err := applyStagedBundleWithRollbackForReplace(paths, *stageRoot)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if err := postUpdateSyncForReplace(paths); err != nil {
		if rollbackErr := rollbackInstall(); rollbackErr != nil {
			printPostUpdateSyncFailure(err)
			printHumanWarn("rollback failed: %s", rollbackErr)
			return 1
		}
		if restoreErr := postUpdateSyncForReplace(paths); restoreErr != nil {
			printHumanErr("update aborted: %s", err)
			printHumanWarn("runtime rollback succeeded, but restoring client integrations failed: %s", restoreErr)
			return 1
		}
		printHumanErr("update aborted: %s", err)
		return 1
	}
	if err := commitInstall(); err != nil {
		printHumanWarn("updated to v%s, but could not remove the previous install backup: %s", localVersion(paths), err)
	}
	printHumanInfo("Updated to v%s", localVersion(paths))
	return 0
}

func runInternalSyncClients(paths runtimePaths, _ []string) int {
	if err := postUpdateSync(paths); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	return 0
}

func syncInstalledClientsForCurrentVersion(paths runtimePaths, currentVersion, targetVersion string, cmp int) int {
	if err := postUpdateSync(paths); err != nil {
		if cmp > 0 {
			printHumanErr("Already on newer version v%s than target v%s, but client sync failed: %s", currentVersion, targetVersion, err)
		} else {
			printHumanErr("Already up to date: v%s, but client sync failed: %s", currentVersion, err)
		}
		return 1
	}
	if cmp > 0 {
		printHumanInfo("Already on newer version v%s than target v%s", currentVersion, targetVersion)
		return 0
	}
	printHumanInfo("Already up to date: v%s", currentVersion)
	return 0
}

func postUpdateSync(paths runtimePaths) error {
	detectedClients, err := detectInstalledClients(paths)
	if err != nil {
		return err
	}
	state := loadStateOrDefault(paths)
	configured := normalizeClients(append(append([]string{}, state.InstalledClients...), detectedClients...))
	failed := []string{}
	for _, client := range configured {
		entry, ok, err := findRegistryClient(paths, client)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		status := evaluateClientStatus(paths, state, entry)
		if !status.RuntimeDetected {
			printHumanWarn("Skipping %s until the client runtime is installed in this environment", entry.Label)
			continue
		}
		if err := installClients(paths, &state, []string{client}); err != nil {
			printHumanWarn("Client sync failed: %s (%s)", client, err)
			failed = append(failed, client)
			continue
		}
		printHumanInfo("Client synced: %s", client)
	}
	state.InstalledClients = configured
	state.Version = localVersion(paths)
	if err := saveState(paths, state); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed clients: %s", strings.Join(normalizeClients(failed), ", "))
	}
	return nil
}

func launchWindowsReplace(paths runtimePaths, stageRoot string) error {
	tempHelper := filepath.Join(os.TempDir(), "ha-nova-updater-"+strconv.Itoa(os.Getpid())+".exe")
	if err := copyFile(filepath.Join(paths.InstallRoot, publicBinaryName()), tempHelper); err != nil {
		return err
	}
	cmd := buildWindowsHelperCommand(tempHelper, "internal-replace", "--parent-pid", strconv.Itoa(os.Getpid()), "--stage-root", stageRoot)
	cmd.Env = append(os.Environ(), helperInstallRootEnv(paths.InstallRoot)...)
	return cmd.Start()
}
