package main

import (
	"path/filepath"
	"testing"
)

// Regression: a passwordless (device) setup must stamp state.Version like the
// interactive path, or later ensureClientsVerifiedForCurrentVersion treats the
// paired install as pre-setup.
func TestPersistDeviceSetupStateStampsVersion(t *testing.T) {
	dir := t.TempDir()
	paths := runtimePaths{
		ConfigFile: filepath.Join(dir, "config.json"),
		StateFile:  filepath.Join(dir, "state.json"),
	}
	state := installState{} // fresh device setup: Version empty

	if err := persistDeviceSetupState(paths, runtimeConfig{RelayBaseURL: "http://relay:8791"}, &state); err != nil {
		t.Fatalf("persistDeviceSetupState: %v", err)
	}
	if state.Version == "" {
		t.Fatal("device setup left state.Version empty")
	}

	saved, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if saved.Version == "" {
		t.Fatal("persisted state.Version is empty")
	}
}
