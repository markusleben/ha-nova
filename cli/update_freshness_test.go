package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// staleCache writes a cached release and ages it past the TTL floor so the next
// fetch revalidates over the network instead of returning the cache directly.
func staleCache(t *testing.T, paths runtimePaths, info releaseInfo) {
	t.Helper()
	cacheReleaseInfo(paths, info)
	old := time.Now().Add(-2 * time.Duration(updateCacheTTLSeconds) * time.Second)
	if err := os.Chtimes(paths.UpdateCacheFile, old, old); err != nil {
		t.Fatalf("age cache: %v", err)
	}
	if _, status := inspectCachedRelease(paths); status != "stale" {
		t.Fatalf("cache status = %q, want stale", status)
	}
}

func readCachedRelease(t *testing.T, paths runtimePaths) releaseInfo {
	t.Helper()
	data, err := os.ReadFile(paths.UpdateCacheFile)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var info releaseInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	return info
}

func TestFetchLatestReleaseRevalidatesWithETagAnd304(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	staleCache(t, paths, releaseInfo{Version: "0.4.2", ETag: `"etag-v042"`})

	originalHTTPClient := httpClient
	defer func() { httpClient = originalHTTPClient }()
	var sawConditional bool
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("If-None-Match") == `"etag-v042"` {
				sawConditional = true
			}
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	info, err := fetchLatestRelease(paths, true, true)
	if err != nil {
		t.Fatalf("fetchLatestRelease() error: %v", err)
	}
	if !sawConditional {
		t.Fatalf("expected a conditional request carrying the cached ETag")
	}
	if info.Version != "0.4.2" {
		t.Fatalf("version = %q, want 0.4.2", info.Version)
	}
	if _, status := inspectCachedRelease(paths); status != "fresh" {
		t.Fatalf("cache status after 304 = %q, want fresh (TTL window refreshed)", status)
	}
}

func TestFetchLatestReleaseStoresNewVersionAndETagOn200(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	staleCache(t, paths, releaseInfo{Version: "0.4.2", ETag: `"old"`})

	originalHTTPClient := httpClient
	defer func() { httpClient = originalHTTPClient }()
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("ETag", `"etag-v050"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v0.5.0","html_url":"https://example.test/v0.5.0"}`)),
				Header:     header,
			}, nil
		}),
	}

	info, err := fetchLatestRelease(paths, true, true)
	if err != nil {
		t.Fatalf("fetchLatestRelease() error: %v", err)
	}
	if info.Version != "0.5.0" {
		t.Fatalf("version = %q, want 0.5.0", info.Version)
	}
	cached := readCachedRelease(t, paths)
	if cached.Version != "0.5.0" {
		t.Fatalf("cached version = %q, want 0.5.0", cached.Version)
	}
	if cached.ETag != `"etag-v050"` {
		t.Fatalf("cached etag = %q, want %q", cached.ETag, `"etag-v050"`)
	}
}

func TestFetchLatestReleaseFreshCacheSkipsNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	cacheReleaseInfo(paths, releaseInfo{Version: "0.4.2"}) // mtime now -> fresh

	originalHTTPClient := httpClient
	defer func() { httpClient = originalHTTPClient }()
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Errorf("unexpected network call while cache is fresh: %s", req.URL)
			return nil, io.EOF
		}),
	}

	info, err := fetchLatestRelease(paths, true, true)
	if err != nil {
		t.Fatalf("fetchLatestRelease() error: %v", err)
	}
	if info.Version != "0.4.2" {
		t.Fatalf("version = %q, want 0.4.2", info.Version)
	}
}

func TestFetchLatestReleaseOfflineFallsBackToCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	staleCache(t, paths, releaseInfo{Version: "0.4.2", ETag: `"e"`})

	originalHTTPClient := httpClient
	defer func() { httpClient = originalHTTPClient }()
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, io.ErrUnexpectedEOF // simulate offline / transient failure
		}),
	}

	info, err := fetchLatestRelease(paths, true, true)
	if err != nil {
		t.Fatalf("fetchLatestRelease() should fall back to cache offline, got error: %v", err)
	}
	if info.Version != "0.4.2" {
		t.Fatalf("version = %q, want cached 0.4.2", info.Version)
	}
}

// Guard against silently regressing to a long TTL, which is what hid a freshly
// published release from check-update for up to a day.
func TestUpdateCacheTTLStaysShort(t *testing.T) {
	if updateCacheTTLSeconds > 60*60 {
		t.Fatalf("updateCacheTTLSeconds = %d, want <= 3600 (short floor + conditional revalidation)", updateCacheTTLSeconds)
	}
}
