package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	installSourceBundle   = "bundle"
	installSourceDev      = "dev"
	installSourceWinget   = "winget"
	wingetPackageID       = "markusleben.ha-nova"
	wingetPortableLinks   = `\microsoft\winget\links\`
	wingetPortablePackage = `\microsoft\winget\packages\`
)

var executablePathForInstallSource = os.Executable
var execCommandForLifecycle = exec.Command

func normalizeInstallSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case installSourceBundle:
		return installSourceBundle
	case installSourceDev:
		return installSourceDev
	case installSourceWinget:
		return installSourceWinget
	default:
		return ""
	}
}

func detectInstallSource(paths runtimePaths, state installState) string {
	if strings.TrimSpace(os.Getenv("HA_NOVA_DEV_ROOT")) != "" {
		return installSourceDev
	}

	exePath, _ := executablePathForInstallSource()
	if channelChecksUseWindowsPlatform() {
		if isWingetManagedPath(exePath) || isWingetManagedPath(resolveSourceRoot(paths)) {
			return installSourceWinget
		}
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
	if source := normalizeInstallSource(state.InstallSource); source != "" {
		switch source {
		case installSourceWinget:
			if channelChecksUseWindowsPlatform() && wingetInstallPresentOnDisk(paths.Home) {
				return source
			}
		case installSourceBundle:
			if bundleInstallPresentOnDisk(paths.InstallRoot) {
				return source
			}
		}
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
		if channelChecksUseWindowsPlatform() && isWingetManagedPath(exePath) {
			if wingetRoot := resolveWingetBundleRoot(paths.Home); wingetRoot != "" {
				return wingetRoot
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
		addCandidate(filepath.Dir(exePath))
	}
	if channelChecksUseWindowsPlatform() {
		addCandidate(resolveWingetBundleRoot(paths.Home))
	}
	addCandidate(paths.InstallRoot)
	if cwd, err := os.Getwd(); err == nil {
		addCandidate(cwd)
		addCandidate(filepath.Join(cwd, ".."))
	}
	return candidates
}

func isWingetManagedPath(path string) bool {
	if !channelChecksUseWindowsPlatform() {
		return false
	}
	clean := strings.ToLower(filepath.Clean(path))
	clean = strings.ReplaceAll(clean, "/", `\`)
	return strings.Contains(clean, wingetPortableLinks) || strings.Contains(clean, wingetPortablePackage)
}

func runWingetUpgrade() error {
	if _, err := execLookPathForLifecycle("winget"); err != nil {
		return err
	}
	cmd := execCommandForLifecycle("winget", "upgrade", "--id", wingetPackageID, "--exact", "--accept-source-agreements", "--accept-package-agreements")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runWingetUninstall(mode uninstallMode) error {
	if _, err := execLookPathForLifecycle("winget"); err != nil {
		return err
	}
	args := []string{"uninstall", "--id", wingetPackageID, "--exact", "--accept-source-agreements", "--purge"}
	if mode == uninstallModePurge {
		args = append(args, "--force")
	}
	cmd := execCommandForLifecycle("winget", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
