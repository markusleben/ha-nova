package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
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
	// cloudRemoteReleaseProof hashes the executable this process started
	// from, once, on the first question whether Link may run.
	cloudRemoteReleaseProof = func() bool { return false }
)

var cloudRemoteDevAppSlugPattern = regexp.MustCompile(
	cloudRemoteDevAppSlugExpression,
)
var cloudRemoteReleaseVersionPattern = regexp.MustCompile(
	`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc[1-9]\d*)?$`,
)

// configureCloudRemoteFeature runs before dispatch: it reads the release
// metadata and opens the executable this process started from, so a later
// update can't change what the deferred digest check reads.
func configureCloudRemoteFeature(paths runtimePaths) {
	cloudRemoteReleaseEnabled = false
	cloudRemoteReleasePlatforms = nil
	cloudRemoteReleaseProof = func() bool { return false }
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
	if _, listed := platforms[runtime.GOOS]; !listed {
		return
	}
	proof, matches := cloudRemoteReleaseRuntimeMatches(paths, metadata)
	if !matches {
		return
	}
	cloudRemoteReleaseEnabled = true
	cloudRemoteReleasePlatforms = platforms
	cloudRemoteReleaseProof = sync.OnceValue(proof)
}

// cloudRemoteReleaseVerified: an official release whose executable matches
// its signed digest (the first call checks it).
func cloudRemoteReleaseVerified() bool {
	return cloudRemoteReleaseEnabled && cloudRemoteReleaseProof()
}

// cloudRemoteReleaseRuntimeMatches checks the metadata and the files now and
// returns the digest check of the opened executable for later.
func cloudRemoteReleaseRuntimeMatches(
	paths runtimePaths,
	metadata versionJSON,
) (func() bool, bool) {
	baseVersion, validVersion := cloudRemoteReleaseBaseVersion(Version)
	if !validVersion || metadata.SkillVersion != baseVersion {
		return nil, false
	}

	bundle, err := loadBundleMetadata(paths)
	if err != nil ||
		bundle.BundleFormatVersion != bundleFormatVersion ||
		bundle.Version != Version ||
		bundle.OS != bundlePlatformOS() ||
		bundle.Arch != bundlePlatformArch() ||
		bundle.BinaryName != publicBinaryName() {
		return nil, false
	}

	executablePath, err := executablePathForInstallSource()
	if err != nil {
		return nil, false
	}
	if executable, err := os.Stat(executablePath); err != nil || !executable.Mode().IsRegular() {
		return nil, false // never open a pipe or device: it could block the start
	}
	file, err := os.Open(executablePath)
	if err != nil {
		return nil, false
	}
	executable, err := file.Stat()
	installed, installedErr := os.Stat(
		filepath.Join(paths.InstallRoot, publicBinaryName()),
	)
	if err != nil || installedErr != nil ||
		!executable.Mode().IsRegular() ||
		!installed.Mode().IsRegular() ||
		!os.SameFile(executable, installed) {
		file.Close()
		return nil, false
	}
	return func() bool {
		defer file.Close()
		return verifyCloudReleaseProvenance(bundle, metadata, file)
	}, true
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
	_, enabled := cloudRemoteReleasePlatforms[runtime.GOOS]
	return enabled && cloudRemoteReleaseVerified()
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
