package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	configSchemaVersion   = 1
	stateSchemaVersion    = 1
	bundleFormatVersion   = 1
	keyringServiceName    = "ha-nova.relay-auth-token"
	updateCacheTTLSeconds = 24 * 60 * 60
)

type runtimePaths struct {
	Home            string
	ConfigDir       string
	CacheDir        string
	InstallRoot     string
	BinDir          string
	PublicBinary    string
	ConfigFile      string
	StateFile       string
	VersionFile     string
	BundleFile      string
	UpdateCacheFile string
}

func detectPaths() (runtimePaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return runtimePaths{}, fmt.Errorf("cannot determine home directory: %w", err)
	}

	configDir := filepath.Join(home, ".config", "ha-nova")
	cacheDir := filepath.Join(home, ".cache", "ha-nova")
	installRoot := filepath.Join(home, ".local", "share", "ha-nova")
	binDir := filepath.Join(home, ".local", "bin")
	publicBinary := filepath.Join(binDir, publicCommandName())

	return runtimePaths{
		Home:            home,
		ConfigDir:       configDir,
		CacheDir:        cacheDir,
		InstallRoot:     installRoot,
		BinDir:          binDir,
		PublicBinary:    publicBinary,
		ConfigFile:      filepath.Join(configDir, "config.json"),
		StateFile:       filepath.Join(configDir, "state.json"),
		VersionFile:     filepath.Join(installRoot, "version.json"),
		BundleFile:      filepath.Join(installRoot, "bundle.json"),
		UpdateCacheFile: filepath.Join(cacheDir, "latest-release.json"),
	}, nil
}

func publicCommandName() string {
	if runtime.GOOS == "windows" {
		return "ha-nova.cmd"
	}
	return "ha-nova"
}

func publicBinaryName() string {
	if runtime.GOOS == "windows" {
		return "ha-nova.exe"
	}
	return "ha-nova"
}

func bundlePlatformOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return runtime.GOOS
	}
}

func bundlePlatformArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}
