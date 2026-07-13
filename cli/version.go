package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type versionJSON struct {
	SkillVersion    string `json:"skill_version"`
	MinRelayVersion string `json:"min_relay_version"`
}

type releaseInfo struct {
	Version   string `json:"version"`
	HTMLURL   string `json:"html_url,omitempty"`
	AssetName string `json:"asset_name,omitempty"`
	// ETag from the GitHub latest-release response. Sent back as If-None-Match
	// so an unchanged release returns a cheap 304 (off the rate limit) while a
	// new release returns 200 and is detected immediately.
	ETag string `json:"etag,omitempty"`
}

type updateCheckResult struct {
	Status          string `json:"status"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	Source          string `json:"source"`
	InstallSource   string `json:"install_source,omitempty"`
	HTMLURL         string `json:"html_url,omitempty"`
	CacheStatus     string `json:"cache_status"`
	Message         string `json:"message"`
}

type parsedReleaseVersion struct {
	Major int
	Minor int
	Patch int
	RC    int
}

var errUnsupportedVersionFormat = errors.New("unsupported version format")

func compareReleaseVersions(a, b string) (int, error) {
	ap, err := parseReleaseVersion(a)
	if err != nil {
		return 0, err
	}
	bp, err := parseReleaseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		var av, bv int
		switch i {
		case 0:
			av, bv = ap.Major, bp.Major
		case 1:
			av, bv = ap.Minor, bp.Minor
		default:
			av, bv = ap.Patch, bp.Patch
		}
		if av < bv {
			return -1, nil
		}
		if av > bv {
			return 1, nil
		}
	}
	switch {
	case ap.RC == 0 && bp.RC > 0:
		return 1, nil
	case ap.RC > 0 && bp.RC == 0:
		return -1, nil
	case ap.RC < bp.RC:
		return -1, nil
	case ap.RC > bp.RC:
		return 1, nil
	}
	return 0, nil
}

func parseReleaseVersion(s string) (parsedReleaseVersion, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(s, "v"))
	if raw == "" {
		return parsedReleaseVersion{}, fmt.Errorf("%w: empty version", errUnsupportedVersionFormat)
	}
	base := raw
	rc := 0
	if strings.Count(raw, "-") > 1 {
		return parsedReleaseVersion{}, fmt.Errorf("%w: %q", errUnsupportedVersionFormat, s)
	}
	if dash := strings.IndexByte(raw, '-'); dash >= 0 {
		base = raw[:dash]
		suffix := raw[dash+1:]
		if !strings.HasPrefix(suffix, "rc") {
			return parsedReleaseVersion{}, fmt.Errorf("%w: %q", errUnsupportedVersionFormat, s)
		}
		value := strings.TrimPrefix(suffix, "rc")
		if value == "" {
			return parsedReleaseVersion{}, fmt.Errorf("%w: %q", errUnsupportedVersionFormat, s)
		}
		parsedRC, err := strconv.Atoi(value)
		if err != nil || parsedRC < 1 {
			return parsedReleaseVersion{}, fmt.Errorf("%w: %q", errUnsupportedVersionFormat, s)
		}
		rc = parsedRC
	}
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return parsedReleaseVersion{}, fmt.Errorf("%w: %q", errUnsupportedVersionFormat, s)
	}
	values := [3]int{}
	for i, part := range parts {
		if part == "" {
			return parsedReleaseVersion{}, fmt.Errorf("%w: %q", errUnsupportedVersionFormat, s)
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return parsedReleaseVersion{}, fmt.Errorf("%w: %q", errUnsupportedVersionFormat, s)
		}
		values[i] = value
	}
	return parsedReleaseVersion{
		Major: values[0],
		Minor: values[1],
		Patch: values[2],
		RC:    rc,
	}, nil
}

func isRCVersion(s string) bool {
	parsed, err := parseReleaseVersion(s)
	return err == nil && parsed.RC > 0
}

func normalizeExplicitVersion(raw string) (string, error) {
	normalized := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if normalized == "" {
		return "", nil
	}
	if _, err := parseReleaseVersion(normalized); err != nil {
		return "", fmt.Errorf("unsupported version format %q; use X.Y.Z or X.Y.Z-rcN", raw)
	}
	return normalized, nil
}

func isStableTargetFromRC(currentVersion, targetVersion string) bool {
	return currentVersion != targetVersion && isRCVersion(currentVersion) && !isRCVersion(targetVersion)
}

func localVersion(paths runtimePaths) string {
	if BuildChannel == "dev" && Version != "" && Version != "dev" {
		return strings.TrimPrefix(Version, "v")
	}

	if meta, err := loadBundleMetadata(paths); err == nil && meta.Version != "" {
		return strings.TrimPrefix(meta.Version, "v")
	}

	if data, err := os.ReadFile(paths.VersionFile); err == nil {
		var v versionJSON
		if json.Unmarshal(data, &v) == nil && v.SkillVersion != "" {
			return strings.TrimPrefix(v.SkillVersion, "v")
		}
	}

	if Version != "" && Version != "dev" {
		return strings.TrimPrefix(Version, "v")
	}
	return "dev"
}

// versionDisplay renders the user-facing `ha-nova version` line. Locally
// dev-synced builds append a clearly-labeled DEV suffix so "which build is
// loaded?" is answerable in any client; released builds print the bare version.
func versionDisplay(paths runtimePaths) string {
	v := localVersion(paths)
	if BuildChannel != "dev" {
		return v
	}
	stamp := BuildStamp
	if stamp == "" {
		stamp = "unstamped"
	}
	return fmt.Sprintf("%s (local DEV build — dev-sync %s)", v, stamp)
}

func readVersionJSON(path string) (versionJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return versionJSON{}, err
	}
	var v versionJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return versionJSON{}, err
	}
	return v, nil
}

func readMinRelayVersion(dir string) string {
	v, err := readVersionJSON(filepath.Join(dir, "version.json"))
	if err != nil {
		return ""
	}
	return v.MinRelayVersion
}

func checkRelayVersion(paths runtimePaths, healthBody []byte) humanNotice {
	version := parseRelayHealthVersion(healthBody)
	if version == "" {
		return humanNotice{}
	}
	return checkRelayVersionValue(paths, version)
}

// parseRelayHealthVersion extracts the relay version from a /health body. A
// live GET /health answers with the relay envelope
// {"ok":true,"data":{"version":...}} — the version is NOT top-level. The
// top-level field stays as a fallback for bare health bodies.
func parseRelayHealthVersion(healthBody []byte) string {
	var health struct {
		Version string `json:"version"`
		Data    struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if json.Unmarshal(healthBody, &health) != nil {
		return ""
	}
	if health.Data.Version != "" {
		return health.Data.Version
	}
	return health.Version
}

// relayFloorNotice fetches the relay health once and returns the
// outdated-relay warning when the running relay is below min_relay_version.
// Best-effort by design: a missing config, missing token, or unreachable
// relay yields an empty notice — update/check-update must not fail or get
// noisy because Home Assistant is offline.
func relayFloorNotice(paths runtimePaths) humanNotice {
	cfg, err := loadConfig(paths)
	if err != nil || cfg.RelayBaseURL == "" {
		return humanNotice{}
	}
	token, err := readRelayAuthTokenForDoctor()
	if err != nil || token == "" {
		return humanNotice{}
	}
	body, err := fetchRelayHealth(cfg.RelayBaseURL, token)
	if err != nil {
		return humanNotice{}
	}
	return checkRelayVersion(paths, body)
}

// checkRelayVersionValue compares a bare relay version (from the /health body
// or the relay's version response header on /ws and /core) against
// min_relay_version.
func checkRelayVersionValue(paths runtimePaths, relayVersion string) humanNotice {
	if strings.TrimSpace(relayVersion) == "" {
		return humanNotice{}
	}

	v, err := readVersionJSON(paths.VersionFile)
	if err != nil || v.MinRelayVersion == "" {
		return humanNotice{}
	}

	cmp, err := compareReleaseVersions(relayVersion, v.MinRelayVersion)
	if err != nil {
		return humanNotice{
			level:   humanNoticeWarning,
			kind:    humanNoticeKindRelayOutdated,
			message: fmt.Sprintf("Relay version check unavailable: unsupported relay version format (relay v%s, minimum v%s). Inform the user: update the NOVA Relay in Home Assistant (App, or pull the new container image).", relayVersion, v.MinRelayVersion),
		}
	}
	if cmp < 0 {
		return humanNotice{
			level:   humanNoticeWarning,
			kind:    humanNoticeKindRelayOutdated,
			message: fmt.Sprintf("Relay outdated: v%s is below minimum v%s. Inform the user, then ask whether to install the relay update now — the updates skill (ha-nova:updates) handles the App update; a standalone container needs a manual image pull.", relayVersion, v.MinRelayVersion),
		}
	}
	return humanNotice{}
}

func inspectCachedRelease(paths runtimePaths) (releaseInfo, string) {
	info, err := os.Stat(paths.UpdateCacheFile)
	if err != nil {
		return releaseInfo{}, "miss"
	}

	data, err := os.ReadFile(paths.UpdateCacheFile)
	if err != nil {
		return releaseInfo{}, "miss"
	}

	var cached releaseInfo
	if json.Unmarshal(data, &cached) != nil || cached.Version == "" {
		return releaseInfo{}, "miss"
	}

	if time.Since(info.ModTime()) > time.Duration(updateCacheTTLSeconds)*time.Second {
		return cached, "stale"
	}
	return cached, "fresh"
}

func cacheReleaseInfo(paths runtimePaths, info releaseInfo) {
	if info.Version == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(paths.UpdateCacheFile), 0o755); err != nil {
		return
	}
	_ = writeJSONFile(paths.UpdateCacheFile, info, 0o644)
}
