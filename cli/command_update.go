package main

import (
	"flag"
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

	targetVersion := strings.TrimPrefix(strings.TrimSpace(*versionFlag), "v")
	if targetVersion == "" {
		release, err := fetchLatestRelease(paths, true)
		if err != nil {
			printHumanErr("%s", err)
			return 1
		}
		targetVersion = release.Version
	}
	currentVersion := localVersion(paths)
	if currentVersion != "dev" && compareSemver(currentVersion, targetVersion) >= 0 {
		printHumanInfo("Already up to date: v%s", currentVersion)
		return 0
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

func postUpdateSync(paths runtimePaths) error {
	state := loadStateOrDefault(paths)
	detectedClients, err := detectInstalledClients(paths)
	if err != nil {
		return err
	}
	configured := normalizeClients(append(append([]string{}, state.InstalledClients...), detectedClients...))
	synced := []string{}
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
			if containsClient(state.InstalledClients, client) {
				synced = append(synced, client)
			}
			continue
		}
		if err := installClients(paths, &state, []string{client}); err != nil {
			return err
		}
		synced = append(synced, client)
	}
	state.InstalledClients = normalizeClients(synced)
	state.Version = localVersion(paths)
	return saveState(paths, state)
}

func launchWindowsReplace(paths runtimePaths, stageRoot string) error {
	tempHelper := filepath.Join(os.TempDir(), "ha-nova-updater-"+strconv.Itoa(os.Getpid())+".exe")
	if err := copyFile(filepath.Join(paths.InstallRoot, publicBinaryName()), tempHelper); err != nil {
		return err
	}
	cmd := buildWindowsHelperCommand(tempHelper, "internal-replace", "--parent-pid", strconv.Itoa(os.Getpid()), "--stage-root", stageRoot)
	return cmd.Start()
}
