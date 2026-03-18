package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCheckForUpdateReturnsStructuredUpdateAvailableNotice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalVersion := Version
	originalHTTPClient := httpClient
	defer func() {
		Version = originalVersion
		httpClient = originalHTTPClient
	}()

	Version = "0.1.0"
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"tag_name":"v0.2.0","html_url":"https://example.test/release"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	notice := checkForUpdate(paths, false)
	if notice.kind != humanNoticeKindUpdateAvailable {
		t.Fatalf("notice.kind = %q, want %q", notice.kind, humanNoticeKindUpdateAvailable)
	}
	if notice.level != humanNoticeWarning {
		t.Fatalf("notice.level = %q, want %q", notice.level, humanNoticeWarning)
	}
	if !strings.Contains(notice.message, "Run: ha-nova update") {
		t.Fatalf("expected update guidance in message, got %q", notice.message)
	}
}

func TestBuildUpdateCheckResultUsesFreshCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.1.0","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: "0.2.0", HTMLURL: "https://example.invalid/releases/v0.2.0"}, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	result := buildUpdateCheckResult(paths)
	if result.Status != "update_available" {
		t.Fatalf("result.Status = %q, want update_available", result.Status)
	}
	if result.CacheStatus != "fresh" {
		t.Fatalf("result.CacheStatus = %q, want fresh", result.CacheStatus)
	}
	if result.LatestVersion != "0.2.0" {
		t.Fatalf("result.LatestVersion = %q, want 0.2.0", result.LatestVersion)
	}
	if !result.UpdateAvailable {
		t.Fatal("expected update_available=true")
	}
}

func TestBuildUpdateCheckResultMarksFetchedStaleCacheAsFresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.1.0","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: "0.1.5", HTMLURL: "https://example.invalid/releases/v0.1.5"}, 0o644); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}
	staleTime := time.Now().Add(-(time.Duration(updateCacheTTLSeconds)*time.Second + time.Minute))
	if err := os.Chtimes(paths.UpdateCacheFile, staleTime, staleTime); err != nil {
		t.Fatalf("mark cache stale: %v", err)
	}

	originalHTTPClient := httpClient
	defer func() {
		httpClient = originalHTTPClient
	}()
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"tag_name":"v0.2.0","html_url":"https://example.test/release"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	result := buildUpdateCheckResult(paths)
	if result.Status != "update_available" {
		t.Fatalf("result.Status = %q, want update_available", result.Status)
	}
	if result.CacheStatus != "fresh" {
		t.Fatalf("result.CacheStatus = %q, want fresh after successful fetch", result.CacheStatus)
	}
	if result.LatestVersion != "0.2.0" {
		t.Fatalf("result.LatestVersion = %q, want 0.2.0", result.LatestVersion)
	}
}

func TestRunCheckUpdateJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.1.0","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: "0.2.0", HTMLURL: "https://example.invalid/releases/v0.2.0"}, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	output := captureStdout(t, func() {
		if exitCode := runCheckUpdate(paths, []string{"--json"}); exitCode != 0 {
			t.Fatalf("runCheckUpdate() exit = %d, want 0", exitCode)
		}
	})

	if !strings.Contains(output, `"status": "update_available"`) {
		t.Fatalf("expected JSON update_available output, got %s", output)
	}
	if !strings.Contains(output, `"cache_status": "fresh"`) {
		t.Fatalf("expected JSON fresh cache status, got %s", output)
	}
}

func TestCheckRelayVersionReturnsStructuredOutdatedNotice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.2.0","min_relay_version":"0.2.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	notice := checkRelayVersion(paths, []byte(`{"version":"0.1.0"}`))
	if notice.kind != humanNoticeKindRelayOutdated {
		t.Fatalf("notice.kind = %q, want %q", notice.kind, humanNoticeKindRelayOutdated)
	}
	if notice.level != humanNoticeWarning {
		t.Fatalf("notice.level = %q, want %q", notice.level, humanNoticeWarning)
	}
	if !strings.Contains(notice.message, "Relay outdated: v0.1.0 is below minimum v0.2.0") {
		t.Fatalf("unexpected relay warning message: %q", notice.message)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("ReadFrom() error: %v", err)
	}
	return buf.String()
}
