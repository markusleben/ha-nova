package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestBuildUpdateCheckResultGuidesLegacyWindowsPackageReinstall(t *testing.T) {
	originalPlatform := channelChecksUseWindowsPlatform
	originalExecutable := executablePathForInstallSource
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
		executablePathForInstallSource = originalExecutable
	}()

	channelChecksUseWindowsPlatform = func() bool { return true }
	executablePathForInstallSource = func() (string, error) {
		return filepath.Join(t.TempDir(), publicBinaryName()), nil
	}

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
	if err := saveState(paths, installState{InstallSource: "winget"}); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	result := buildUpdateCheckResult(paths)
	if result.Status != "update_available" {
		t.Fatalf("result.Status = %q, want update_available", result.Status)
	}
	if result.InstallSource != installSourceLegacyWindowsPackage {
		t.Fatalf("result.InstallSource = %q, want %q", result.InstallSource, installSourceLegacyWindowsPackage)
	}
	if strings.Contains(result.Message, "Run: ha-nova update") {
		t.Fatalf("unexpected in-place update guidance for legacy Windows package: %q", result.Message)
	}
	for _, want := range []string{
		"Installed Apps / App Installer",
		"install.ps1 | iex",
	} {
		if !strings.Contains(result.Message, want) {
			t.Fatalf("expected legacy reinstall guidance %q in %q", want, result.Message)
		}
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

func TestRunUpdateIgnoresFreshReleaseCacheWhenResolvingTargetVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.2.3","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: "0.2.1", HTMLURL: "https://example.invalid/releases/v0.2.1"}, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	originalHTTPClient := httpClient
	defer func() {
		httpClient = originalHTTPClient
	}()
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"tag_name":"v0.2.3","html_url":"https://example.test/release"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	output := captureStdout(t, func() {
		if exitCode := runUpdate(paths, nil); exitCode != 0 {
			t.Fatalf("runUpdate() exit = %d, want 0", exitCode)
		}
	})

	if !strings.Contains(output, "Already up to date: v0.2.3") {
		t.Fatalf("expected up-to-date message, got %q", output)
	}
	if strings.Contains(output, "target v0.2.1") {
		t.Fatalf("expected update target to ignore fresh cache, got %q", output)
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

func TestRunCheckUpdateJSONOffersStableReturnFromRC(t *testing.T) {
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

	output := captureStdout(t, func() {
		if exitCode := runCheckUpdate(paths, []string{"--json"}); exitCode != 0 {
			t.Fatalf("runCheckUpdate() exit = %d, want 0", exitCode)
		}
	})

	var result updateCheckResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal output error: %v\noutput=%s", err, output)
	}
	if result.Status != "update_available" {
		t.Fatalf("result.Status = %q, want update_available", result.Status)
	}
	if result.Message != "Return to stable: v0.4.0-rc1 -> v0.3.1 | Run: ha-nova update" {
		t.Fatalf("unexpected JSON stable return guidance: %q", result.Message)
	}
}

func TestRunCheckUpdateTextModeUsesSingleFetch(t *testing.T) {
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

	originalHTTPClient := httpClient
	defer func() {
		httpClient = originalHTTPClient
	}()

	requests := 0
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			body := `{"tag_name":"v0.2.0","html_url":"https://example.test/release"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	if exitCode := runCheckUpdate(paths, nil); exitCode != 0 {
		t.Fatalf("runCheckUpdate() exit = %d, want 0", exitCode)
	}

	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestRunCheckUpdateTextModeOffersStableReturnFromRC(t *testing.T) {
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

	output := captureStderr(t, func() {
		if exitCode := runCheckUpdate(paths, nil); exitCode != 0 {
			t.Fatalf("runCheckUpdate() exit = %d, want 0", exitCode)
		}
	})

	if !strings.Contains(output, "Return to stable: v0.4.0-rc1 -> v0.3.1 | Run: ha-nova update") {
		t.Fatalf("expected text stable return guidance, got %q", output)
	}
}

func TestBuildUpdateCheckResultRejectsInvalidLatestReleaseTag(t *testing.T) {
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

	originalHTTPClient := httpClient
	defer func() {
		httpClient = originalHTTPClient
	}()
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"tag_name":"v0.4.0-beta1","html_url":"https://example.test/release"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	result := buildUpdateCheckResult(paths)
	if result.Status != "check_failed" {
		t.Fatalf("result.Status = %q, want check_failed", result.Status)
	}
	if !strings.Contains(result.Message, "latest release tag invalid") {
		t.Fatalf("expected invalid latest release tag error, got %q", result.Message)
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

func TestCheckRelayVersionWarnsOnUnsupportedRelayVersionFormat(t *testing.T) {
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

	notice := checkRelayVersion(paths, []byte(`{"version":"0.2.0-beta1"}`))
	if notice.kind != humanNoticeKindRelayOutdated {
		t.Fatalf("notice.kind = %q, want %q", notice.kind, humanNoticeKindRelayOutdated)
	}
	if notice.level != humanNoticeWarning {
		t.Fatalf("notice.level = %q, want %q", notice.level, humanNoticeWarning)
	}
	if !strings.Contains(notice.message, "unsupported relay version format") {
		t.Fatalf("unexpected relay warning message: %q", notice.message)
	}
}

func TestCheckRelayVersionReadsTheRealHealthEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.14.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	// Verbatim shape of a live GET /health response: the version sits inside
	// the relay's {"ok":true,"data":{...}} envelope, not at the top level. A
	// parser that only reads a top-level "version" silently skips the check —
	// doctor then reports a healthy setup while the relay is below the floor.
	body := []byte(`{"ok":true,"data":{"status":"ok","ha_ws_connected":true,"version":"0.2.6","uptime_s":322135}}`)
	notice := checkRelayVersion(paths, body)
	if notice.kind != humanNoticeKindRelayOutdated {
		t.Fatalf("notice.kind = %q, want %q (envelope body must not be skipped)", notice.kind, humanNoticeKindRelayOutdated)
	}
	if !strings.Contains(notice.message, "Relay outdated: v0.2.6 is below minimum v0.4.0") {
		t.Fatalf("unexpected relay warning message: %q", notice.message)
	}
}

func TestRelayFloorNoticeWarnsWhenLiveRelayIsBelowFloor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"status":"ok","ha_ws_connected":true,"version":"0.2.6"}}`))
	}))
	defer relay.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.14.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: relay.URL}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	notice := relayFloorNotice(paths)
	if notice.kind != humanNoticeKindRelayOutdated {
		t.Fatalf("notice.kind = %q, want %q", notice.kind, humanNoticeKindRelayOutdated)
	}
	if !strings.Contains(notice.message, "Relay outdated: v0.2.6 is below minimum v0.4.0") {
		t.Fatalf("unexpected relay warning message: %q", notice.message)
	}
}

func TestRelayFloorNoticeStaysSilentWhenRelayUnreachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.14.0","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	// Port 1 refuses connections without ever having been bound by this
	// process — no close-then-rebind recycling risk (see #310).
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: "http://127.0.0.1:1"}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	if notice := relayFloorNotice(paths); !notice.empty() {
		t.Fatalf("expected silence for unreachable relay, got %q", notice.message)
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
