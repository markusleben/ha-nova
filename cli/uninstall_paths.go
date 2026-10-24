package main

import (
	"fmt"
	"os"
	"path/filepath"
)

var beforeUninstallConfigSnapshotRemoval func(string) error

func removeManagedConfigArtifacts(paths runtimePaths, report *uninstallReport, purge bool) error {
	return removeManagedConfigArtifactsWithSnapshot(
		paths,
		report,
		purge,
		nil,
		false,
		false,
	)
}

func removeManagedConfigArtifactsAtSnapshot(
	paths runtimePaths,
	report *uninstallReport,
	config []byte,
	configExists bool,
) error {
	return removeManagedConfigArtifactsWithSnapshot(
		paths,
		report,
		true,
		config,
		configExists,
		true,
	)
}

func removeManagedConfigArtifactsWithSnapshot(
	paths runtimePaths,
	report *uninstallReport,
	purge bool,
	config []byte,
	configExists bool,
	verifyConfigSnapshot bool,
) error {
	// Persist an opaque stop marker before removing actual install state.
	// A true no-op uninstall must not create residue on a clean machine.
	if installLifecycleEvidence(paths) {
		if err := markInstallLifecycleStopped(paths); err != nil {
			return fmt.Errorf("cannot stop install lifecycle: %w", err)
		}
		report.addDetail("Left a few tiny marker files so HA NOVA still running in an open AI app can't come back. They hold nothing about you and are never sent. The next setup removes them.")
	}
	for _, path := range managedConfigArtifactPaths(paths, purge) {
		report.setArea(uninstallAreaFiles)
		if path == paths.ConfigFile {
			report.setArea(uninstallAreaSettings)
		}
		if purge && path == paths.ConfigFile && verifyConfigSnapshot {
			if err := removeConfigAtSnapshot(
				path,
				config,
				configExists,
				report,
			); err != nil {
				return err
			}
			continue
		}
		if err := removePathWithReport(path, report); err != nil && !isNotExist(err) {
			return err
		}
	}
	return removeDirIfEmptyWithReport(paths.ConfigDir, report)
}

func removeConfigAtSnapshot(
	path string,
	expected []byte,
	expectedExists bool,
	report *uninstallReport,
) error {
	if beforeUninstallConfigSnapshotRemoval != nil {
		if err := beforeUninstallConfigSnapshotRemoval(path); err != nil {
			return fmt.Errorf("prepare verified config cleanup: %w", err)
		}
	}
	// The uninstall client-mutation lock excludes every supported config
	// writer. Recheck the exact bytes inside the removal function, after all
	// credential proofs and cleanup hooks, so no stale inventory is deleted.
	if err := ensureOptionalFileSnapshotCurrent(
		path,
		expected,
		expectedExists,
	); err != nil {
		return fmt.Errorf(
			"configuration changed before config cleanup: %w",
			err,
		)
	}
	if !expectedExists {
		return nil
	}
	if err := removePathWithReport(path, report); err != nil &&
		!isNotExist(err) {
		return err
	}
	return nil
}

func installLifecycleEvidence(paths runtimePaths) bool {
	for _, path := range []string{
		uninstallMarkerPath(paths),
		paths.StateFile,
		paths.ConfigFile,
		paths.VersionFile,
		paths.BundleFile,
		paths.PublicBinary,
		paths.InstallRoot,
	} {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil || !isNotExist(err) {
			return true
		}
	}
	return false
}

func removeManagedCacheArtifacts(paths runtimePaths, report *uninstallReport) error {
	report.setArea(uninstallAreaFiles)
	for _, path := range managedCacheArtifactPaths(paths) {
		if err := removePathWithReport(path, report); err != nil && !isNotExist(err) {
			return err
		}
	}
	return removeDirIfEmptyWithReport(paths.CacheDir, report)
}

func managedConfigArtifactPaths(paths runtimePaths, purge bool) []string {
	pathsList := []string{
		paths.StateFile,
		filepath.Join(paths.ConfigDir, "relay"),
		filepath.Join(paths.ConfigDir, "relay.exe"),
		filepath.Join(paths.ConfigDir, "update"),
		filepath.Join(paths.ConfigDir, "update.cmd"),
		filepath.Join(paths.ConfigDir, "version-check"),
		filepath.Join(paths.ConfigDir, "check-update.cmd"),
		filepath.Join(paths.ConfigDir, "version.json"),
		filepath.Join(paths.ConfigDir, "onboarding.env"),
		filepath.Join(paths.ConfigDir, "doctor-cache.env"),
		filepath.Join(paths.ConfigDir, "claude-marketplace"),
		filepath.Join(paths.ConfigDir, "undo-snapshot.json"),
		filepath.Join(paths.ConfigDir, "undo-snapshots.json"),
		demoProfilePath(paths),
	}
	if purge {
		pathsList = append(pathsList, paths.ConfigFile)
	}
	return pathsList
}

func managedCacheArtifactPaths(paths runtimePaths) []string {
	return []string{
		paths.UpdateCacheFile,
		filepath.Join(paths.CacheDir, updateNudgeMarkerName),
		filepath.Join(paths.CacheDir, updateRefreshMarkerName),
		filepath.Join(paths.CacheDir, "relay-outdated-warned"),
		filepath.Join(paths.CacheDir, "automation-bp-snapshot.json"),
		filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker),
		filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile),
		phoneResultsFile(paths),
		planDir(paths), // pending confirmations: short-lived, worthless after uninstall
	}
}

func removeDirIfEmptyWithReport(path string, report *uninstallReport) error {
	if path == "" {
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	if err := os.Remove(path); err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	report.addRemovedIn(uninstallAreaFiles, path)
	return nil
}
