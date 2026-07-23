package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunCheckUpdateQuietBoundsRelayDiscoveryAsOneDeadline(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer relay.Close()
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
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{
		Version: "0.20.0",
		HTMLURL: "https://example.invalid/releases/v0.20.0",
	}, 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}
	t.Setenv("HA_NOVA_NO_CENSUS", "1")
	previousTimeout := firstUseRelayNoticeTimeout
	firstUseRelayNoticeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { firstUseRelayNoticeTimeout = previousTimeout })

	start := time.Now()
	if exit := runCheckUpdate(paths, []string{"--quiet"}); exit != 0 {
		t.Fatalf("best-effort Relay timeout changed exit code: %d", exit)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("quiet Relay discovery exceeded one bounded deadline: %s", elapsed)
	}
}

func TestRunCheckUpdateQuietSurfacesReturnToStable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.22.0-rc1","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{
		Version: "0.21.0",
		HTMLURL: "https://example.invalid/releases/v0.21.0",
	}, 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runCheckUpdate(paths, []string{"--quiet"})
	})
	if exitCode != 0 {
		t.Fatalf("stable-return notice must not fail check-update: exit %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Return to stable: v0.22.0-rc1 -> v0.21.0") {
		t.Fatalf("quiet check-update missed stable-return notice:\n%s", output)
	}
}
