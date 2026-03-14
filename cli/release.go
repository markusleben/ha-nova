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
	return fmt.Sprintf("ha-nova-%s-%s%s", bundlePlatformOS(), bundlePlatformArch(), ext)
}

func rawBinaryAssetName() string {
	name := fmt.Sprintf("ha-nova-%s-%s", bundlePlatformOS(), bundlePlatformArch())
	if bundlePlatformOS() == "windows" {
		return name + ".exe"
	}
	return name
}

func fetchLatestRelease(paths runtimePaths, quiet bool) (releaseInfo, error) {
	if cached, ok := loadCachedRelease(paths); ok {
		return cached, nil
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
	info := releaseInfo{Version: version, HTMLURL: release.HTMLURL, AssetName: bundleAssetName()}
	cacheReleaseInfo(paths, info)
	if !quiet {
		printInfo("Latest release: v%s", version)
	}
	return info, nil
}

func checkForUpdate(paths runtimePaths, quiet bool) string {
	release, err := fetchLatestRelease(paths, true)
	if err != nil {
		if !quiet {
			return fmt.Sprintf("[ha-nova] WARNING: could not check for updates (%s)", err)
		}
		return ""
	}

	current := localVersion(paths)
	if current == "dev" || compareSemver(current, release.Version) >= 0 {
		if quiet {
			return ""
		}
		return fmt.Sprintf("[ha-nova] Up to date: v%s", current)
	}

	return fmt.Sprintf("⚠️ UPDATE AVAILABLE: v%s -> v%s | Run: ha-nova update", current, release.Version)
}

func findBundleBinary(stageDir string) string {
	candidate := filepath.Join(stageDir, "ha-nova", publicBinaryName())
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return filepath.Join(stageDir, publicBinaryName())
}
