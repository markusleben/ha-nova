package main

import (
	"os"
	"path/filepath"
	"strings"
)

type installChannelSnapshot struct {
	CurrentSource string
	BundlePresent bool
	Conflict      bool
	BundlePath    string
}

var channelChecksUseWindowsPlatform = func() bool {
	return bundlePlatformOS() == "windows"
}

func inspectInstallChannels(paths runtimePaths, state installState) installChannelSnapshot {
	snapshot := installChannelSnapshot{
		CurrentSource: detectInstallSource(paths, state),
	}
	if !channelChecksUseWindowsPlatform() || snapshot.CurrentSource == installSourceDev {
		return snapshot
	}

	snapshot.BundlePath = resolvedBundleInstallRoot(paths)
	snapshot.BundlePresent = bundleInstallPresentOnDisk(snapshot.BundlePath)
	return snapshot
}

func resolvedBundleInstallRoot(paths runtimePaths) string {
	root := windowsBundleInstallRoot(paths.Home)
	if candidate := strings.TrimSpace(resolveSourceRoot(paths)); candidate != "" && bundleInstallPresentOnDisk(candidate) {
		return candidate
	}
	return root
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

func windowsBundleInstallRoot(home string) string {
	return filepath.Join(windowsLocalAppDataDir(home), "Programs", "ha-nova")
}
