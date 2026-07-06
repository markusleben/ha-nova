package main

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func disableAssetRetryDelay(t *testing.T) {
	t.Helper()
	original := assetRetryDelay
	assetRetryDelay = 0
	t.Cleanup(func() { assetRetryDelay = original })
}

func TestAssetHTTPClientHasNoTotalTimeout(t *testing.T) {
	// The relay httpClient enforces a 15s TOTAL response timeout, which aborts
	// any bundle download slower than 15 seconds. The asset client must bound
	// stalls per phase (dial/TLS/headers) instead of capping the whole body.
	if assetHTTPClient.Timeout != 0 {
		t.Fatalf("assetHTTPClient.Timeout = %v, want 0 (no total timeout)", assetHTTPClient.Timeout)
	}
}

func TestDownloadAssetFileRetriesTransientServerError(t *testing.T) {
	disableAssetRetryDelay(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "flaky", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, "payload")
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "asset")
	if err := downloadAssetFile(server.URL, dest); err != nil {
		t.Fatalf("downloadAssetFile() error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("server calls = %d, want 2", got)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("dest content = %q, want payload", data)
	}
}

func TestDownloadAssetFileFailsFastOnNotFound(t *testing.T) {
	disableAssetRetryDelay(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	err := downloadAssetFile(server.URL, filepath.Join(t.TempDir(), "asset"))
	if err == nil {
		t.Fatal("expected 404 to fail")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("server calls = %d, want 1 (no retries on 404)", got)
	}
	if !strings.Contains(err.Error(), server.URL) {
		t.Fatalf("error should name the asset URL, got %q", err)
	}
}

func TestDownloadAssetFileRetriesEmptyBody(t *testing.T) {
	disableAssetRetryDelay(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// 200 with an empty body: a broken proxy/captive portal artifact.
			return
		}
		fmt.Fprint(w, "payload")
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "asset")
	if err := downloadAssetFile(server.URL, dest); err != nil {
		t.Fatalf("downloadAssetFile() error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("server calls = %d, want 2", got)
	}
}

func TestDownloadAssetFileRetriesStalledBodyRead(t *testing.T) {
	disableAssetRetryDelay(t)
	originalIdleTimeout := assetBodyIdleTimeout
	assetBodyIdleTimeout = 20 * time.Millisecond
	t.Cleanup(func() { assetBodyIdleTimeout = originalIdleTimeout })

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "flush unavailable", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			time.Sleep(200 * time.Millisecond)
			return
		}
		fmt.Fprint(w, "payload")
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "asset")
	if err := downloadAssetFile(server.URL, dest); err != nil {
		t.Fatalf("downloadAssetFile() error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("server calls = %d, want 2", got)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("dest content = %q, want payload", data)
	}
}

func TestDownloadAssetFileSendsInstallerHeaders(t *testing.T) {
	disableAssetRetryDelay(t)
	var userAgent, accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		accept = r.Header.Get("Accept")
		fmt.Fprint(w, "payload")
	}))
	defer server.Close()

	if err := downloadAssetFile(server.URL, filepath.Join(t.TempDir(), "asset")); err != nil {
		t.Fatalf("downloadAssetFile() error: %v", err)
	}
	if userAgent != "ha-nova-installer" {
		t.Fatalf("User-Agent = %q, want ha-nova-installer", userAgent)
	}
	if accept != "application/octet-stream" {
		t.Fatalf("Accept = %q, want application/octet-stream", accept)
	}
}

func TestDownloadAssetFileVerifiedRedownloadsOnChecksumMismatch(t *testing.T) {
	disableAssetRetryDelay(t)
	payload := []byte("bundle-bytes")
	sum := sha256.Sum256(payload)
	manifest := fmt.Sprintf("%x  bundle", sum)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// Wrong bytes with 200 OK: proxy substitution / truncated cache.
			fmt.Fprint(w, "<html>not the bundle</html>")
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "bundle")
	if err := downloadAssetFileVerified(server.URL, dest, manifest); err != nil {
		t.Fatalf("downloadAssetFileVerified() error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("server calls = %d, want 2", got)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("dest content mismatch after verified retry")
	}
}

func TestDownloadAssetFileVerifiedFailsWithAggregatedAttempts(t *testing.T) {
	disableAssetRetryDelay(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "always wrong")
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "bundle")
	err := downloadAssetFileVerified(server.URL, dest, "deadbeef  bundle")
	if err == nil {
		t.Fatal("expected persistent checksum mismatch to fail")
	}
	if !strings.Contains(err.Error(), server.URL) {
		t.Fatalf("error should name the asset URL, got %q", err)
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error should mention the checksum mismatch, got %q", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched download must not be left on disk (stat err: %v)", statErr)
	}
}

func TestStageBundleSurvivesSlowClientAndFlakyFirstDownload(t *testing.T) {
	disableAssetRetryDelay(t)
	archivePath, checksumPath := createTestBundleArchive(t, "0.3.1")

	var bundleCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bundle":
			if bundleCalls.Add(1) == 1 {
				http.Error(w, "flaky", http.StatusServiceUnavailable)
				return
			}
			http.ServeFile(w, r, archivePath)
		case "/bundle.sha256":
			http.ServeFile(w, r, checksumPath)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("HA_NOVA_BUNDLE_URL", server.URL+"/bundle")
	t.Setenv("HA_NOVA_BUNDLE_SHA256_URL", server.URL+"/bundle.sha256")

	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	stageRoot, err := stageBundle(paths, "0.3.1")
	if err != nil {
		t.Fatalf("stageBundle() error: %v", err)
	}
	defer cleanupStagedBundle(stageRoot)
	if got := bundleCalls.Load(); got != 2 {
		t.Fatalf("bundle calls = %d, want 2 (retry after 503)", got)
	}
	if _, err := os.Stat(filepath.Join(stageRoot, "bundle.json")); err != nil {
		t.Fatalf("staged bundle metadata missing: %v", err)
	}
}
