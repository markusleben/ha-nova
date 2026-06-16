package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A locally dev-synced build (BuildChannel=dev) must never be told to update,
// even when version.json sits below the latest release — otherwise `ha-nova
// update` would overwrite the developer's working tree. Released builds leave
// BuildChannel empty, so the real update path is unaffected. Regression for the
// review finding: localVersion reads the real version.json, so the existing
// `current == "dev"` short-circuit never fires after dev-sync.
func TestBuildUpdateCheckResultSuppressesNudgeOnDevBuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	// Local version below the latest cached release → would normally nudge.
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.1.0","min_relay_version":"0.1.0"}`), 0o644); err != nil {
		t.Fatalf("write version file: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: "0.2.0"}, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	orig := BuildChannel
	t.Cleanup(func() { BuildChannel = orig })

	// Release build (no channel): the same setup MUST report an update.
	BuildChannel = ""
	if got := buildUpdateCheckResult(paths); got.Status != "update_available" {
		t.Fatalf("release build: Status = %q, want update_available", got.Status)
	}

	// Dev build: the nudge is suppressed.
	BuildChannel = "dev"
	got := buildUpdateCheckResult(paths)
	if got.Status != "up_to_date" {
		t.Fatalf("dev build: Status = %q, want up_to_date", got.Status)
	}
	if got.UpdateAvailable {
		t.Fatal("dev build: UpdateAvailable must be false")
	}
}
