package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// versionDisplay is what `ha-nova version` prints. Released builds must show the
// bare version; locally dev-synced builds must self-identify so any client's LLM
// can answer "which build is loaded?" without inspecting skill files.
func TestVersionDisplayChannel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.4.2","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}

	origChannel, origStamp := BuildChannel, BuildStamp
	t.Cleanup(func() { BuildChannel, BuildStamp = origChannel, origStamp })

	// Released build: no ldflags injected -> bare version, no DEV label.
	BuildChannel, BuildStamp = "", ""
	if got := versionDisplay(paths); got != "0.4.2" {
		t.Fatalf("release versionDisplay() = %q, want %q", got, "0.4.2")
	}

	// Dev build: stamped -> keeps the version and adds a clear DEV marker.
	BuildChannel, BuildStamp = "dev", "2026-06-14T09:30-abc123"
	got := versionDisplay(paths)
	for _, want := range []string{"0.4.2", "local DEV build", "2026-06-14T09:30-abc123"} {
		if !strings.Contains(got, want) {
			t.Fatalf("dev versionDisplay() = %q, want it to contain %q", got, want)
		}
	}

	// Dev build without a stamp still self-identifies as DEV.
	BuildChannel, BuildStamp = "dev", ""
	got = versionDisplay(paths)
	if !strings.Contains(got, "local DEV build") || !strings.Contains(got, "unstamped") {
		t.Fatalf("unstamped dev versionDisplay() = %q, want DEV + unstamped marker", got)
	}
}
