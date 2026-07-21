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
	staleCache(t, paths, releaseInfo{
		Version:     "0.4.2",
		ETag:        `"etag-v042"`,
		PublishedAt: "2026-07-01T10:00:00Z",
		ReleaseHighlights: []releaseHighlight{
			{Kind: releaseHighlightKindFeature, Text: "New energy skill"},
		},
	})

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
	cached, status := inspectCachedRelease(paths)
	if status != "fresh" {
		t.Fatalf("cache status after 304 = %q, want fresh (TTL window refreshed)", status)
	}
	// A 304 has no body: the existing digest metadata must survive unchanged.
	if cached.PublishedAt != "2026-07-01T10:00:00Z" {
		t.Fatalf("published_at after 304 = %q, want preserved", cached.PublishedAt)
	}
	if len(cached.ReleaseHighlights) != 1 || cached.ReleaseHighlights[0].Text != "New energy skill" {
		t.Fatalf("release highlights after 304 = %+v, want preserved", cached.ReleaseHighlights)
	}
}

func TestFetchLatestReleaseStoresDigestNotBodyOn200(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalHTTPClient := httpClient
	defer func() { httpClient = originalHTTPClient }()
	body := "## New Features\n\n- **Energy skill** for solar and grid analysis\n\n## Bug Fixes\n\n- Fix relay reconnect loop\n"
	release := map[string]any{
		"tag_name":     "v0.5.0",
		"html_url":     "https://example.test/v0.5.0",
		"published_at": "2026-07-21T10:00:00Z",
		"body":         body,
	}
	payload, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("marshal release: %v", err)
	}
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(payload))),
				Header:     make(http.Header),
			}, nil
		}),
	}

	info, err := fetchLatestRelease(paths, true, true)
	if err != nil {
		t.Fatalf("fetchLatestRelease() error: %v", err)
	}
	if info.PublishedAt != "2026-07-21T10:00:00Z" {
		t.Fatalf("published_at = %q, want 2026-07-21T10:00:00Z", info.PublishedAt)
	}
	cached := readCachedRelease(t, paths)
	want := []releaseHighlight{
		{Kind: releaseHighlightKindFeature, Text: "Energy skill for solar and grid analysis"},
		{Kind: releaseHighlightKindFix, Text: "Fix relay reconnect loop"},
	}
	if len(cached.ReleaseHighlights) != len(want) {
		t.Fatalf("cached highlights = %+v, want %+v", cached.ReleaseHighlights, want)
	}
	for i, h := range want {
		if cached.ReleaseHighlights[i] != h {
			t.Fatalf("cached highlight[%d] = %+v, want %+v", i, cached.ReleaseHighlights[i], h)
		}
	}
	// Only the normalized digest may be cached, never the raw release body.
	raw, err := os.ReadFile(paths.UpdateCacheFile)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	for _, forbidden := range []string{"## New Features", "**Energy skill**", `"body"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("cache must not contain raw body fragment %q:\n%s", forbidden, raw)
		}
	}
}

func TestFetchLatestReleaseMissingDigestMetadataSkipsIfNoneMatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	// Pre-digest cache entry: version + ETag, but no digest metadata. Sending
	// If-None-Match here would answer 304 forever and starve the digest.
	staleCache(t, paths, releaseInfo{Version: "0.4.2", ETag: `"etag-v042"`})

	originalHTTPClient := httpClient
	defer func() { httpClient = originalHTTPClient }()
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("If-None-Match"); got != "" {
				t.Errorf("If-None-Match = %q, want unset while the cache lacks digest metadata", got)
			}
			header := make(http.Header)
			header.Set("ETag", `"etag-v042"`)
			body := `{"tag_name":"v0.4.2","html_url":"https://example.test/v0.4.2","published_at":"2026-07-01T10:00:00Z","body":"## Bug Fixes\n\n- Fix relay reconnect loop\n"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     header,
			}, nil
		}),
	}

	if _, err := fetchLatestRelease(paths, true, true); err != nil {
		t.Fatalf("fetchLatestRelease() error: %v", err)
	}
	cached := readCachedRelease(t, paths)
	if cached.PublishedAt == "" {
		t.Fatal("expected the 200 to refill published_at digest metadata")
	}
	if len(cached.ReleaseHighlights) != 1 || cached.ReleaseHighlights[0].Text != "Fix relay reconnect loop" {
		t.Fatalf("expected the 200 to refill highlights, got %+v", cached.ReleaseHighlights)
	}

	// With digest metadata present, the next revalidation is conditional again.
	staleCache(t, paths, readCachedRelease(t, paths))
	sawConditional := false
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
	if _, err := fetchLatestRelease(paths, true, true); err != nil {
		t.Fatalf("fetchLatestRelease() error: %v", err)
	}
	if !sawConditional {
		t.Fatal("expected conditional revalidation once digest metadata exists")
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
