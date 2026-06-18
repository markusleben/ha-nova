package main

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	installSourceBundle               = "bundle"
	installSourceDev                  = "dev"
	installSourceLegacyWindowsPackage = "legacy_windows_package"
)

var executablePathForInstallSource = os.Executable

func normalizeInstallSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case installSourceBundle:
		return installSourceBundle
	case installSourceDev:
		return installSourceDev
	case installSourceLegacyWindowsPackage, "winget":
		return installSourceLegacyWindowsPackage
	default:
		return ""
	}
}

func detectInstallSource(paths runtimePaths, state installState) string {
	if strings.TrimSpace(os.Getenv("HA_NOVA_DEV_ROOT")) != "" {
		return installSourceDev
	}
	if legacyWindowsPackageBinaryRunning() {
		return installSourceLegacyWindowsPackage
	}

	// A current bundle install on disk takes precedence over on-disk WinGet
	// legacy residue: install.ps1 no longer blocks on leftover private/test
	// WinGet package paths, so a fresh bundle install over residue must
	// classify as bundle — update/uninstall then work, and uninstall removes
	// the residue via removeLegacyWindowsPackageResidue. Only a binary that
	// itself runs from a WinGet-managed path still classifies as legacy.
	sourceRoot := resolveSourceRoot(paths)
	if sourceRoot != "" && bundleInstallPresentOnDisk(sourceRoot) {
		if filepath.Clean(sourceRoot) != filepath.Clean(paths.InstallRoot) {
			return installSourceDev
		}
		return installSourceBundle
	}
	if legacyWindowsPackageSourcePresent(paths) {
		return installSourceLegacyWindowsPackage
	}
	if sourceRoot != "" && filepath.Clean(sourceRoot) != filepath.Clean(paths.InstallRoot) {
		return installSourceDev
	}
	if channelChecksUseWindowsPlatform() && normalizeInstallSource(state.InstallSource) == installSourceLegacyWindowsPackage && !bundleInstallPresentOnDisk(paths.InstallRoot) {
		return installSourceLegacyWindowsPackage
	}

	if normalizeInstallSource(state.InstallSource) == installSourceBundle && bundleInstallPresentOnDisk(paths.InstallRoot) {
		return installSourceBundle
	}
	return installSourceBundle
}

// isTransientInstallBackup reports whether dir is one of the updater's transient
// swap siblings (.ha-nova-next-/old-/failed-). During an in-place update the
// running binary is renamed into `.ha-nova-old-*` (and that backup still carries
// a bundle.json), so os.Executable() inside postUpdateSync would otherwise make
// resolveSourceRoot pick the stale, about-to-be-deleted tree as the client
// source. Keying off the backup basename is the only signal that distinguishes
// this from a legitimate portable install whose exe sits in a stable custom dir.
func isTransientInstallBackup(dir string) bool {
	base := filepath.Base(filepath.Clean(dir))
	return strings.HasPrefix(base, installBackupPrefixOld) ||
		strings.HasPrefix(base, installBackupPrefixNext) ||
		strings.HasPrefix(base, installBackupPrefixFailed)
}

func resolveSourceRoot(paths runtimePaths) string {
	if override := strings.TrimSpace(os.Getenv("HA_NOVA_DEV_ROOT")); override != "" {
		return filepath.Clean(override)
	}
	if exePath, err := executablePathForInstallSource(); err == nil {
		exeRoot := filepath.Dir(exePath)
		// Never resolve the source from a transient update backup: the swap
		// guarantees paths.InstallRoot already holds the fresh bundle before
		// postUpdateSync runs (and the restored old bundle after a rollback) —
		// both correct, while the backup is stale and about to be deleted.
		if !isTransientInstallBackup(exeRoot) {
			if _, err := os.Stat(filepath.Join(exeRoot, "bundle.json")); err == nil {
				return exeRoot
			}
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
		// Same guard as resolveSourceRoot: a transient update backup must never
		// become a client-registry source candidate (locateClientRegistry).
		if exeDir := filepath.Dir(exePath); !isTransientInstallBackup(exeDir) {
			addCandidate(exeDir)
		}
	}
	addCandidate(paths.InstallRoot)
	if cwd, err := os.Getwd(); err == nil {
		addCandidate(cwd)
		addCandidate(filepath.Join(cwd, ".."))
	}
	return candidates
}

func legacyWindowsPackageBinaryRunning() bool {
	if !channelChecksUseWindowsPlatform() {
		return false
	}
	exePath, err := executablePathForInstallSource()
	return err == nil && isLegacyWindowsPackageManagedPath(exePath)
}

func legacyWindowsPackageSourcePresent(paths runtimePaths) bool {
	if !channelChecksUseWindowsPlatform() {
		return false
	}
	if legacyWindowsPackageBinaryRunning() {
		return true
	}
	if fileExists(legacyWindowsPackageLinkPath(paths)) {
		return true
	}
	for _, packageDir := range legacyWindowsPackageDirectories(paths) {
		if fileExists(filepath.Join(packageDir, publicBinaryName())) {
			return true
		}
	}
	return false
}

func isLegacyWindowsPackageManagedPath(path string) bool {
	clean := strings.ToLower(filepath.Clean(path))
	clean = strings.ReplaceAll(clean, "/", `\`)
	return strings.Contains(clean, `\microsoft\winget\links\`) || strings.Contains(clean, `\microsoft\winget\packages\`)
}
