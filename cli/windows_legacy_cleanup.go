package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const legacyWindowsPackageID = "markusleben.ha-nova"

func legacyWindowsPackageLinkPath(paths runtimePaths) string {
	return filepath.Join(windowsLocalAppDataDir(paths.Home), "Microsoft", "WinGet", "Links", publicBinaryName())
}

func legacyWindowsPackagesRoot(paths runtimePaths) string {
	return filepath.Join(windowsLocalAppDataDir(paths.Home), "Microsoft", "WinGet", "Packages")
}

func legacyWindowsPackageResiduePaths(paths runtimePaths) []string {
	residue := []string{legacyWindowsPackageLinkPath(paths)}
	entries, err := os.ReadDir(legacyWindowsPackagesRoot(paths))
	if err != nil {
		return normalizePathList(residue)
	}
	legacyID := strings.ToLower(legacyWindowsPackageID)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		entryName := strings.ToLower(entry.Name())
		if entryName != legacyID && !strings.HasPrefix(entryName, legacyID+"_") {
			continue
		}
		residue = append(residue, filepath.Join(legacyWindowsPackagesRoot(paths), entry.Name()))
	}
	sort.Strings(residue)
	return normalizePathList(residue)
}

func removeLegacyWindowsPackageResidue(paths runtimePaths, report *uninstallReport) error {
	for _, path := range legacyWindowsPackageResiduePaths(paths) {
		if err := removePathWithReport(path, report); err != nil && !isNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
	}
	if err := removeDirIfEmptyWithReport(filepath.Dir(legacyWindowsPackageLinkPath(paths)), nil); err != nil && !isNotExist(err) {
		return err
	}
	if err := removeDirIfEmptyWithReport(legacyWindowsPackagesRoot(paths), nil); err != nil && !isNotExist(err) {
		return err
	}
	return nil
}
