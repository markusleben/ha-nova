package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
