package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	updateNudgeOptOutEnv    = "HA_NOVA_NO_UPDATE_NUDGE"
	updateNudgeMarkerName   = "update-nudge-shown"
	updateRefreshMarkerName = "update-refresh-spawned"
	updateNudgeInterval     = 24 * time.Hour
)

// spawnDetachedUpdateRefresh fires the same cache-refresh contract the Claude
// session hook uses (`check-update --quiet --json`), detached so the hot path
// never waits on GitHub. Overridable for tests.
var spawnDetachedUpdateRefresh = func() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "check-update", "--quiet", "--json")
	// Stdout/Stderr stay nil on purpose: exec connects nil descriptors to
	// /dev/null, so the child's JSON can never interleave with the relay JSON
	// on the parent's stdout that skills parse.
	if err := cmd.Start(); err != nil {
		return
	}
	_ = cmd.Process.Release()
}

// maybeNudgeSkillUpdate surfaces a skill/CLI update notice during normal relay
// traffic so every client learns about updates through commands skills already
// run — not only Claude's session hook.
func maybeNudgeSkillUpdate(paths runtimePaths, throttled bool) {
	printHumanNotice(skillUpdateNudgeNotice(paths, throttled))
}

// skillUpdateNudgeNotice decides whether relay traffic should surface an
// update notice. Hot-path contract: the version compare is cache-only (never a
// network call); a non-fresh cache is refreshed by a detached background
// check. `throttled` keeps skill traffic (ws/core) at one notice per 24h;
// `relay health` passes false and stays unthrottled as the explicit
// diagnostic path, mirroring the relay-outdated warning split.
func skillUpdateNudgeNotice(paths runtimePaths, throttled bool) humanNotice {
	if strings.TrimSpace(os.Getenv(updateNudgeOptOutEnv)) == "1" {
		return humanNotice{}
	}
	current := localVersion(paths)
	// Same dev short-circuit as buildUpdateCheckResult: a dev-synced tree must
	// never be nudged to overwrite itself.
	if current == "dev" || BuildChannel == "dev" {
		return humanNotice{}
	}

	cached, cacheStatus := inspectCachedRelease(paths)
	if cacheStatus != "fresh" && passesNudgeThrottle(paths, updateRefreshMarkerName) {
		spawnDetachedUpdateRefresh()
	}
	if cacheStatus == "miss" {
		return humanNotice{}
	}

	cmp, err := compareReleaseVersions(current, cached.Version)
	if err != nil {
		return humanNotice{}
	}
	returnToStable := isStableTargetFromRC(current, cached.Version)
	if cmp >= 0 && !returnToStable {
		return humanNotice{}
	}
	if throttled && !passesNudgeThrottle(paths, updateNudgeMarkerName) {
		return humanNotice{}
	}

	lead := "HA NOVA update available"
	if returnToStable {
		lead = "HA NOVA return to stable"
	}
	return humanNotice{
		level:   humanNoticeWarning,
		kind:    humanNoticeKindUpdateAvailable,
		message: fmt.Sprintf("%s: v%s -> v%s. Inform the user: run 'ha-nova update', then start a new session.", lead, current, cached.Version),
	}
}

// passesNudgeThrottle reports whether the marker is older than the nudge
// interval and stamps it when it passes — the same marker-file pattern as
// shouldWarnRelayOutdated. Failures degrade to "allow" so a broken cache dir
// never suppresses notices.
func passesNudgeThrottle(paths runtimePaths, markerName string) bool {
	marker := filepath.Join(paths.CacheDir, markerName)
	if info, err := os.Stat(marker); err == nil && time.Since(info.ModTime()) < updateNudgeInterval {
		return false
	}
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		return true
	}
	if err := os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		return true
	}
	return true
}
