package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildUpdateCheckResultOffersStableUpgradeFromRC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.4.0-rc1","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: "0.3.1", HTMLURL: "https://example.invalid/releases/v0.3.1"}, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	result := buildUpdateCheckResult(paths)
	if result.Status != "update_available" {
		t.Fatalf("result.Status = %q, want update_available", result.Status)
	}
	if !strings.Contains(result.Message, "Return to stable: v0.4.0-rc1 -> v0.3.1 | Run: ha-nova update") {
		t.Fatalf("expected stable return guidance, got %q", result.Message)
	}
}

func TestRunUpdateRejectsUnsupportedPrereleaseFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	stderr := captureStderr(t, func() {
		if exitCode := runUpdate(paths, []string{"--version", "0.3.1-beta1"}); exitCode != 1 {
			t.Fatalf("runUpdate() exit = %d, want 1", exitCode)
		}
	})

	if !strings.Contains(stderr, "use X.Y.Z or X.Y.Z-rcN") {
		t.Fatalf("expected explicit prerelease guidance, got %q", stderr)
	}
}

func TestRunUpdateReturnsSelfManagedRCToStable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.4.0-rc1","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	archivePath, checksumPath := createTestBundleArchive(t, "0.3.1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bundle":
			http.ServeFile(w, r, archivePath)
		case "/bundle.sha256":
			http.ServeFile(w, r, checksumPath)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalHTTPClient := httpClient
	defer func() { httpClient = originalHTTPClient }()
	serverTransport := server.Client().Transport
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() == latestReleaseURL {
				body := `{"tag_name":"v0.3.1","html_url":"https://example.test/release"}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}
			return serverTransport.RoundTrip(req)
		}),
	}
	t.Setenv("HA_NOVA_BUNDLE_URL", server.URL+"/bundle")
	t.Setenv("HA_NOVA_BUNDLE_SHA256_URL", server.URL+"/bundle.sha256")

	stdout := captureStdout(t, func() {
		if exitCode := runUpdate(paths, nil); exitCode != 0 {
			t.Fatalf("runUpdate() exit = %d, want 0", exitCode)
		}
	})

	if !strings.Contains(stdout, "Updated to v0.3.1") {
		t.Fatalf("expected stable rollback message, got %q", stdout)
	}
	if got := localVersion(paths); got != "0.3.1" {
		t.Fatalf("localVersion() = %q, want 0.3.1", got)
	}
}

func TestRunUpdateExactRCFromStableInstallsThatRC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.3.0","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	archivePath, checksumPath := createTestBundleArchive(t, "0.3.1-rc1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bundle":
			http.ServeFile(w, r, archivePath)
		case "/bundle.sha256":
			http.ServeFile(w, r, checksumPath)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalHTTPClient := httpClient
	defer func() { httpClient = originalHTTPClient }()
	serverTransport := server.Client().Transport
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return serverTransport.RoundTrip(req)
		}),
	}
	t.Setenv("HA_NOVA_BUNDLE_URL", server.URL+"/bundle")
	t.Setenv("HA_NOVA_BUNDLE_SHA256_URL", server.URL+"/bundle.sha256")

	stdout := captureStdout(t, func() {
		if exitCode := runUpdate(paths, []string{"--version", "v0.3.1-rc1"}); exitCode != 0 {
			t.Fatalf("runUpdate() exit = %d, want 0", exitCode)
		}
	})

	if !strings.Contains(stdout, "Updated to v0.3.1-rc1") {
		t.Fatalf("expected exact RC pin update message, got %q", stdout)
	}
	if got := localVersion(paths); got != "0.3.1-rc1" {
		t.Fatalf("localVersion() = %q, want 0.3.1-rc1", got)
	}
}

func TestRunUpdateExactStablePinFromRCInstallsThatStable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.4.0-rc1","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	archivePath, checksumPath := createTestBundleArchive(t, "0.3.0")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bundle":
			http.ServeFile(w, r, archivePath)
		case "/bundle.sha256":
			http.ServeFile(w, r, checksumPath)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalHTTPClient := httpClient
	defer func() { httpClient = originalHTTPClient }()
	serverTransport := server.Client().Transport
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return serverTransport.RoundTrip(req)
		}),
	}
	t.Setenv("HA_NOVA_BUNDLE_URL", server.URL+"/bundle")
	t.Setenv("HA_NOVA_BUNDLE_SHA256_URL", server.URL+"/bundle.sha256")

	stdout := captureStdout(t, func() {
		if exitCode := runUpdate(paths, []string{"--version", "0.3.0"}); exitCode != 0 {
			t.Fatalf("runUpdate() exit = %d, want 0", exitCode)
		}
	})

	if !strings.Contains(stdout, "Updated to v0.3.0") {
		t.Fatalf("expected exact stable pin update message, got %q", stdout)
	}
	if got := localVersion(paths); got != "0.3.0" {
		t.Fatalf("localVersion() = %q, want 0.3.0", got)
	}
}

func TestRunUpdateTreatsSameRCTargetAsUpToDate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.3.2-rc1","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	stdout := captureStdout(t, func() {
		if exitCode := runUpdate(paths, []string{"--version", "0.3.2-rc1"}); exitCode != 0 {
			t.Fatalf("runUpdate() exit = %d, want 0", exitCode)
		}
	})

	if !strings.Contains(stdout, "Already up to date: v0.3.2-rc1") {
		t.Fatalf("expected same-RC no-op message, got %q", stdout)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
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

func createTestBundleArchive(t *testing.T, version string) (string, string) {
	t.Helper()

	root := t.TempDir()
	bundleRoot := filepath.Join(root, "ha-nova")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "clients"), 0o755); err != nil {
		t.Fatalf("mkdir bundle clients: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, publicBinaryName()), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write bundle runtime: %v", err)
	}
	bundleJSON := fmt.Sprintf(`{"bundle_format_version":1,"version":"%s","os":"%s","arch":"%s","binary_name":"%s"}`, version, bundlePlatformOS(), bundlePlatformArch(), publicBinaryName())
	if err := os.WriteFile(filepath.Join(bundleRoot, "bundle.json"), []byte(bundleJSON), 0o644); err != nil {
		t.Fatalf("write bundle metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "clients", "registry.json"), []byte(`{"clients":[{"id":"claude","label":"Claude Code","adapter_kind":"plugin_marketplace","supported_os":["macos","linux","windows"]}]}`), 0o644); err != nil {
		t.Fatalf("write registry.json: %v", err)
	}
	var archivePath string
	if bundlePlatformOS() == "windows" {
		archivePath = filepath.Join(root, bundleAssetName())
		createZipArchive(t, archivePath, bundleRoot)
	} else {
		archivePath = filepath.Join(root, bundleAssetName())
		createTarGzArchive(t, archivePath, bundleRoot)
	}
	sum := sha256.Sum256(mustReadFile(t, archivePath))
	checksumPath := archivePath + ".sha256"
	if err := os.WriteFile(checksumPath, []byte(fmt.Sprintf("%x  %s\n", sum, filepath.Base(archivePath))), 0o644); err != nil {
		t.Fatalf("write checksum: %v", err)
	}
	return archivePath, checksumPath
}

func createTarGzArchive(t *testing.T, archivePath, bundleRoot string) {
	t.Helper()

	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer file.Close()
	gzw := gzip.NewWriter(file)
	defer gzw.Close()
	tw := tar.NewWriter(gzw)
	defer tw.Close()

	files := []struct {
		path string
		mode int64
	}{
		{path: "ha-nova/" + publicBinaryName(), mode: 0o755},
		{path: "ha-nova/bundle.json", mode: 0o644},
		{path: "ha-nova/clients/registry.json", mode: 0o644},
	}
	for _, fileMeta := range files {
		data := mustReadFile(t, filepath.Join(bundleRoot, strings.TrimPrefix(fileMeta.path, "ha-nova/")))
		header := &tar.Header{
			Name: fileMeta.path,
			Mode: fileMeta.mode,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
}

func createZipArchive(t *testing.T, archivePath, bundleRoot string) {
	t.Helper()

	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	defer zw.Close()

	files := []string{
		"ha-nova/" + publicBinaryName(),
		"ha-nova/bundle.json",
		"ha-nova/clients/registry.json",
	}
	for _, name := range files {
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		data := mustReadFile(t, filepath.Join(bundleRoot, strings.TrimPrefix(name, "ha-nova/")))
		if _, err := writer.Write(data); err != nil {
			t.Fatalf("write zip body: %v", err)
		}
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
