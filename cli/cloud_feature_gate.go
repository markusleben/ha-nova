package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const HAOfficialNOVAAppSlug = "2368fcfa_ha_nova_relay"
const cloudRemoteDevAppSlugExpression = `^local_[a-z0-9][a-z0-9_-]{0,57}$`

type cloudRemoteBuildIdentity struct {
	Development bool
	Disabled    bool
	Official    bool
	AppSlug     string
}

// The production implementation is selected at compile time. Tests replace
// this function seam directly; linker flags cannot turn a release build into a
// development build.
var cloudRemoteBuildIdentityForRuntime = compiledCloudRemoteBuildIdentity
var cloudRemoteSecureStorageBoundaryAvailable = platformCloudRemoteSecureStorageBoundaryAvailable

var (
	cloudRemoteReleaseEnabled   bool
	cloudRemoteReleasePlatforms map[string]struct{}
)

var cloudRemoteDevAppSlugPattern = regexp.MustCompile(
	cloudRemoteDevAppSlugExpression,
)
var cloudRemoteReleaseVersionPattern = regexp.MustCompile(
	`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc[1-9]\d*)?$`,
)

func configureCloudRemoteFeature(paths runtimePaths) {
	cloudRemoteReleaseEnabled = false
	cloudRemoteReleasePlatforms = nil
	identity := cloudRemoteBuildIdentityForRuntime()
	if identity.Development || identity.Disabled || !identity.Official {
		return
	}
	metadata, err := readVersionJSON(paths.VersionFile)
	if err != nil || !metadata.CloudRemoteEnabled {
		return
	}
	platforms := make(map[string]struct{}, len(metadata.CloudRemotePlatforms))
	for _, platform := range metadata.CloudRemotePlatforms {
		if platform != "darwin" && platform != "linux" && platform != "windows" {
			return
		}
		if _, duplicate := platforms[platform]; duplicate {
			return
		}
		platforms[platform] = struct{}{}
	}
	if len(platforms) == 0 {
		return
	}
	if !cloudRemoteReleaseRuntimeMatches(paths, metadata) {
		return
	}
	cloudRemoteReleaseEnabled = true
	cloudRemoteReleasePlatforms = platforms
}

func cloudRemoteReleaseRuntimeMatches(
	paths runtimePaths,
	metadata versionJSON,
) bool {
	baseVersion, validVersion := cloudRemoteReleaseBaseVersion(Version)
	if !validVersion || metadata.SkillVersion != baseVersion {
		return false
	}

	bundle, err := loadBundleMetadata(paths)
	if err != nil ||
		bundle.BundleFormatVersion != bundleFormatVersion ||
		bundle.Version != Version ||
		bundle.OS != bundlePlatformOS() ||
		bundle.Arch != bundlePlatformArch() ||
		bundle.BinaryName != publicBinaryName() {
		return false
	}

	executablePath, err := executablePathForInstallSource()
	if err != nil {
		return false
	}
	executable, err := os.Stat(executablePath)
	if err != nil || !executable.Mode().IsRegular() {
		return false
	}
	installed, err := os.Stat(
		filepath.Join(paths.InstallRoot, publicBinaryName()),
	)
	return err == nil &&
		installed.Mode().IsRegular() &&
		os.SameFile(executable, installed) &&
		verifyCloudReleaseProvenance(bundle, metadata, executablePath)
}

func cloudRemoteReleaseBaseVersion(version string) (string, bool) {
	if !cloudRemoteReleaseVersionPattern.MatchString(version) {
		return "", false
	}
	base, _, _ := strings.Cut(version, "-")
	return base, true
}

func cloudRemoteFeatureAvailable() bool {
	if !cloudRemotePlatformSupported(runtime.GOOS) {
		return false
	}
	if !cloudRemoteSecureStorageBoundaryAvailable() {
		return false
	}
	identity := cloudRemoteBuildIdentityForRuntime()
	if identity.Disabled {
		return false
	}
	if identity.Development {
		return validateCloudRemoteDevelopmentIdentity(identity) == nil
	}
	if !identity.Official {
		return false
	}
	if !cloudRemoteReleaseEnabled {
		return false
	}
	_, enabled := cloudRemoteReleasePlatforms[runtime.GOOS]
	return enabled
}

func cloudRemotePlatformSupported(platform string) bool {
	switch platform {
	case "darwin", "linux", "windows":
		return true
	default:
		return false
	}
}

func requireCloudRemoteFeature() error {
	if cloudRemoteFeatureAvailable() {
		return nil
	}
	return cloudAdapterUnavailableProblem()
}

func selectedCloudNOVAAppSlug() (string, error) {
	identity := cloudRemoteBuildIdentityForRuntime()
	if !identity.Development {
		return HAOfficialNOVAAppSlug, nil
	}
	if err := validateCloudRemoteDevelopmentIdentity(identity); err != nil {
		return "", err
	}
	return identity.AppSlug, nil
}

func validateCloudRemoteDevelopmentIdentity(
	identity cloudRemoteBuildIdentity,
) error {
	appSlug := identity.AppSlug
	if appSlug == "" ||
		appSlug != strings.TrimSpace(appSlug) ||
		appSlug == HAOfficialNOVAAppSlug ||
		!cloudRemoteDevAppSlugPattern.MatchString(appSlug) {
		return errors.New("invalid developer Cloud App slug")
	}
	return nil
}

func requireCloudRemoteFeatureForSetup() error {
	if err := requireCloudRemoteFeature(); err != nil {
		return fmt.Errorf(
			"Cloud Remote Beta is disabled for this build or platform: %w",
			err,
		)
	}
	return nil
}
