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
	forceFlag := fs.Bool("force", false, "proceed even from a local dev build (restores the release over the dev tree)")
	if err := fs.Parse(args); err != nil {
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
		printHumanWarn("updated to v%s, but could not remove the previous install backup: %s", targetVersion, err)
	}
	// Report the staged target, not localVersion(): the still-running process
	// may be the OLD (dev) binary whose compiled-in version predates the
	// update (issue #245 showed "Updated to v0.7.1" while installing 0.8.0).
	printHumanInfo("Updated to v%s", targetVersion)
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
	detectedClients, err := detectInstalledClients(paths)
	if err != nil {
		return err
	}
	state := loadStateOrDefault(paths)
	configured := normalizeClients(append(append([]string{}, state.InstalledClients...), detectedClients...))
	failed := []string{}
	skipped := false
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
	if len(failed) == 0 && !skipped && len(residue) == 0 {
		state.ClientsVerifiedVersion = version
	} else if len(residue) > 0 {
		// Residue is definitive evidence the tree is stale, so a previously-matching
		// marker is now wrong — clear it (do not merely skip re-stamping) so the
		// self-heal, which short-circuits on a matching marker, actually re-runs.
		state.ClientsVerifiedVersion = ""
	}
	if err := saveState(paths, state); err != nil {
		return err
	}
	if len(residue) > 0 {
		printHumanWarn("These clients still reference a temporary update backup and are not fully up to date: %s", strings.Join(normalizeClients(residue), ", "))
		printHumanWarn("Run `ha-nova doctor` to refresh them.")
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
