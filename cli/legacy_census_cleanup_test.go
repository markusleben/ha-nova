package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func legacyCensusTestPaths(t *testing.T) runtimePaths {
	t.Helper()
	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(key, home)
	}
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	return paths
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchRemovesLegacyCensusState(t *testing.T) {
	paths := legacyCensusTestPaths(t)
	leftovers := []string{
		filepath.Join(paths.ConfigDir, "census.json"),
		filepath.Join(paths.ConfigDir, "census.lock"),
		filepath.Join(paths.LocalDataDir, "census.json"),
		filepath.Join(paths.CacheDir, "census-ask"),
	}
	for _, path := range leftovers {
		writeTestFile(t, path, `{"enabled":true}`)
	}
	marker := uninstallMarkerPath(paths)
	legacyMarker := filepath.Join(filepath.Dir(marker), ".ha-nova-census-uninstalled")
	writeTestFile(t, legacyMarker, `"opaque-nonce"`)

	if exit, output := captureCommandOutput(t, func() int {
		return dispatch(paths, "ha-nova", []string{"version"})
	}); exit != 0 {
		t.Fatalf("version exit = %d:\n%s", exit, output)
	}

	for _, path := range append(leftovers, legacyMarker) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy census file survived: %s (%v)", path, err)
		}
	}
	// The old uninstall barrier keeps its opaque nonce under the new name, so
	// a setup that captured it before the upgrade still matches.
	if data, err := os.ReadFile(marker); err != nil || string(data) != `"opaque-nonce"` {
		t.Fatalf("uninstall marker = %q, %v", data, err)
	}
	if !installLifecycleStopped(paths) {
		t.Fatal("migrated uninstall marker no longer stops stale processes")
	}
}

func TestCensusCommandIsUnknown(t *testing.T) {
	paths := legacyCensusTestPaths(t)
	exit, output := captureCommandOutput(t, func() int {
		return dispatch(paths, "ha-nova", []string{"census", "status"})
	})
	if exit != 1 || !strings.Contains(output, "Unknown command: census") {
		t.Fatalf("census status exit = %d:\n%s", exit, output)
	}
	if strings.Contains(strings.ToLower(output), "ha-nova census") {
		t.Fatalf("usage still advertises census:\n%s", output)
	}
}

func TestSetupCompletionDropsLegacyUninstallMarker(t *testing.T) {
	// The startup rename can fail transiently; a completed setup must still
	// not leave the old-release barrier behind to re-block the fresh install.
	paths := legacyCensusTestPaths(t)
	legacy := legacyUninstallMarkerPath(paths)
	writeTestFile(t, legacy, `"0123"`)
	if _, err := clearUninstallMarkerAfterSetup(paths, nil); err != nil {
		t.Fatalf("clearUninstallMarkerAfterSetup() error: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy marker still present after setup completion: %v", err)
	}
}
