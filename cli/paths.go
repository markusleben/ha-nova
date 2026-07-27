package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// v3: named profiles gain immutable identities, route policy, and non-secret
	// Cloud lifecycle metadata. v1/v2 configs migrate on their first save; only
	// legacy local fields remain mirrored for older binaries.
	configSchemaVersion = 3
	stateSchemaVersion  = 1
	bundleFormatVersion = 1
	keyringServiceName  = "ha-nova.relay-auth-token"
	// Short cache floor: within this window check-update reuses the cached
	// result without touching the network. Beyond it, fetchLatestRelease
	// revalidates with a conditional request (If-None-Match), so a freshly
	// published release is detected within the hour instead of being hidden for
	// a full day. Keep this small — a long TTL means users stay blind to a new
	// release until it expires.
	updateCacheTTLSeconds      = 60 * 60
	windowsInstallRootEnv      = "HA_NOVA_INSTALL_ROOT"
	windowsInstallRootAllowEnv = "HA_NOVA_ALLOW_INSTALL_ROOT_OVERRIDE"
	configDirEnv               = "HA_NOVA_CONFIG_DIR"
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
	CensusFile          string
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
	censusDir := configDir
	if runtime.GOOS == "windows" {
		appData := windowsAppDataDir(home)
		localAppData := windowsLocalAppDataDir(home)
		configDir = filepath.Join(appData, "ha-nova")
		localDataDir = filepath.Join(localAppData, "ha-nova")
		cacheDir = filepath.Join(localDataDir, "cache")
		// Consent is device-local. APPDATA may roam between Windows hosts;
		// LOCALAPPDATA must not carry one machine's answer to another.
		censusDir = localDataDir
		installRoot = filepath.Join(localAppData, "Programs", "ha-nova")
		if override := strings.TrimSpace(os.Getenv(windowsInstallRootEnv)); override != "" && allowWindowsInstallRootOverride() {
			installRoot = filepath.Clean(override)
		} else if exePath, err := executablePathForInstallSource(); err == nil {
			exeRoot := filepath.Dir(exePath)
			if _, err := os.Stat(filepath.Join(exeRoot, "bundle.json")); err == nil {
				installRoot = exeRoot
			}
		}
		binDir = installRoot
		publicBinary = filepath.Join(installRoot, publicCommandName())
	}
	if override := strings.TrimSpace(os.Getenv(configDirEnv)); override != "" {
		configDir = filepath.Clean(override)
		if !filepath.IsAbs(configDir) {
			return runtimePaths{}, fmt.Errorf(
				"%s must be an absolute path",
				configDirEnv,
			)
		}
		if filepath.Dir(configDir) == configDir {
			return runtimePaths{}, fmt.Errorf(
				"%s must not be a filesystem root",
				configDirEnv,
			)
		}
		censusDir = configDir
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
		CensusFile:          filepath.Join(censusDir, "census.json"),
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

func allowWindowsInstallRootOverride() bool {
	if strings.TrimSpace(os.Getenv(windowsInstallRootAllowEnv)) == "1" {
		return true
	}
	if len(os.Args) < 2 {
		return false
	}
	switch os.Args[1] {
	case "internal-replace", "internal-uninstall":
		return true
	default:
		return false
	}
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
