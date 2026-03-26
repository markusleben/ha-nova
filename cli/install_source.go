package main

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	installSourceBundle = "bundle"
	installSourceDev    = "dev"
)

var executablePathForInstallSource = os.Executable

func normalizeInstallSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case installSourceBundle:
		return installSourceBundle
	case installSourceDev:
		return installSourceDev
	default:
		return ""
	}
}

func detectInstallSource(paths runtimePaths, state installState) string {
	if strings.TrimSpace(os.Getenv("HA_NOVA_DEV_ROOT")) != "" {
		return installSourceDev
	}

	sourceRoot := resolveSourceRoot(paths)
	if sourceRoot != "" {
		if bundleInstallPresentOnDisk(sourceRoot) {
			if filepath.Clean(sourceRoot) != filepath.Clean(paths.InstallRoot) {
				return installSourceDev
			}
			return installSourceBundle
		}
		if filepath.Clean(sourceRoot) != filepath.Clean(paths.InstallRoot) {
			return installSourceDev
		}
	}

	if normalizeInstallSource(state.InstallSource) == installSourceBundle && bundleInstallPresentOnDisk(paths.InstallRoot) {
		return installSourceBundle
	}
	return installSourceBundle
}

func resolveSourceRoot(paths runtimePaths) string {
	if override := strings.TrimSpace(os.Getenv("HA_NOVA_DEV_ROOT")); override != "" {
		return filepath.Clean(override)
	}
	if exePath, err := executablePathForInstallSource(); err == nil {
		exeRoot := filepath.Dir(exePath)
		if _, err := os.Stat(filepath.Join(exeRoot, "bundle.json")); err == nil {
			return exeRoot
		}
	}
	return paths.InstallRoot
}

func sourceRootCandidates(paths runtimePaths) []string {
	candidates := []string{}
	addCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		value = filepath.Clean(value)
		for _, existing := range candidates {
			if filepath.Clean(existing) == value {
				return
			}
		}
		candidates = append(candidates, value)
	}

	if override := strings.TrimSpace(os.Getenv("HA_NOVA_DEV_ROOT")); override != "" {
		addCandidate(override)
	}
	if exePath, err := executablePathForInstallSource(); err == nil {
		addCandidate(filepath.Dir(exePath))
	}
	addCandidate(paths.InstallRoot)
	if cwd, err := os.Getwd(); err == nil {
		addCandidate(cwd)
		addCandidate(filepath.Join(cwd, ".."))
	}
	return candidates
}
