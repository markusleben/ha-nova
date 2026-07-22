package main

import (
	"os"
	"path/filepath"
)

func removeManagedConfigArtifacts(paths runtimePaths, report *uninstallReport, purge bool) error {
	for _, path := range managedConfigArtifactPaths(paths, purge) {
		if err := removePathWithReport(path, report); err != nil && !isNotExist(err) {
			return err
		}
	}
	return removeDirIfEmptyWithReport(paths.ConfigDir, report)
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
		paths.StateFile,
		paths.CensusFile,
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
		filepath.Join(paths.CacheDir, "automation-bp-snapshot.json"),
	}
	// Census action markers carry dynamic names (census-ping-<week>,
	// census-notice-<n>); leftovers would make a same-HOME reinstall inherit
	// "already attempted"/"already noticed" suppression.
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
