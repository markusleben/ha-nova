package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const latestReleaseURL = "https://api.github.com/repos/markusleben/ha-nova/releases/latest"

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func bundleAssetName() string {
	ext := ".tar.gz"
	if runtimeGOOS := bundlePlatformOS(); runtimeGOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("ha-nova-installer-bundle-%s-%s%s", bundlePlatformOS(), bundlePlatformArch(), ext)
}

func rawBinaryAssetName() string {
	name := fmt.Sprintf("ha-nova-%s-%s", bundlePlatformOS(), bundlePlatformArch())
	if bundlePlatformOS() == "windows" {
		return name + ".exe"
	}
	return name
}

func fetchLatestRelease(paths runtimePaths, quiet bool, allowCache bool) (releaseInfo, error) {
	if allowCache {
		if cached, ok := loadCachedRelease(paths); ok {
			return cached, nil
		}
	}

	req, err := http.NewRequest("GET", latestReleaseURL, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ha-nova/"+localVersion(paths))

	resp, err := httpClient.Do(req)
	if err != nil {
		return releaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return releaseInfo{}, fmt.Errorf("GitHub latest release lookup failed: %s", strings.TrimSpace(string(body)))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return releaseInfo{}, err
	}
	version := strings.TrimPrefix(release.TagName, "v")
	if version == "" {
		return releaseInfo{}, fmt.Errorf("latest release tag missing")
	}
	if _, err := parseReleaseVersion(version); err != nil {
		return releaseInfo{}, fmt.Errorf("latest release tag invalid: %w", err)
	}
	info := releaseInfo{Version: version, HTMLURL: release.HTMLURL, AssetName: bundleAssetName()}
	cacheReleaseInfo(paths, info)
	if !quiet {
		printHumanInfo("Latest release: v%s", version)
	}
	return info, nil
}

func buildUpdateCheckResult(paths runtimePaths) updateCheckResult {
	current := localVersion(paths)
	_, cacheStatus := inspectCachedRelease(paths)
	state := loadStateOrDefault(paths)
	channels := inspectInstallChannels(paths, state)
	result := updateCheckResult{
		CurrentVersion: current,
		InstallSource:  channels.CurrentSource,
	}

	result.Source = "github_releases"

	release, err := fetchLatestRelease(paths, true, true)
	if err != nil {
		result.CacheStatus = cacheStatus
		result.Status = "check_failed"
		result.Message = fmt.Sprintf("could not check for updates (%s)", err)
		return result
	}

	result.CacheStatus = "fresh"
	result.LatestVersion = release.Version
	result.HTMLURL = release.HTMLURL
	if current == "dev" {
		result.Status = "up_to_date"
		result.Message = fmt.Sprintf("Up to date: v%s", current)
		return result
	}
	cmp, err := compareReleaseVersions(current, release.Version)
	if err != nil {
		result.Status = "check_failed"
		result.Message = fmt.Sprintf("could not compare versions (%s)", err)
		return result
	}
	if cmp >= 0 && !isStableTargetFromRC(current, release.Version) {
		result.Status = "up_to_date"
		result.Message = fmt.Sprintf("Up to date: v%s", current)
		return result
	}

	result.Status = "update_available"
	result.UpdateAvailable = true
	if isStableTargetFromRC(current, release.Version) {
		result.Message = fmt.Sprintf("Return to stable: v%s -> v%s | Run: ha-nova update", current, release.Version)
		return result
	}
	result.Message = fmt.Sprintf("Update available: v%s -> v%s | Run: ha-nova update", current, release.Version)
	return result
}

func updateCheckExitCode(result updateCheckResult) int {
	switch result.Status {
	case "check_failed":
		return 1
	default:
		return 0
	}
}

func humanNoticeFromUpdateCheckResult(result updateCheckResult, quiet bool) humanNotice {
	switch result.Status {
	case "check_failed":
		if quiet {
			return humanNotice{}
		}
		return humanNotice{
			level:   humanNoticeWarning,
			kind:    humanNoticeKindUpdateCheckFailed,
			message: result.Message,
		}
	case "up_to_date":
		if quiet {
			return humanNotice{}
		}
		return humanNotice{
			level:   humanNoticeInfo,
			kind:    humanNoticeKindUpToDate,
			message: result.Message,
		}
	case "update_available":
		return humanNotice{
			level:   humanNoticeWarning,
			kind:    humanNoticeKindUpdateAvailable,
			message: result.Message,
		}
	default:
		return humanNotice{}
	}
}

func checkForUpdate(paths runtimePaths, quiet bool) humanNotice {
	return humanNoticeFromUpdateCheckResult(buildUpdateCheckResult(paths), quiet)
}

func findBundleBinary(stageDir string) string {
	candidate := filepath.Join(stageDir, "ha-nova", publicBinaryName())
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return filepath.Join(stageDir, publicBinaryName())
}
