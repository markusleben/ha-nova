package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// nudgeTestEnv prepares an isolated HOME with a release-build identity at the
// given local version, intercepts the detached refresh spawn, and returns the
// paths plus a counter of spawn attempts.
func nudgeTestEnv(t *testing.T, localVersionValue string) (runtimePaths, *int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(updateNudgeOptOutEnv, "")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	versionJSONBody := `{"skill_version":"` + localVersionValue + `","min_relay_version":"0.1.0"}`
	if err := os.WriteFile(paths.VersionFile, []byte(versionJSONBody), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	origChannel := BuildChannel
	BuildChannel = ""
	t.Cleanup(func() { BuildChannel = origChannel })

	spawnCount := 0
	origSpawn := spawnDetachedUpdateRefresh
	spawnDetachedUpdateRefresh = func() { spawnCount++ }
	t.Cleanup(func() { spawnDetachedUpdateRefresh = origSpawn })

	return paths, &spawnCount
}

func writeFreshReleaseCache(t *testing.T, paths runtimePaths, version string) {
	t.Helper()
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: version}, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
}

func TestSkillUpdateNudgeNoticeThrottlesTo24h(t *testing.T) {
	paths, spawnCount := nudgeTestEnv(t, "0.1.0")
	writeFreshReleaseCache(t, paths, "0.2.0")

	notice := skillUpdateNudgeNotice(paths, true)
	if notice.empty() {
		t.Fatal("first throttled call: expected an update notice")
	}
	if notice.kind != humanNoticeKindUpdateAvailable {
		t.Fatalf("kind = %q, want %q", notice.kind, humanNoticeKindUpdateAvailable)
	}
	if !strings.Contains(notice.message, "v0.1.0 -> v0.2.0") {
		t.Fatalf("message missing versions: %q", notice.message)
	}
	if !strings.Contains(notice.message, "ha-nova update") {
		t.Fatalf("message missing update guidance: %q", notice.message)
	}

	if got := skillUpdateNudgeNotice(paths, true); !got.empty() {
		t.Fatalf("second throttled call within 24h: expected empty, got %q", got.message)
	}

	// The explicit diagnostic path (relay health) stays unthrottled.
	if got := skillUpdateNudgeNotice(paths, false); got.empty() {
		t.Fatal("unthrottled call: expected an update notice despite marker")
	}

	// Fresh cache: no background refresh may be spawned.
	if *spawnCount != 0 {
		t.Fatalf("spawnCount = %d, want 0 for a fresh cache", *spawnCount)
	}
}

func TestSkillUpdateNudgeNoticeExpiredMarkerNudgesAgain(t *testing.T) {
	paths, _ := nudgeTestEnv(t, "0.1.0")
	writeFreshReleaseCache(t, paths, "0.2.0")

	if notice := skillUpdateNudgeNotice(paths, true); notice.empty() {
		t.Fatal("first call: expected an update notice")
	}
	marker := filepath.Join(paths.CacheDir, updateNudgeMarkerName)
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatalf("age marker: %v", err)
	}
	if notice := skillUpdateNudgeNotice(paths, true); notice.empty() {
		t.Fatal("call after marker expiry: expected an update notice")
	}
}

func TestSkillUpdateNudgeNoticeOptOutEnv(t *testing.T) {
	paths, spawnCount := nudgeTestEnv(t, "0.1.0")
	writeFreshReleaseCache(t, paths, "0.2.0")
	t.Setenv(updateNudgeOptOutEnv, "1")

	if notice := skillUpdateNudgeNotice(paths, true); !notice.empty() {
		t.Fatalf("opted out: expected empty notice, got %q", notice.message)
	}
	if *spawnCount != 0 {
		t.Fatalf("opted out: spawnCount = %d, want 0", *spawnCount)
	}
}

func TestSkillUpdateNudgeNoticeDevBuildSuppressed(t *testing.T) {
	paths, spawnCount := nudgeTestEnv(t, "0.1.0")
	writeFreshReleaseCache(t, paths, "0.2.0")
	BuildChannel = "dev"

	if notice := skillUpdateNudgeNotice(paths, true); !notice.empty() {
		t.Fatalf("dev build: expected empty notice, got %q", notice.message)
	}
	if *spawnCount != 0 {
		t.Fatalf("dev build: spawnCount = %d, want 0", *spawnCount)
	}
}

func TestSkillUpdateNudgeNoticeCacheMissSpawnsRefreshOnce(t *testing.T) {
	paths, spawnCount := nudgeTestEnv(t, "0.1.0")

	if notice := skillUpdateNudgeNotice(paths, true); !notice.empty() {
		t.Fatalf("cache miss: expected empty notice, got %q", notice.message)
	}
	if *spawnCount != 1 {
		t.Fatalf("cache miss: spawnCount = %d, want 1", *spawnCount)
	}

	// Second call within the refresh window must not spawn again.
	if notice := skillUpdateNudgeNotice(paths, true); !notice.empty() {
		t.Fatalf("cache miss repeat: expected empty notice, got %q", notice.message)
	}
	if *spawnCount != 1 {
		t.Fatalf("cache miss repeat: spawnCount = %d, want 1", *spawnCount)
	}
}

func TestSkillUpdateNudgeNoticeStaleCacheStillNudgesAndRefreshes(t *testing.T) {
	paths, spawnCount := nudgeTestEnv(t, "0.1.0")
	writeFreshReleaseCache(t, paths, "0.2.0")
	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(paths.UpdateCacheFile, stale, stale); err != nil {
		t.Fatalf("age cache: %v", err)
	}

	notice := skillUpdateNudgeNotice(paths, true)
	if notice.empty() {
		t.Fatal("stale cache: expected an update notice from stale data")
	}
	if *spawnCount != 1 {
		t.Fatalf("stale cache: spawnCount = %d, want 1", *spawnCount)
	}
}

func TestSkillUpdateNudgeNoticeUpToDateStaysSilent(t *testing.T) {
	paths, _ := nudgeTestEnv(t, "0.2.0")
	writeFreshReleaseCache(t, paths, "0.2.0")

	if notice := skillUpdateNudgeNotice(paths, false); !notice.empty() {
		t.Fatalf("up to date: expected empty notice, got %q", notice.message)
	}
	// No nudge marker may be stamped on silent runs, or the first real update
	// after a quiet day would be swallowed by a stale throttle.
	if _, err := os.Stat(filepath.Join(paths.CacheDir, updateNudgeMarkerName)); !os.IsNotExist(err) {
		t.Fatal("up to date: nudge marker must not be written")
	}
}

func TestSkillUpdateNudgeMessageMatchesInstallSourceGuidance(t *testing.T) {
	normal := skillUpdateNudgeMessage("HA NOVA update available", "0.1.0", "0.2.0", installSourceBundle)
	if !strings.Contains(normal, "Run: ha-nova update") {
		t.Fatalf("bundle install must advise ha-nova update: %q", normal)
	}
	// Legacy Windows package installs reject `ha-nova update` in runUpdate, so
	// the passive nudge must never advertise a known-failing command.
	legacy := skillUpdateNudgeMessage("HA NOVA update available", "0.1.0", "0.2.0", installSourceLegacyWindowsPackage)
	if strings.Contains(legacy, "Run: ha-nova update") {
		t.Fatalf("legacy Windows package installs must not be told to run ha-nova update: %q", legacy)
	}
	if !strings.Contains(legacy, "install.ps1") {
		t.Fatalf("legacy guidance must point at the supported reinstall path: %q", legacy)
	}
}

func TestSkillUpdateNudgeNoticeReturnToStableFromRC(t *testing.T) {
	paths, _ := nudgeTestEnv(t, "0.2.0-rc1")
	writeFreshReleaseCache(t, paths, "0.2.0")

	notice := skillUpdateNudgeNotice(paths, false)
	if notice.empty() {
		t.Fatal("rc build with stable release: expected a return-to-stable notice")
	}
	if !strings.Contains(notice.message, "return to stable") {
		t.Fatalf("message missing return-to-stable lead: %q", notice.message)
	}
	if !strings.Contains(notice.message, "v0.2.0-rc1 -> v0.2.0") {
		t.Fatalf("message missing versions: %q", notice.message)
	}
}
