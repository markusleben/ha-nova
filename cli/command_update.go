package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

const postUpdateSessionInstruction = "Next step: start a new AI client session to load the updated HA NOVA skills."
const postUpdatePartialSessionInstruction = "Next step for refreshed clients: start a new AI client session in each one to load the updated HA NOVA skills."
const windowsUpdateStagedInstruction = "Update staged. Wait for the updater to finish. After it reports success, start a new AI client session to load the updated HA NOVA skills."

func runUpdate(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	versionFlag := fs.String("version", "", "install exactly this release tag (e.g. v0.14.1); default is the latest stable")
	forceFlag := fs.Bool("force", false, "proceed even from a local dev build (restores the release over the dev tree)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova update [--version <tag>] [--force]") {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}
	updateLifecycleMarker, err := readInstallLifecycleGeneration(paths)
	if err != nil {
		printHumanErr("cannot inspect install lifecycle: %s", err)
		return 1
	}
	if censusLifecycleStopped(paths) {
		printHumanErr("HA NOVA was uninstalled; run `ha-nova setup` before updating.")
		return 1
	}

	targetVersion, err := normalizeExplicitVersion(*versionFlag)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	explicitTarget := targetVersion != ""

	// A locally dev-synced build (BuildChannel=dev) must not be silently replaced
	// with the published release: `ha-nova update` with no explicit target would
	// overwrite the developer's working tree. The nudge is already suppressed for
	// dev builds; this guards the explicit command too. Require --version/--force.
	if BuildChannel == "dev" && targetVersion == "" && !*forceFlag {
		printHumanErr("Local dev build detected (dev-sync) — `ha-nova update` would replace it with the published release.")
		printHumanWarn("To restore the release deliberately, re-run with `--force` (or `--version <tag>`).")
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
	// On a dev build, an explicit restore (--force OR --version <tag>, both
	// offered by the guard above) must stage the release even when version.json
	// matches the target: localVersion reads version.json (a release value like
	// 0.6.0, not "dev"), so without this the up-to-date short-circuit below keeps
	// the dev binary in place and the restore silently does nothing.
	forcingDevRestore := BuildChannel == "dev" && (*forceFlag || explicitTarget)
	if currentVersion != "dev" && !forcingDevRestore {
		cmp, err := compareReleaseVersions(currentVersion, targetVersion)
		if err != nil {
			printHumanErr("cannot compare version v%s with target v%s: %s", currentVersion, targetVersion, err)
			return 1
		}
		if cmp >= 0 && !isStableTargetFromRC(currentVersion, targetVersion) {
			return syncInstalledClientsForCurrentVersion(paths, currentVersion, targetVersion, cmp, updateLifecycleMarker)
		}
	}

	stageRoot, err := stageBundle(paths, targetVersion)
	if err != nil {
		printHumanErr("update failed: %s", err)
		return 1
	}

	if runtime.GOOS == "windows" {
		if err := launchWindowsReplace(paths, stageRoot, updateLifecycleMarker); err != nil {
			printHumanErr("cannot start Windows updater: %s", err)
			return 1
		}
		printHumanInfo("%s", windowsUpdateStagedInstruction)
		return 0
	}
	defer cleanupStagedBundle(stageRoot)

	releaseMutation, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		printHumanErr("update blocked: another HA NOVA client update is already in progress")
		return 1
	}
	if err := ensureUpdateLifecycleCurrent(paths, updateLifecycleMarker); err != nil {
		releaseMutation()
		printHumanErr("%s", err)
		return 1
	}
	rollbackInstall, commitInstall, err := applyStagedBundleWithRollback(paths, stageRoot)
	if err != nil {
		releaseMutation()
		printHumanErr("cannot apply update: %s", err)
		return 1
	}
	syncResult := postUpdateSyncWithResultUnlocked(paths)
	if syncResult.Err != nil {
		syncErr := syncResult.Err
		if rollbackErr := rollbackInstall(); rollbackErr != nil {
			releaseMutation()
			printPostUpdateSyncFailure(syncErr)
			printHumanWarn("rollback failed: %s", rollbackErr)
			return 1
		}
		if restoreErr := postUpdateSyncWithResultUnlocked(paths).Err; restoreErr != nil {
			releaseMutation()
			printHumanErr("update aborted: %s", syncErr)
			printHumanWarn("runtime rollback succeeded, but restoring client integrations failed: %s", restoreErr)
			return 1
		}
		releaseMutation()
		printHumanErr("update aborted: %s", syncErr)
		return 1
	}
	if err := commitInstall(); err != nil {
		printHumanWarn("updated to v%s, but could not remove the previous install backup: %s", targetVersion, err)
	}
	releaseMutation()
	// Report the staged target, not localVersion(): the still-running process
	// may be the OLD (dev) binary whose compiled-in version predates the
	// update (issue #245 showed "Updated to v0.7.1" while installing 0.8.0).
	printHumanInfo("Updated to v%s", targetVersion)
	// The freshly installed version.json may raise min_relay_version, and Home
	// Assistant may expose a newer compatible App update. The update moment is
	// where the user can still act instead of being interrupted later.
	if notice := relayUpdateNotice(paths); !notice.empty() {
		printHumanNotice(notice)
		maybeOfferGuidedRelayUpdate(paths, notice)
	}
	printPostUpdateSessionInstruction(syncResult)
	// One-time census ask on the interactive update tail (existing users meet
	// the question here). Never on the Windows internal-replace path — that
	// helper runs detached without stdin.
	maybeAskCensus(paths, "update")
	return 0
}

func runInternalReplace(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("internal-replace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stageRoot := fs.String("stage-root", "", "stage root")
	parentPID := fs.Int("parent-pid", 0, "parent pid")
	lifecycleMarkerFlag := fs.String("lifecycle-marker", "", "captured install lifecycle marker")
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if *stageRoot == "" {
		printHumanErr("missing --stage-root")
		return 1
	}
	lifecycleMarker, err := decodeUpdateLifecycleMarker(*lifecycleMarkerFlag)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	defer cleanupStagedBundle(*stageRoot)
	waitForParentReleaseForReplace(*parentPID)
	releaseMutation, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		printHumanErr("update blocked: another HA NOVA client update is already in progress")
		return 1
	}
	if err := ensureUpdateLifecycleCurrent(paths, lifecycleMarker); err != nil {
		releaseMutation()
		printHumanErr("%s", err)
		return 1
	}
	rollbackInstall, commitInstall, err := applyStagedBundleWithRollbackForReplace(paths, *stageRoot)
	if err != nil {
		releaseMutation()
		printHumanErr("%s", err)
		return 1
	}
	syncResult := postUpdateSyncForReplace(paths)
	if syncResult.Err != nil {
		syncErr := syncResult.Err
		if rollbackErr := rollbackInstall(); rollbackErr != nil {
			releaseMutation()
			printPostUpdateSyncFailure(syncErr)
			printHumanWarn("rollback failed: %s", rollbackErr)
			return 1
		}
		if restoreResult := postUpdateSyncForReplace(paths); restoreResult.Err != nil {
			releaseMutation()
			printHumanErr("update aborted: %s", syncErr)
			printHumanWarn("runtime rollback succeeded, but restoring client integrations failed: %s", restoreResult.Err)
			return 1
		}
		releaseMutation()
		printHumanErr("update aborted: %s", syncErr)
		return 1
	}
	if err := commitInstall(); err != nil {
		printHumanWarn("updated to v%s, but could not remove the previous install backup: %s", localVersion(paths), err)
	}
	releaseMutation()
	printHumanInfo("Updated to v%s", localVersion(paths))
	// Windows finishes the update in this replacement process — the freshly
	// installed version.json and Relay App-update state are live here, so the
	// notice belongs here too (the staging branch in runUpdate exits before it).
	// No guided prompt here: this helper runs in the background with stdin
	// unwired (windowsHelperLaunchProfile attaches output only) and the
	// console is already back at the shell — the documented Windows contract
	// is background-complete, never same-console-interactive. Point at the
	// interactive path instead.
	if notice := relayUpdateNotice(paths); !notice.empty() {
		printHumanNotice(notice)
		printHumanInfo("Run `ha-nova doctor` in a terminal to be offered the guided relay update.")
	}
	printPostUpdateSessionInstruction(syncResult)
	return 0
}

func runInternalSyncClients(paths runtimePaths, _ []string) int {
	if err := postUpdateSync(paths); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	return 0
}

func syncInstalledClientsForCurrentVersion(paths runtimePaths, currentVersion, targetVersion string, cmp int, lifecycleMarker []byte) int {
	syncResult := postUpdateSyncWithResultForLifecycle(paths, lifecycleMarker)
	if syncResult.Err != nil {
		if cmp > 0 {
			printHumanErr("Already on newer version v%s than target v%s, but client sync failed: %s", currentVersion, targetVersion, syncResult.Err)
		} else {
			printHumanErr("Already up to date: v%s, but client sync failed: %s", currentVersion, syncResult.Err)
		}
		return 1
	}
	if cmp > 0 {
		printHumanInfo("Already on newer version v%s than target v%s", currentVersion, targetVersion)
	} else {
		printHumanInfo("Already up to date: v%s", currentVersion)
	}
	// "Up to date" is misleading while the relay is below the required floor
	// or Home Assistant exposes a newer App update — say so where the user is
	// looking.
	if notice := relayUpdateNotice(paths); !notice.empty() {
		printHumanNotice(notice)
		maybeOfferGuidedRelayUpdate(paths, notice)
	}
	printPostUpdateSessionInstruction(syncResult)
	// Same census-ask tail as a real update: "already up to date" is the far
	// more common interactive `ha-nova update` outcome.
	maybeAskCensus(paths, "update")
	return 0
}

func printPostUpdateSessionInstruction(result postUpdateSyncResult) {
	if result.FullySynced {
		printHumanInfo("%s", postUpdateSessionInstruction)
	} else if result.RefreshedClients {
		printHumanInfo("%s", postUpdatePartialSessionInstruction)
	}
}

func launchWindowsReplace(paths runtimePaths, stageRoot string, lifecycleMarker []byte) error {
	tempHelper := filepath.Join(os.TempDir(), "ha-nova-updater-"+strconv.Itoa(os.Getpid())+".exe")
	if err := copyFile(filepath.Join(paths.InstallRoot, publicBinaryName()), tempHelper); err != nil {
		return err
	}
	cmd := buildWindowsHelperCommand(
		tempHelper,
		"internal-replace",
		"--parent-pid", strconv.Itoa(os.Getpid()),
		"--stage-root", stageRoot,
		"--lifecycle-marker", encodeUpdateLifecycleMarker(lifecycleMarker),
	)
	cmd.Env = append(os.Environ(), helperInstallRootEnv(paths.InstallRoot)...)
	return cmd.Start()
}

func ensureUpdateLifecycleCurrent(paths runtimePaths, lifecycleMarker []byte) error {
	current, err := readInstallLifecycleGeneration(paths)
	if err != nil {
		return fmt.Errorf("cannot verify update lifecycle: %w", err)
	}
	if !bytes.Equal(current, lifecycleMarker) || censusLifecycleStopped(paths) {
		return fmt.Errorf("update was superseded by an uninstall; run `ha-nova setup` before updating")
	}
	return nil
}

func encodeUpdateLifecycleMarker(lifecycleMarker []byte) string {
	if len(lifecycleMarker) == 0 {
		return "none"
	}
	return hex.EncodeToString(lifecycleMarker)
}

func decodeUpdateLifecycleMarker(value string) ([]byte, error) {
	if value == "none" {
		return nil, nil
	}
	if value == "" {
		return nil, fmt.Errorf("missing --lifecycle-marker")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) == 0 {
		return nil, fmt.Errorf("invalid --lifecycle-marker")
	}
	return decoded, nil
}
