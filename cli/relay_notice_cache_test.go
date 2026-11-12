package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// quietAppCheckSetup: a legacy-token install whose NOVA App reports
// relayVersion on /health and answers nothing else, with a fresh release
// cache. It returns the count of App checks (/health requests).
func quietAppCheckSetup(t *testing.T, relayVersion string, healthDelay time.Duration) (runtimePaths, *atomic.Int32) {
	t.Helper()
	paths, cfg := doctorTestSetup(t)
	var checks atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		checks.Add(1)
		time.Sleep(healthDelay)
		_, _ = w.Write([]byte(`{"ok":true,"data":{"version":"` + relayVersion + `"}}`))
	}))
	t.Cleanup(relay.Close)
	cfg.RelayBaseURL = relay.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.20.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: "0.20.0", HTMLURL: "https://example.invalid/releases/v0.20.0"}, 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	return paths, &checks
}

func runQuietCheckUpdate(t *testing.T, paths runtimePaths) string {
	t.Helper()
	exit, output := captureCommandOutput(t, func() int { return runCheckUpdate(paths, []string{"--quiet"}) })
	if exit != 0 {
		t.Fatalf("quiet check-update exit %d:\n%s", exit, output)
	}
	return output
}

func TestQuietCheckUpdateSkipsASilentAppCheckWithinTheHour(t *testing.T) {
	paths, checks := quietAppCheckSetup(t, "9.9.9", 0)

	runQuietCheckUpdate(t, paths)
	if got := checks.Load(); got != 1 {
		t.Fatalf("first quiet check: %d App checks, want 1", got)
	}
	runQuietCheckUpdate(t, paths)
	if got := checks.Load(); got != 1 {
		t.Fatalf("a silent App check within the hour must be skipped: %d App checks, want 1", got)
	}

	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.21.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	runQuietCheckUpdate(t, paths)
	if got := checks.Load(); got != 2 {
		t.Fatalf("a new HA NOVA version must check the App again: %d App checks, want 2", got)
	}

	marker := filepath.Join(paths.CacheDir, relayNoticeSilentMarkerName)
	old := time.Now().Add(-relayNoticeSilentTTL - time.Minute)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatalf("age marker: %v", err)
	}
	runQuietCheckUpdate(t, paths)
	if got := checks.Load(); got != 3 {
		t.Fatalf("after the hour the App is checked again: %d App checks, want 3", got)
	}
}

func TestQuietCheckUpdateRechecksAnAppNoticeEverySession(t *testing.T) {
	paths, checks := quietAppCheckSetup(t, "0.1.0", 0)

	for run := 1; run <= 2; run++ {
		output := runQuietCheckUpdate(t, paths)
		if !strings.Contains(output, "The NOVA App in Home Assistant needs an update") {
			t.Fatalf("run %d: App below the floor not named:\n%s", run, output)
		}
		if got := checks.Load(); got != int32(run) {
			t.Fatalf("run %d: %d App checks, want %d", run, got, run)
		}
	}
}

func TestQuietCheckUpdateChecksTheAppWhileTheReleaseLookupRuns(t *testing.T) {
	const delay = 400 * time.Millisecond
	paths, checks := quietAppCheckSetup(t, "9.9.9", delay)
	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(paths.UpdateCacheFile, stale, stale); err != nil {
		t.Fatalf("age release cache: %v", err)
	}
	originalHTTPClient := httpClient
	t.Cleanup(func() { httpClient = originalHTTPClient })
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == latestReleaseURL {
			time.Sleep(delay)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v0.20.0","html_url":"https://example.invalid/releases/v0.20.0"}`)),
				Header:     make(http.Header),
			}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	})}

	start := time.Now()
	runQuietCheckUpdate(t, paths)
	if elapsed := time.Since(start); elapsed >= 2*delay-delay/8 {
		t.Fatalf("App check waited for the release lookup: %s for two %s lookups", elapsed, delay)
	}
	if got := checks.Load(); got != 1 {
		t.Fatalf("%d App checks, want 1", got)
	}
}
