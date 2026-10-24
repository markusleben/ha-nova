package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
)

const postUpdateSessionInstruction = "Next step: start a new chat in your AI app so it uses the new version."
const postUpdatePartialSessionInstruction = "Next step: start a new chat in each updated AI app so it uses the new version."
const windowsUpdateStagedInstruction = "Installing the update in the background. When it says \"Updated HA NOVA\" (about a minute), start a new chat in your AI app."

func runUpdate(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	versionFlag := fs.String("version", "", "install exactly this release tag (e.g. v0.14.1); default is the latest stable")
	forceFlag := fs.Bool("force", false, "proceed even from a local dev build (restores the release over the dev tree)")
	server := fs.String("server", "", "server profile whose Relay App update is checked afterwards (multi-server installs)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova update [--version <tag>] [--force] [--server <name>]") {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}
	if err := selectServerFlag(fs, *server); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if *server != "" { // a mistyped server fails before anything is installed
		if _, err := loadConfig(paths); err != nil {
			printHumanErr("%s; nothing was updated", err)
			return 1
		}
	}
	updateLifecycleMarker, err := readInstallLifecycleGeneration(paths)
	if err != nil {
		printHumanErr("cannot inspect install lifecycle: %s", err)
		return 1
	}
	if installLifecycleStopped(paths) {
		printHumanErr("HA NOVA is not set up on this computer yet; run: ha-nova setup")
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
			printHumanErr("Can't find the new version right now. Nothing was changed. Check your internet, then try again.")
			printHumanWarn("Details: %s", err)
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
			code := syncInstalledClientsForCurrentVersion(paths, currentVersion, targetVersion, cmp, updateLifecycleMarker)
			noteLegacyAccess(paths)
			return code
		}
	}

	if !explicitTarget {
		for _, line := range backToStableNotice(currentVersion, targetVersion) {
			printHumanInfo("%s", line)
		}
	}
	// Before the new version goes in: this binary may be the last release that
	// accepts the shared access, and the one it installs may not.
	noteLegacyAccess(paths)
	stageRoot, err := stageBundle(paths, targetVersion)
	if err != nil {
		printHumanErr("The update could not be downloaded or checked. Nothing was changed. Try again later; if it keeps failing, run: ha-nova doctor")
		printHumanWarn("Details: %s", err)
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
		printHumanErr("Another HA NOVA update is running. Wait for it, then try again.")
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
			printHumanWarn("HA NOVA went back to the old version, but couldn't set it up again in your AI apps. Run ha-nova setup. Details: %s", restoreErr)
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
	printUpdateDone("Updated HA NOVA to "+targetVersion, syncResult)
	// The freshly installed version.json may raise min_relay_version, and Home
	// Assistant may expose a newer compatible App update. The update moment is
	// where the user can still act instead of being interrupted later.
	offerRelayUpdateAfterUpdate(paths)
	printPostUpdateSessionInstruction(syncResult)
	return 0
}

// noteLegacyAccess: `ha-nova update` names the shared-access switch when it
// applies: before it installs a newer version, or when already current.
func noteLegacyAccess(paths runtimePaths) {
	ctx, cancel := context.WithTimeout(context.Background(), legacyNoticeProbeTimeout)
	defer cancel()
	printLegacyAccessNotice(ctx, paths, false)
}

func runInternalReplace(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("internal-replace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stageRoot := fs.String("stage-root", "", "stage root")
	parentPID := fs.Int("parent-pid", 0, "parent pid")
	lifecycleMarkerFlag := fs.String("lifecycle-marker", "", "captured install lifecycle marker")
	selfPath := fs.String("self-path", "", "temporary helper executable to delete after exit")
	if err := fs.Parse(args); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	// The helper is a copy of the CLI under %TEMP%; whatever happens below,
	// it must not outlive this run (same detached sleeper as the uninstaller).
	defer func() {
		if err := scheduleWindowsSelfDeleteForUpdate(*selfPath); err != nil {
			printHumanWarn("could not schedule update helper cleanup: %s", err)
		}
	}()
	// Registered before the argument checks so a rejected marker or stage
	// root does not strand the staged bundle (no-op on an empty path).
	defer cleanupStagedBundle(*stageRoot)
	if *stageRoot == "" {
		printHumanErr("missing --stage-root")
		return 1
	}
	lifecycleMarker, err := decodeUpdateLifecycleMarker(*lifecycleMarkerFlag)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	waitForParentReleaseForReplace(*parentPID)
	releaseMutation, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		printHumanErr("Another HA NOVA update is running. Wait for it, then try again.")
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
			printHumanWarn("HA NOVA went back to the old version, but couldn't set it up again in your AI apps. Run ha-nova setup. Details: %s", restoreResult.Err)
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
	printUpdateDone("Updated HA NOVA to "+localVersion(paths), syncResult)
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
		doctor := "ha-nova doctor"
		if name, _ := requestedServerSelection(); name != "" {
			doctor += " --server " + name
		}
		if notice.appUpdate { // doctor can offer only an update Home Assistant has
			printHumanInfo("Or run `%s` in a terminal: it offers to update the NOVA App for you.", doctor) // the notice names the Home Assistant path
		}
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
	// A repair counts as new to the AI app: a sync that hadn't finished for
	// this version and now did (an app that is always skipped never stamps
	// it), or a Claude Code copy older than it.
	unverified := loadStateOrDefault(paths).ClientsVerifiedVersion != currentVersion
	claudeOlder := claudeHANovaAt(claudeConfigRoot(paths.Home), currentVersion) == claudeHANovaOlder
	syncResult := postUpdateSyncWithResultForLifecycle(paths, lifecycleMarker)
	if syncResult.Err != nil {
		if cmp > 0 {
			printHumanErr("Already on newer version v%s than target v%s, but client sync failed: %s", currentVersion, targetVersion, syncResult.Err)
		} else {
			printHumanErr("Already up to date: v%s, but client sync failed: %s", currentVersion, syncResult.Err)
		}
		return 1
	}
	printUpdateDone("You already have HA NOVA "+currentVersion, syncResult)
	if cmp > 0 {
		printHumanInfo("It is newer than %s.", targetVersion)
	}
	// "Already have" is misleading while the relay is below the required floor
	// or Home Assistant exposes a newer App update — say so where the user is
	// looking.
	offerRelayUpdateAfterUpdate(paths)
	if unverified && syncResult.FullySynced || claudeOlder { // else nothing new went in: no "start a new chat" step
		printPostUpdateSessionInstruction(syncResult)
	}
	return 0
}

func printPostUpdateSessionInstruction(result postUpdateSyncResult) {
	if result.FullySynced {
		printHumanInfo("%s", postUpdateSessionInstruction)
	} else if result.RefreshedClients {
		printHumanInfo("%s", postUpdatePartialSessionInstruction)
	}
}

func buildWindowsReplaceCommand(paths runtimePaths, tempHelper, stageRoot string, lifecycleMarker []byte) *exec.Cmd {
	cmd := buildWindowsHelperCommand(
		tempHelper,
		"internal-replace",
		"--parent-pid", strconv.Itoa(os.Getpid()),
		"--stage-root", stageRoot,
		"--lifecycle-marker", encodeUpdateLifecycleMarker(lifecycleMarker),
		"--self-path", tempHelper,
	)
	cmd.Env = append(os.Environ(), helperInstallRootEnv(paths.InstallRoot)...)
	if serverSelectionOverride != "" { // the helper checks this server's Relay App too
		cmd.Env = append(cmd.Env, serverSelectionEnvVar+"="+serverSelectionOverride)
	}
	return cmd
}

func launchWindowsReplace(paths runtimePaths, stageRoot string, lifecycleMarker []byte) error {
	tempHelper, err := stageWindowsHelperCopy(paths, windowsUpdateHelperPrefix, stageRoot)
	if err != nil {
		_ = os.Remove(tempHelper)
		cleanupStagedBundle(stageRoot)
		return err
	}
	if err := buildWindowsReplaceCommand(paths, tempHelper, stageRoot, lifecycleMarker).Start(); err != nil {
		// Nobody will run the helper: remove it and the staged bundle now,
		// otherwise both leak under %TEMP% on every failed launch.
		_ = os.Remove(tempHelper)
		cleanupStagedBundle(stageRoot)
		return err
	}
	return nil
}

func ensureUpdateLifecycleCurrent(paths runtimePaths, lifecycleMarker []byte) error {
	current, err := readInstallLifecycleGeneration(paths)
	if err != nil {
		return fmt.Errorf("cannot verify update lifecycle: %w", err)
	}
	if !bytes.Equal(current, lifecycleMarker) || installLifecycleStopped(paths) {
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
