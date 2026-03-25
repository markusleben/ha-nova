package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	configSchemaVersion   = 1
	stateSchemaVersion    = 1
	bundleFormatVersion   = 1
	keyringServiceName    = "ha-nova.relay-auth-token"
	updateCacheTTLSeconds = 24 * 60 * 60
	windowsInstallRootEnv  = "HA_NOVA_INSTALL_ROOT"
)

type runtimePaths struct {
	Home                string
	ConfigDir           string
	CacheDir            string
	LocalDataDir        string
	InstallRoot         string
	BinDir              string
	PublicBinary        string
	ConfigFile          string
	StateFile           string
	VersionFile         string
	BundleFile          string
	UpdateCacheFile     string
	UninstallStatusFile string
}

func detectPaths() (runtimePaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return runtimePaths{}, fmt.Errorf("cannot determine home directory: %w", err)
	}

	configDir := filepath.Join(home, ".config", "ha-nova")
	cacheDir := filepath.Join(home, ".cache", "ha-nova")
	localDataDir := cacheDir
	installRoot := filepath.Join(home, ".local", "share", "ha-nova")
	binDir := filepath.Join(home, ".local", "bin")
	publicBinary := filepath.Join(binDir, publicCommandName())
	if runtime.GOOS == "windows" {
		appData := windowsAppDataDir(home)
		localAppData := windowsLocalAppDataDir(home)
		configDir = filepath.Join(appData, "ha-nova")
		localDataDir = filepath.Join(localAppData, "ha-nova")
		cacheDir = filepath.Join(localDataDir, "cache")
		installRoot = filepath.Join(localAppData, "Programs", "ha-nova")
		if override := strings.TrimSpace(os.Getenv(windowsInstallRootEnv)); override != "" {
			installRoot = filepath.Clean(override)
		} else if exePath, err := executablePathForInstallSource(); err == nil {
			exeRoot := filepath.Dir(exePath)
			if _, err := os.Stat(filepath.Join(exeRoot, "bundle.json")); err == nil {
				installRoot = exeRoot
			} else if isWingetManagedPath(exePath) {
				if wingetRoot := resolveWingetBundleRoot(home); wingetRoot != "" {
					installRoot = wingetRoot
				}
			}
		}
		binDir = installRoot
		publicBinary = filepath.Join(installRoot, publicCommandName())
	}

	paths := runtimePaths{
		Home:                home,
		ConfigDir:           configDir,
		CacheDir:            cacheDir,
		LocalDataDir:        localDataDir,
		InstallRoot:         installRoot,
		BinDir:              binDir,
		PublicBinary:        publicBinary,
		ConfigFile:          filepath.Join(configDir, "config.json"),
		StateFile:           filepath.Join(configDir, "state.json"),
		VersionFile:         filepath.Join(installRoot, "version.json"),
		BundleFile:          filepath.Join(installRoot, "bundle.json"),
		UpdateCacheFile:     filepath.Join(cacheDir, "latest-release.json"),
		UninstallStatusFile: filepath.Join(localDataDir, "uninstall-status.json"),
	}
	if runtime.GOOS == "windows" {
		migrateLegacyWindowsDirs(paths)
	}
	return paths, nil
}

func publicCommandName() string {
	return publicBinaryName()
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

func windowsAppDataDir(home string) string {
	if value := strings.TrimSpace(os.Getenv("APPDATA")); value != "" {
		return value
	}
	return filepath.Join(home, "AppData", "Roaming")
}

func windowsLocalAppDataDir(home string) string {
	if value := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); value != "" {
		return value
	}
	return filepath.Join(home, "AppData", "Local")
}
