package main

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsCensusMutexContractIsCrossSessionAndContentionSafe(t *testing.T) {
	source, err := os.ReadFile("census_lock_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{`Global\HA_NOVA_CENSUS_`, "windows.ERROR_ALREADY_EXISTS", "windows.WaitForSingleObject", "runtime.LockOSThread()", "runtime.UnlockOSThread()"} {
		if !strings.Contains(text, required) {
			t.Fatalf("Windows census mutex source missing %q", required)
		}
	}
	if strings.Contains(text, `Local\HA_NOVA_CENSUS_`) {
		t.Fatal("session-local mutex would allow cross-session duplicate sends")
	}
}

func TestWindowsCensusStateIsDeviceLocalAndLegacyRoamingStateIsOnlyDeleted(t *testing.T) {
	pathsSource, err := os.ReadFile("paths.go")
	if err != nil {
		t.Fatal(err)
	}
	uninstallSource, err := os.ReadFile("uninstall_paths.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pathsSource), "censusDir = localDataDir") {
		t.Fatal("Windows census state must use device-local LOCALAPPDATA")
	}
	if !strings.Contains(string(uninstallSource), `filepath.Join(paths.ConfigDir, "census.json")`) {
		t.Fatal("uninstall must remove, not migrate, legacy roaming census consent")
	}
}
