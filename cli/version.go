package main

import (
	"encoding/json"
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
}

func compareSemver(a, b string) int {
	ap := parseSemver(a)
	bp := parseSemver(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(s string) [3]int {
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".", 3)
	var v [3]int
	for i := range parts {
		if i < 3 {
			v[i], _ = strconv.Atoi(parts[i])
		}
	}
	return v
}

func localVersion(paths runtimePaths) string {
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

func checkRelayVersion(paths runtimePaths, healthBody []byte) string {
	var health struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(healthBody, &health) != nil || health.Version == "" {
		return ""
	}

	v, err := readVersionJSON(paths.VersionFile)
	if err != nil || v.MinRelayVersion == "" {
		return ""
	}

	if compareSemver(health.Version, v.MinRelayVersion) < 0 {
		return fmt.Sprintf("⚠️ RELAY OUTDATED: v%s is below minimum v%s — Inform the user: update the NOVA Relay App in Home Assistant.", health.Version, v.MinRelayVersion)
	}
	return ""
}

func loadCachedRelease(paths runtimePaths) (releaseInfo, bool) {
	info, err := os.Stat(paths.UpdateCacheFile)
	if err != nil {
		return releaseInfo{}, false
	}

	if time.Since(info.ModTime()) > time.Duration(updateCacheTTLSeconds)*time.Second {
		return releaseInfo{}, false
	}

	data, err := os.ReadFile(paths.UpdateCacheFile)
	if err != nil {
		return releaseInfo{}, false
	}

	var cached releaseInfo
	if json.Unmarshal(data, &cached) != nil || cached.Version == "" {
		return releaseInfo{}, false
	}
	return cached, true
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
