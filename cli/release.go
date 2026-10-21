package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const latestReleaseURL = "https://api.github.com/repos/markusleben/ha-nova/releases/latest"

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	// Body feeds the compact highlight digest (cli/release_digest.go); only
	// the normalized highlights are cached, never the full body.
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
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

func fetchLatestRelease(paths runtimePaths, quiet bool, allowCache bool) (releaseInfo, error) {
	return fetchLatestReleaseWithClient(paths, quiet, allowCache, httpClient)
}

func fetchLatestReleaseWithClient(paths runtimePaths, quiet bool, allowCache bool, client *http.Client) (releaseInfo, error) {
	cached, cacheStatus := inspectCachedRelease(paths)
	hasCache := cacheStatus != "miss" && cached.Version != ""
	// Within the short TTL floor, reuse the cache without touching the network.
	if allowCache && cacheStatus == "fresh" {
		return cached, nil
	}

	req, err := http.NewRequest("GET", latestReleaseURL, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ha-nova/"+localVersion(paths))
	// Revalidate cheaply: an unchanged release answers 304 (off the rate limit);
	// a new release answers 200 and is picked up immediately. A cache entry
	// without digest metadata (written by a pre-digest CLI) skips the
	// conditional header once: a 304 has no body, so it could never refill the
	// digest — one full 200 does.
	if allowCache && hasCache && cached.ETag != "" && cached.PublishedAt != "" {
		req.Header.Set("If-None-Match", cached.ETag)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Offline or transient failure: a known release beats a hard error.
		if allowCache && hasCache {
			return cached, nil
		}
		return releaseInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && hasCache {
		// Nothing changed; refresh the cache window so later checks stay cheap.
		cacheReleaseInfo(paths, cached)
		if !quiet {
			printHumanInfo("Latest release: v%s", cached.Version)
		}
		return cached, nil
	}

	if resp.StatusCode >= 400 {
		if allowCache && hasCache {
			return cached, nil
		}
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
	info := releaseInfo{
		Version:           version,
		HTMLURL:           release.HTMLURL,
		AssetName:         bundleAssetName(),
		ETag:              strings.TrimSpace(resp.Header.Get("ETag")),
		PublishedAt:       strings.TrimSpace(release.PublishedAt),
		ReleaseHighlights: deriveReleaseHighlights(release.Body),
	}
	cacheReleaseInfo(paths, info)
	if !quiet {
		printHumanInfo("Latest release: v%s", version)
	}
	return info, nil
}

func buildUpdateCheckResult(paths runtimePaths) updateCheckResult {
	return buildUpdateCheckResultWithClient(paths, httpClient)
}

func buildUpdateCheckResultWithClient(paths runtimePaths, client *http.Client) updateCheckResult {
	current := localVersion(paths)
	_, cacheStatus := inspectCachedRelease(paths)
	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		return updateCheckResult{
			CurrentVersion: current,
			Status:         "check_failed",
			Message:        err.Error(),
		}
	}
	channels := inspectInstallChannels(paths, state)
	result := updateCheckResult{
		CurrentVersion: current,
		InstallSource:  channels.CurrentSource,
	}

	result.Source = "github_releases"

	// A locally dev-synced build (BuildChannel=dev, injected by scripts/dev-sync.sh)
	// must never be nudged to update — and the check must not even require the
	// network. localVersion reads the real version.json (e.g. 0.6.0), so without
	// this short-circuit a dev tree below the latest release would be told to
	// `ha-nova update` and overwrite itself; worse, an offline dev box (no cache,
	// no network) would fall into the fetch-failure path below and surface a
	// misleading "check_failed" instead of a clean skip. Run it BEFORE the GitHub
	// lookup. Released binaries leave BuildChannel empty, so the real update path
	// is untouched.
	if current == "dev" || BuildChannel == "dev" {
		result.CacheStatus = cacheStatus
		result.Status = "up_to_date"
		if BuildChannel == "dev" {
			result.Message = "Local dev build (dev-sync) — update check skipped"
		} else {
			result.Message = fmt.Sprintf("Up to date: v%s", current)
		}
		result.human = result.Message
		return result
	}

	release, err := fetchLatestReleaseWithClient(paths, true, true, client)
	if err != nil {
		result.CacheStatus = cacheStatus
		result.Status = "check_failed"
		result.Message = fmt.Sprintf("could not check for updates (%s)", err)
		result.human = fmt.Sprintf("Can't check for HA NOVA updates right now. Check your internet, then try again.\nDetails: %s", err)
		return result
	}

	result.CacheStatus = "fresh"
	result.LatestVersion = release.Version
	result.HTMLURL = release.HTMLURL
	result.PublishedAt = release.PublishedAt
	result.ReleaseHighlights = release.ReleaseHighlights
	cmp, err := compareReleaseVersions(current, release.Version)
	if err != nil {
		result.Status = "check_failed"
		result.Message = fmt.Sprintf("could not compare versions (%s)", err)
		result.human = result.Message
		return result
	}
	updateGuidance := updateGuidanceForInstallSource(result.InstallSource)
	if cmp >= 0 {
		result.Status = "up_to_date"
		result.Message = fmt.Sprintf("Up to date: v%s", current)
		result.human = fmt.Sprintf("HA NOVA is up to date (%s).", current)
		// An RC ahead of the latest stable is current for its tester; the way
		// back is one info line (silent under --quiet), never an update notice.
		if isStableTargetFromRC(current, release.Version) {
			result.Message = fmt.Sprintf("Up to date: v%s (prerelease, newer than stable v%s) | To return to stable: %s", current, release.Version, updateGuidance)
			result.human = fmt.Sprintf("You have the test release %s. It is newer than the stable release %s.\n%s", current, release.Version, updateStep(result.InstallSource, "To go back to stable"))
		}
		return result
	}

	result.Status = "update_available"
	result.UpdateAvailable = true
	result.Message = fmt.Sprintf("Update available: v%s -> v%s | %s", current, release.Version, updateGuidance)
	return result
}

// updateReadyText is the one plain update notice. The terminal (check-update,
// doctor) and the agent channels (notices[], relay stderr) say the same words;
// the skills turn it into one chat offer.
func updateReadyText(current, latest, installSource string, highlights []releaseHighlight, htmlURL string) string {
	have := "you have " + current
	if isRCVersion(current) {
		have = "you have the test release " + current
	}
	lines := []string{fmt.Sprintf("HA NOVA %s is ready (%s).", latest, have)}
	if headline := releaseHeadline(highlights); headline != "" {
		lines = append(lines, "New: "+headline)
	}
	lines = append(lines, updateStep(installSource, "To update"))
	if url := strings.TrimSpace(htmlURL); url != "" {
		lines = append(lines, "What's new: "+url)
	}
	return strings.Join(lines, "\n")
}

// updateStep names the one command that updates this install; legacy Windows
// packages cannot update in place and get the reinstall path instead.
func updateStep(installSource, lead string) string {
	if normalizeInstallSource(installSource) == installSourceLegacyWindowsPackage {
		return lead + ": " + updateGuidanceForInstallSource(installSource)
	}
	return lead + ", run: ha-nova update"
}

// releaseHeadline: the one highlight a notice shows — the first sentence of
// the first new feature, else of the first fix, else of the action item.
func releaseHeadline(highlights []releaseHighlight) string {
	for _, kind := range []string{releaseHighlightKindFeature, releaseHighlightKindFix, releaseHighlightKindAction} {
		for _, highlight := range highlights {
			if highlight.Kind != kind {
				continue
			}
			text := strings.TrimSpace(highlight.Text)
			if end := strings.Index(text, ". "); end > 0 {
				text = text[:end+1]
			}
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func updateGuidanceForInstallSource(source string) string {
	if normalizeInstallSource(source) == installSourceLegacyWindowsPackage {
		return "Remove the old HA NOVA app in Installed Apps / App Installer, then reinstall with: irm https://raw.githubusercontent.com/markusleben/ha-nova/main/install.ps1 | iex"
	}
	return "Run: ha-nova update"
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
			message: result.humanText(),
		}
	case "up_to_date":
		if quiet {
			return humanNotice{}
		}
		return humanNotice{
			level:   humanNoticeInfo,
			kind:    humanNoticeKindUpToDate,
			message: result.humanText(),
		}
	case "update_available":
		// The plain text is for people; --json keeps the pinned Message.
		return humanNotice{
			level:   humanNoticeNote,
			kind:    humanNoticeKindUpdateAvailable,
			message: updateReadyText(result.CurrentVersion, result.LatestVersion, result.InstallSource, result.ReleaseHighlights, result.HTMLURL),
		}
	default:
		return humanNotice{}
	}
}

// humanText: the plain words for the terminal; Message (pinned in --json)
// is the fallback for a result built without them.
func (result updateCheckResult) humanText() string {
	if result.human != "" {
		return result.human
	}
	return result.Message
}

func checkForUpdate(paths runtimePaths, quiet bool) humanNotice {
	return humanNoticeFromUpdateCheckResult(buildUpdateCheckResult(paths), quiet)
}

// backToStableNotice: a plain `ha-nova update` on a test release installs the
// older stable release (docs/releasing.md). Say so, and how to get back.
func backToStableNotice(current, target string) []string {
	if !isRCVersion(current) || isRCVersion(target) {
		return nil
	}
	if cmp, err := compareReleaseVersions(current, target); err != nil || cmp <= 0 {
		return nil
	}
	return []string{
		fmt.Sprintf("You have the test release v%s. This goes back to the stable release v%s.", current, target),
		fmt.Sprintf("To get the test release again later, run: ha-nova update --version v%s", current),
	}
}
