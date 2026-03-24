package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type wingetPackageStatus struct {
	Installed        bool
	UpgradeAvailable bool
	InstalledVersion string
	AvailableVersion string
	InventoryScope   string
}

type installChannelSnapshot struct {
	CurrentSource  string
	BundlePresent  bool
	WingetPresent  bool
	Conflict       bool
	BundlePath     string
	WingetLinkPath string
}

var channelChecksUseWindowsPlatform = func() bool {
	return bundlePlatformOS() == "windows"
}

var queryWingetPackageStatusForChannels = queryWingetPackageStatus

const wingetNoApplicationsFoundExitCode uint32 = 0x8A150014

func inspectInstallChannels(paths runtimePaths, state installState) installChannelSnapshot {
	snapshot := installChannelSnapshot{
		CurrentSource: detectInstallSource(paths, state),
	}
	if !channelChecksUseWindowsPlatform() || snapshot.CurrentSource == installSourceDev {
		return snapshot
	}

	snapshot.BundlePath = windowsBundleInstallRoot(paths.Home)
	snapshot.WingetLinkPath = windowsWingetLinkPath(paths.Home)

	if snapshot.CurrentSource == installSourceBundle {
		snapshot.BundlePresent = true
		if root := resolveSourceRoot(paths); strings.TrimSpace(root) != "" {
			snapshot.BundlePath = root
		}
	} else {
		snapshot.BundlePresent = bundleInstallPresentOnDisk(snapshot.BundlePath)
	}

	if snapshot.CurrentSource == installSourceWinget {
		snapshot.WingetPresent = true
	} else {
		snapshot.WingetPresent = wingetInstallPresentOnDisk(paths.Home)
	}

	snapshot.Conflict = snapshot.BundlePresent && snapshot.WingetPresent
	return snapshot
}

func bundleInstallPresentOnDisk(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	binaryPath := filepath.Join(root, publicBinaryName())
	if _, err := os.Stat(binaryPath); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, "bundle.json")); err != nil {
		return false
	}
	return true
}

func wingetInstallPresentOnDisk(home string) bool {
	linkPath := windowsWingetLinkPath(home)
	_, err := os.Stat(linkPath)
	return err == nil
}

func windowsBundleInstallRoot(home string) string {
	return filepath.Join(windowsLocalAppDataDir(home), "Programs", "ha-nova")
}

func windowsWingetLinkPath(home string) string {
	return filepath.Join(windowsLocalAppDataDir(home), "Microsoft", "WinGet", "Links", publicBinaryName())
}

func windowsWingetPackageRoot(home string) string {
	return filepath.Join(windowsLocalAppDataDir(home), "Microsoft", "WinGet", "Packages")
}

func resolveWingetBundleRoot(home string) string {
	linkPath := windowsWingetLinkPath(home)
	if root := resolveWingetBundleRootFromLink(linkPath); root != "" {
		return root
	}
	candidates := listWingetBundleRoots(home)
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}

func resolveWingetBundleRootFromLink(linkPath string) string {
	target, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		return ""
	}
	return normalizeWingetBundleRootCandidate(target)
}

func normalizeWingetBundleRootCandidate(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	candidates := []string{
		target,
		filepath.Dir(target),
		filepath.Join(target, "ha-nova"),
		filepath.Join(filepath.Dir(target), "ha-nova"),
	}
	for _, candidate := range candidates {
		root := filepath.Clean(candidate)
		if !bundleInstallPresentOnDisk(root) {
			continue
		}
		return root
	}
	return ""
}

func listWingetBundleRoots(home string) []string {
	matches, err := filepath.Glob(filepath.Join(windowsWingetPackageRoot(home), wingetPackageID+"*"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	roots := []string{}
	for _, match := range matches {
		root := normalizeWingetBundleRootCandidate(filepath.Join(match, "ha-nova"))
		if root == "" {
			continue
		}
		duplicate := false
		for _, existing := range roots {
			if filepath.Clean(existing) == filepath.Clean(root) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		roots = append(roots, root)
	}
	return roots
}

func installChannelConflictMessage(snapshot installChannelSnapshot, command string) string {
	return fmt.Sprintf(
		"Windows install channel conflict: both bundle and winget installs are present. Current runtime: %s. Bundle path: %s. Winget link: %s. Keep only one Windows install channel before running '%s'.",
		snapshot.CurrentSource,
		snapshot.BundlePath,
		snapshot.WingetLinkPath,
		command,
	)
}

func queryWingetPackageStatus() (wingetPackageStatus, error) {
	if !channelChecksUseWindowsPlatform() {
		return wingetPackageStatus{}, nil
	}
	if _, err := execLookPathForLifecycle("winget"); err != nil {
		return wingetPackageStatus{}, err
	}

	installedOutput, err := runWingetListForStatus("--id", wingetPackageID, "--exact", "--source", "winget")
	if err != nil {
		return wingetPackageStatus{}, err
	}
	status := parseWingetPackageStatus(installedOutput)
	if status.Installed {
		status.InventoryScope = "published_source"
		upgradeOutput, err := runWingetListForStatus("--id", wingetPackageID, "--exact", "--source", "winget", "--upgrade-available")
		if err != nil {
			return wingetPackageStatus{}, err
		}
		upgradeStatus := parseWingetPackageStatus(upgradeOutput)
		if upgradeStatus.UpgradeAvailable {
			status.UpgradeAvailable = true
			if upgradeStatus.AvailableVersion != "" {
				status.AvailableVersion = upgradeStatus.AvailableVersion
			}
		}
		return status, nil
	}

	fallbackOutput, err := runWingetListForStatus("ha-nova")
	if err != nil {
		return wingetPackageStatus{}, err
	}
	fallbackStatus := parseWingetPackageStatus(fallbackOutput)
	if fallbackStatus.Installed {
		fallbackStatus.InventoryScope = "local_manifest"
		return fallbackStatus, nil
	}
	return status, nil
}

func runWingetListForStatus(args ...string) (string, error) {
	cmd := execCommandForLifecycle("winget", append([]string{"list"}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if isWingetNoApplicationsFoundError(err) {
			return trimmed, nil
		}
		return "", fmt.Errorf("%w: %s", err, trimmed)
	}
	return string(output), nil
}

func isWingetNoApplicationsFoundError(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ProcessState == nil {
		return false
	}
	return isWingetNoApplicationsFoundExitCode(exitErr.ProcessState.ExitCode())
}

func isWingetNoApplicationsFoundExitCode(code int) bool {
	return uint32(code) == wingetNoApplicationsFoundExitCode
}

var wingetVersionPattern = regexp.MustCompile(`^[vV]?\d+(?:\.\d+){0,4}(?:[-+][A-Za-z0-9._-]+)?$`)
var wingetSplitPattern = regexp.MustCompile(`\s{2,}`)

func parseWingetPackageStatus(output string) wingetPackageStatus {
	status := wingetPackageStatus{}
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.Contains(strings.ToLower(line), "ha nova") && !strings.Contains(line, wingetPackageID) {
			continue
		}
		fields := wingetSplitPattern.Split(line, -1)
		idIndex := -1
		for i, field := range fields {
			candidate := strings.TrimSpace(field)
			if candidate == wingetPackageID || strings.Contains(candidate, wingetPackageID) {
				idIndex = i
				break
			}
		}
		if idIndex == -1 {
			continue
		}
		status.Installed = true
		if idIndex+1 < len(fields) {
			status.InstalledVersion = strings.TrimSpace(fields[idIndex+1])
		}
		if idIndex+2 < len(fields) {
			available := strings.TrimSpace(fields[idIndex+2])
			if wingetVersionPattern.MatchString(available) {
				status.UpgradeAvailable = true
				status.AvailableVersion = available
			}
		}
	}
	return status
}
