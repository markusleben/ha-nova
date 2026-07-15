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

const postUpdateSessionInstruction = "Next step: start a new AI client session to load the updated HA NOVA skills."
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
		printHumanInfo("%s", windowsUpdateStagedInstruction)
		return 0
	}
	defer cleanupStagedBundle(stageRoot)

	rollbackInstall, commitInstall, err := applyStagedBundleWithRollback(paths, stageRoot)
	if err != nil {
		printHumanErr("cannot apply update: %s", err)
		return 1
	}
	syncResult := postUpdateSyncWithResult(paths)
	if syncResult.Err != nil {
		syncErr := syncResult.Err
		if rollbackErr := rollbackInstall(); rollbackErr != nil {
			printPostUpdateSyncFailure(syncErr)
			printHumanWarn("rollback failed: %s", rollbackErr)
			return 1
		}
		if restoreErr := postUpdateSync(paths); restoreErr != nil {
			printHumanErr("update aborted: %s", syncErr)
			printHumanWarn("runtime rollback succeeded, but restoring client integrations failed: %s", restoreErr)
			return 1
		}
		printHumanErr("update aborted: %s", syncErr)
		return 1
	}
	if err := commitInstall(); err != nil {
		printHumanWarn("updated to v%s, but could not remove the previous install backup: %s", targetVersion, err)
	}
	// Report the staged target, not localVersion(): the still-running process
	// may be the OLD (dev) binary whose compiled-in version predates the
	// update (issue #245 showed "Updated to v0.7.1" while installing 0.8.0).
	printHumanInfo("Updated to v%s", targetVersion)
	// The freshly installed version.json may raise min_relay_version — the
	// update moment is where the user can still act on it, instead of being
	// interrupted mid-session by the proxy warning later.
	if notice := relayFloorNotice(paths); !notice.empty() {
		printHumanNotice(notice)
		maybeOfferGuidedRelayUpdate(paths, notice)
	}
	printPostUpdateSessionInstructionIfFullySynced(syncResult.FullySynced)
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
	syncResult := postUpdateSyncForReplace(paths)
	if syncResult.Err != nil {
		syncErr := syncResult.Err
		if rollbackErr := rollbackInstall(); rollbackErr != nil {
			printPostUpdateSyncFailure(syncErr)
			printHumanWarn("rollback failed: %s", rollbackErr)
			return 1
		}
		if restoreResult := postUpdateSyncForReplace(paths); restoreResult.Err != nil {
			printHumanErr("update aborted: %s", syncErr)
			printHumanWarn("runtime rollback succeeded, but restoring client integrations failed: %s", restoreResult.Err)
			return 1
		}
		printHumanErr("update aborted: %s", syncErr)
		return 1
	}
	if err := commitInstall(); err != nil {
		printHumanWarn("updated to v%s, but could not remove the previous install backup: %s", localVersion(paths), err)
	}
	printHumanInfo("Updated to v%s", localVersion(paths))
	// Windows finishes the update in this replacement process — the freshly
	// installed version.json is live here, so the relay-floor warning belongs
	// here too (the staging branch in runUpdate exits before it). No guided
	// prompt here on purpose: this helper runs in the background with stdin
	// unwired (windowsHelperLaunchProfile attaches output only) and the
	// console is already back at the shell — the documented Windows contract
	// is background-complete, never same-console-interactive. Point at the
	// interactive path instead.
	if notice := relayFloorNotice(paths); !notice.empty() {
		printHumanNotice(notice)
		printHumanInfo("Run `ha-nova doctor` in a terminal to be offered the guided relay update.")
	}
	printPostUpdateSessionInstructionIfFullySynced(syncResult.FullySynced)
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
	syncResult := postUpdateSyncWithResult(paths)
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
	// "Up to date" is misleading while the relay sits below the floor the
	// installed skills need — say so here, where the user is looking.
	if notice := relayFloorNotice(paths); !notice.empty() {
		printHumanNotice(notice)
		maybeOfferGuidedRelayUpdate(paths, notice)
	}
	printPostUpdateSessionInstructionIfFullySynced(syncResult.FullySynced)
	return 0
}

func printPostUpdateSessionInstructionIfFullySynced(fullySynced bool) {
	if !fullySynced {
		return
	}
	printHumanInfo("%s", postUpdateSessionInstruction)
}

type postUpdateSyncResult struct {
	FullySynced bool
	Err         error
}

// postUpdateSync re-syncs every configured client from the canonical install root
// and, when the whole set verified cleanly, stamps the client-verification marker
// (state.ClientsVerifiedVersion) that gates the post-update self-heal (see
// ensureClientsVerifiedForCurrentVersion).
//
// The marker is stamped only when EVERY configured client was actually synced —
// not when one was skipped because its runtime is absent in this environment, not
// when one failed, and not when the post-sync residue scan still finds a
// transient-backup path in a synced tree. Otherwise a client skipped here (e.g.
// its CLI is not on PATH right now) would be marked verified and then never
// repaired once its runtime reappears, because the marker would already match.
// Leaving the marker unset re-attempts those clients on the next
// check-update/doctor (which run at most once per session), so the cost of an
// unresolvable client stays bounded.
//
// The residue scan is the in-tool guarantee behind "Updated == clean": if a sync
// ever resolves from a stale backup, the user gets a loud warning + recovery hint
// instead of a silent success. For a fixed (>=0.6.2) binary it never fires.
func postUpdateSync(paths runtimePaths) error {
	return postUpdateSyncWithResult(paths).Err
}

func postUpdateSyncWithResult(paths runtimePaths) postUpdateSyncResult {
	detectedClients, err := detectInstalledClients(paths)
	if err != nil {
		return postUpdateSyncResult{Err: err}
	}
	state := loadStateOrDefault(paths)
	configured := normalizeClients(append(append([]string{}, state.InstalledClients...), detectedClients...))
	failed := []string{}
	skipped := false
	for _, client := range configured {
		entry, ok, err := findRegistryClient(paths, client)
		if err != nil {
			return postUpdateSyncResult{Err: err}
		}
		if !ok {
			continue
		}
		status := evaluateClientStatus(paths, state, entry)
		if !status.RuntimeDetected {
			printHumanWarn("Skipping %s until the client runtime is installed in this environment", entry.Label)
			skipped = true
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
	version := localVersion(paths)
	state.Version = version
	// Verify the just-synced trees are actually clean before marking the version
	// verified: a residue scan catches a sync that resolved from a transient backup
	// (the pre-0.6.1 bug class), so "Updated" never silently means "updated-ish".
	residue := transientBackupResidue(paths, configured)
	fullySynced := len(failed) == 0 && !skipped && len(residue) == 0
	if fullySynced {
		state.ClientsVerifiedVersion = version
	} else if len(residue) > 0 {
		// Residue proves the installed client tree is stale. Clear a matching marker
		// so the next check-update or doctor run retries the repair.
		state.ClientsVerifiedVersion = ""
	}
	if err := saveState(paths, state); err != nil {
		return postUpdateSyncResult{Err: err}
	}
	if len(residue) > 0 {
		printHumanWarn("These clients still reference a temporary update backup and are not fully up to date: %s", strings.Join(normalizeClients(residue), ", "))
		printHumanWarn("Run `ha-nova doctor` to refresh them.")
	}
	if len(failed) > 0 {
		return postUpdateSyncResult{Err: fmt.Errorf("failed clients: %s", strings.Join(normalizeClients(failed), ", "))}
	}
	return postUpdateSyncResult{FullySynced: fullySynced}
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
