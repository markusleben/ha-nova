package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func removeManagedConfigArtifacts(paths runtimePaths, report *uninstallReport, purge bool) error {
	// Serialize removal with census sends and opt-out. The coordinator lives
	// outside ConfigDir (home-directory flock on Unix, named mutex on Windows),
	// so removing census.json and then ConfigDir cannot unlink the active lock
	// or let another process enter against a replacement inode.
	release, ok := acquireCensusLock(paths)
	if !ok {
		return fmt.Errorf("cannot acquire census state lock for uninstall")
	}
	defer release()
	// Persist an opaque stop marker before removing actual install/census state.
	// A true no-op uninstall must not create residue on a clean machine.
	if censusLifecycleEvidence(paths) {
		if err := markCensusLifecycleStopped(paths); err != nil {
			return fmt.Errorf("cannot stop census lifecycle: %w", err)
		}
		report.addNote("Retained two opaque random local safety markers so stale processes cannot restore the install or restart the census; neither is sent, and successful setup removes the census stop marker while rotating the install generation.")
	}
	for _, path := range managedConfigArtifactPaths(paths, purge) {
		if err := removePathWithReport(path, report); err != nil && !isNotExist(err) {
			return err
		}
	}
	return removeDirIfEmptyWithReport(paths.ConfigDir, report)
}

func censusLifecycleEvidence(paths runtimePaths) bool {
	for _, path := range []string{
		censusLifecycleMarkerPath(paths),
		paths.StateFile,
		paths.CensusFile,
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
	for _, path := range managedCacheArtifactPaths(paths) {
		if err := removePathWithReport(path, report); err != nil && !isNotExist(err) {
			return err
		}
	}
	return removeDirIfEmptyWithReport(paths.CacheDir, report)
}

func managedConfigArtifactPaths(paths runtimePaths, purge bool) []string {
	pathsList := []string{
		// Lifecycle sentinel first. Its removal happens while the census lock is
		// held; every queued census writer then fails closed before it can
		// recreate census.json or ConfigDir.
		paths.StateFile,
		paths.CensusFile,
		// Pre-v0.21 Windows builds placed census state under roaming APPDATA.
		// Delete it, never migrate its potentially cross-device consent.
		filepath.Join(paths.ConfigDir, "census.json"),
		// Remove the pre-v0.21 lock-file artifact. Current locking never uses a
		// file inside ConfigDir.
		filepath.Join(paths.ConfigDir, "census.lock"),
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
	}
	if purge {
		pathsList = append(pathsList, paths.ConfigFile)
	}
	return pathsList
}

func managedCacheArtifactPaths(paths runtimePaths) []string {
	list := []string{
		paths.UpdateCacheFile,
		filepath.Join(paths.CacheDir, updateNudgeMarkerName),
		filepath.Join(paths.CacheDir, updateRefreshMarkerName),
		filepath.Join(paths.CacheDir, "relay-outdated-warned"),
		filepath.Join(paths.CacheDir, "automation-bp-snapshot.json"),
		filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker),
		filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile),
	}
	// Clean pre-v0.21 census action markers. Current census serialization does
	// not create cache markers.
	if paths.CacheDir != "" {
		if markers, err := filepath.Glob(filepath.Join(paths.CacheDir, "census-*")); err == nil {
			list = append(list, markers...)
		}
	}
	return list
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
	report.addRemoved(path)
	return nil
}
