package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An uninstall, then an older release's setup (0.25.1 rotates the shared
// generation and writes state, but doesn't know this marker): the marker is
// stale and must not block the working install. Every other order keeps it.
func TestStaleUninstallMarkerClearedOnlyAfterLaterSetup(t *testing.T) {
	cases := []struct {
		name      string
		prepare   func(t *testing.T, paths runtimePaths)
		wantClear bool
	}{
		{"setup after the uninstall", func(t *testing.T, paths runtimePaths) {}, true},
		{"no setup since the uninstall", func(t *testing.T, paths runtimePaths) {
			if err := markInstallLifecycleStopped(paths); err != nil { // a fresh uninstall: newest marker
				t.Fatal(err)
			}
			_ = os.Remove(legacyUninstallMarkerPath(paths))
		}, false},
		{"census-era marker still there", func(t *testing.T, paths runtimePaths) {
			writeStaleTestFile(t, legacyUninstallMarkerPath(paths), time.Now())
		}, false},
		{"state not written since", func(t *testing.T, paths runtimePaths) {
			writeStaleTestFile(t, paths.StateFile, time.Now().Add(-2*time.Hour))
		}, false},
		{"generation not rotated since", func(t *testing.T, paths runtimePaths) {
			writeStaleTestFile(t, installLifecycleGenerationPath(paths), time.Now().Add(-2*time.Hour))
		}, false},
		{"same time stays", func(t *testing.T, paths runtimePaths) {
			marker, _ := os.Stat(uninstallMarkerPath(paths))
			writeStaleTestFile(t, installLifecycleGenerationPath(paths), marker.ModTime())
		}, false},
		{"no config", func(t *testing.T, paths runtimePaths) {
			_ = os.Remove(paths.ConfigFile)
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupHealableInstall(t)
			// The uninstall an hour ago, then the older release's setup now.
			if err := markInstallLifecycleStopped(paths); err != nil {
				t.Fatal(err)
			}
			hourAgo := time.Now().Add(-time.Hour)
			for _, path := range []string{uninstallMarkerPath(paths), installLifecycleGenerationPath(paths)} {
				if err := os.Chtimes(path, hourAgo, hourAgo); err != nil {
					t.Fatal(err)
				}
			}
			_ = os.Remove(legacyUninstallMarkerPath(paths)) // that setup cleared its own marker
			if err := rotateInstallLifecycleGeneration(paths); err != nil {
				t.Fatal(err)
			}
			writeStaleTestFile(t, paths.StateFile, time.Now())
			writeStaleTestFile(t, paths.ConfigFile, time.Now())
			tc.prepare(t, paths)

			clearStaleUninstallMarker(paths)
			if got := !installLifecycleStopped(paths); got != tc.wantClear {
				t.Fatalf("marker cleared = %v, want %v", got, tc.wantClear)
			}
		})
	}
}

// The lock decides: while another process holds it (an uninstall writing
// its marker), nothing is cleared.
func TestStaleUninstallMarkerKeptWhileLocked(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := markInstallLifecycleStopped(paths); err != nil {
		t.Fatal(err)
	}
	hourAgo := time.Now().Add(-time.Hour)
	_ = os.Chtimes(uninstallMarkerPath(paths), hourAgo, hourAgo)
	_ = os.Remove(legacyUninstallMarkerPath(paths))
	writeStaleTestFile(t, installLifecycleGenerationPath(paths), time.Now())
	writeStaleTestFile(t, paths.StateFile, time.Now())
	writeStaleTestFile(t, paths.ConfigFile, time.Now())
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		t.Fatal("hold lock")
	}
	clearStaleUninstallMarker(paths)
	release()
	if !installLifecycleStopped(paths) {
		t.Fatal("marker cleared without the lock")
	}
}

func writeStaleTestFile(t *testing.T, path string, at time.Time) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`"x"`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}
