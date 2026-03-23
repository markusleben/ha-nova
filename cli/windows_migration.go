package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func migrateLegacyWindowsDirs(paths runtimePaths) {
	if runtime.GOOS != "windows" {
		return
	}
	migrateLegacyWindowsConfigDir(paths)
	migrateLegacyWindowsCacheDir(paths)
}

func migrateLegacyWindowsConfigDir(paths runtimePaths) {
	legacyDir := filepath.Join(paths.Home, ".config", "ha-nova")
	moveLegacyDirEntries(legacyDir, paths.ConfigDir, func(name string) bool {
		return strings.HasSuffix(strings.ToLower(name), ".dpapi")
	})
}

func migrateLegacyWindowsCacheDir(paths runtimePaths) {
	legacyDir := filepath.Join(paths.Home, ".cache", "ha-nova")
	moveLegacyDirEntries(legacyDir, paths.CacheDir, nil)
}

func moveLegacyDirEntries(sourceDir, targetDir string, skip func(name string) bool) {
	if sourceDir == "" || targetDir == "" || filepath.Clean(sourceDir) == filepath.Clean(targetDir) {
		return
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return
	}
	for _, entry := range entries {
		if skip != nil && skip(entry.Name()) {
			continue
		}
		sourcePath := filepath.Join(sourceDir, entry.Name())
		targetPath := filepath.Join(targetDir, entry.Name())
		if _, err := os.Stat(targetPath); err == nil {
			continue
		}
		_ = os.Rename(sourcePath, targetPath)
	}
	remaining, err := os.ReadDir(sourceDir)
	if err != nil {
		return
	}
	if len(remaining) == 0 {
		_ = os.Remove(sourceDir)
	}
}
