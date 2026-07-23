package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallGenerationPreventsNilNonceNilABA(t *testing.T) {
	paths := setupHealableInstall(t)
	oldGeneration, err := readInstallLifecycleGeneration(paths)
	if err != nil {
		t.Fatalf("read initial generation: %v", err)
	}

	if err := markCensusLifecycleStopped(paths); err != nil {
		t.Fatalf("mark lifecycle stopped: %v", err)
	}
	newLifecycle := [][]byte{
		captureInstallLifecycleGeneration(paths),
		captureCensusLifecycleMarker(paths),
	}
	if err := completeSetupLifecycle(paths, newLifecycle...); err != nil {
		t.Fatalf("complete replacement setup lifecycle: %v", err)
	}
	if censusLifecycleStopped(paths) {
		t.Fatal("replacement setup did not reactivate census lifecycle")
	}
	if err := ensureUpdateLifecycleCurrent(paths, oldGeneration); err == nil {
		t.Fatal("pre-uninstall update became valid again after replacement setup")
	}
	if err := ensureSetupLifecycleCurrent(paths, oldGeneration, nil); err == nil {
		t.Fatal("pre-uninstall setup became valid again after replacement setup")
	}
}

func TestSetupLifecycleLockFailurePreservesUninstallGeneration(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := markCensusLifecycleStopped(paths); err != nil {
		t.Fatalf("mark lifecycle stopped: %v", err)
	}
	generation := captureInstallLifecycleGeneration(paths)
	censusMarker := captureCensusLifecycleMarker(paths)

	originalRetry, originalTimeout := censusLockRetryInterval, censusLockTimeout
	censusLockRetryInterval = time.Millisecond
	censusLockTimeout = 30 * time.Millisecond
	t.Cleanup(func() {
		censusLockRetryInterval, censusLockTimeout = originalRetry, originalTimeout
	})
	release, acquired := acquireCensusLock(paths)
	if !acquired {
		t.Fatal("hold census lifecycle lock")
	}
	defer release()

	err := completeSetupLifecycle(paths, generation, censusMarker)
	if err == nil || !strings.Contains(err.Error(), "cannot acquire census lifecycle lock") {
		t.Fatalf("setup lifecycle lock failure = %v", err)
	}
	currentGeneration := captureInstallLifecycleGeneration(paths)
	if !bytes.Equal(currentGeneration, generation) {
		t.Fatal("failed setup finalization rotated the uninstall generation")
	}
	currentMarker := captureCensusLifecycleMarker(paths)
	if !bytes.Equal(currentMarker, censusMarker) || !censusLifecycleStopped(paths) {
		t.Fatal("failed setup finalization cleared the uninstall marker")
	}
}

func TestInstallGenerationReadErrorsFailClosed(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := rotateInstallLifecycleGeneration(paths); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	captured := captureInstallLifecycleGeneration(paths)
	path := installLifecycleGenerationPath(paths)
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove generation file: %v", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("replace generation with unreadable directory: %v", err)
	}

	if err := ensureUpdateLifecycleCurrent(paths, captured); err == nil ||
		!strings.Contains(err.Error(), "cannot verify update lifecycle") {
		t.Fatalf("update generation read error did not fail closed: %v", err)
	}
	if err := ensureSetupLifecycleCurrent(paths, captured); err == nil ||
		!strings.Contains(err.Error(), "cannot verify setup lifecycle") {
		t.Fatalf("setup generation read error did not fail closed: %v", err)
	}
}

func TestSetupRejectsCorruptCensusLifecycleMarker(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, marker string)
	}{
		{
			name: "empty",
			setup: func(t *testing.T, marker string) {
				t.Helper()
				if err := os.WriteFile(marker, nil, 0o600); err != nil {
					t.Fatalf("write empty marker: %v", err)
				}
			},
		},
		{
			name: "unreadable path",
			setup: func(t *testing.T, marker string) {
				t.Helper()
				if err := os.Mkdir(marker, 0o700); err != nil {
					t.Fatalf("create marker directory: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupHealableInstall(t)
			marker := censusLifecycleMarkerPath(paths)
			if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
				t.Fatalf("create marker parent: %v", err)
			}
			tc.setup(t, marker)

			exitCode, output := captureCommandOutput(t, func() int {
				return runSetup(paths, []string{"codex", "--non-interactive"})
			})
			if exitCode == 0 {
				t.Fatalf("setup unexpectedly accepted corrupt marker:\n%s", output)
			}
			if !strings.Contains(output, "cannot inspect uninstall lifecycle") {
				t.Fatalf("missing fail-closed lifecycle error:\n%s", output)
			}
			if _, err := os.Stat(paths.ConfigFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("setup wrote config before lifecycle validation: %v", err)
			}
		})
	}
}

func TestSetupLifecycleRejectsConcurrentServerDefaultChange(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	generation, err := readInstallLifecycleGeneration(paths)
	if err != nil {
		t.Fatal(err)
	}
	censusMarker, err := readCensusLifecycleMarker(paths)
	if err != nil {
		t.Fatal(err)
	}
	configSnapshot, err := readSetupConfigSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	setupLifecycle := [][]byte{generation, censusMarker, configSnapshot}

	if exit := runServerDefault(paths, []string{"cabin"}); exit != 0 {
		t.Fatalf("concurrent server default exit = %d", exit)
	}
	mutated := false
	err = withSetupLifecycleLock(paths, setupLifecycle, func() error {
		mutated = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "server configuration changed during setup") {
		t.Fatalf("stale setup was not rejected: %v", err)
	}
	if mutated {
		t.Fatal("stale setup reached its mutation after server default changed")
	}
}

func TestStaleMaintenancePathsCannotRestoreAfterUninstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated Windows privileges")
	}
	paths := setupHealableInstall(t)
	generation := captureInstallLifecycleGeneration(paths)
	state := installState{
		SchemaVersion:    stateSchemaVersion,
		Version:          "0.6.1",
		InstalledClients: []string{"codex"},
	}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if _, err := installClient(paths, paths.InstallRoot, "codex"); err != nil {
		t.Fatalf("seed Codex install: %v", err)
	}
	codexRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	if err := os.Remove(codexRoot); err != nil {
		t.Fatalf("remove Codex install: %v", err)
	}
	if err := os.Remove(paths.StateFile); err != nil {
		t.Fatalf("remove state: %v", err)
	}
	if err := markCensusLifecycleStopped(paths); err != nil {
		t.Fatalf("mark lifecycle stopped: %v", err)
	}

	if result := postUpdateSyncWithResultForLifecycle(paths, generation); result.Err == nil {
		t.Fatal("stale post-update sync succeeded after uninstall")
	}
	ensureClientsVerifiedForCurrentVersion(paths)
	if repairMissingSessionBootstrap(paths) {
		t.Fatal("session-bootstrap repair ran after uninstall")
	}
	markSessionBootstrapLayoutVerified(paths)
	outcomes := runClientAutoRepair(paths, []clientStatus{{
		ID: "codex", Label: "Codex", RuntimeDetected: true,
	}})
	if len(outcomes) != 1 || !outcomes[0].Skipped {
		t.Fatalf("auto-repair did not fail closed after uninstall: %+v", outcomes)
	}

	for _, path := range []string{
		paths.StateFile,
		codexRoot,
		filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker),
		filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("stale maintenance recreated %s (err=%v)", path, err)
		}
	}
}

func TestPlainCheckUpdateDoesNotCreateFirstUseCarrier(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated Windows privileges")
	}
	paths := setupHealableInstall(t)
	root := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		t.Fatalf("mkdir client parent: %v", err)
	}
	if err := copyDir(filepath.Join(paths.InstallRoot, "skills"), root); err != nil {
		t.Fatalf("copy old client skills: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "ha-nova", "session-bootstrap.md")); err != nil {
		t.Fatalf("remove copied bootstrap: %v", err)
	}
	if err := saveState(paths, installState{
		SchemaVersion:          stateSchemaVersion,
		Version:                "0.6.1",
		ClientsVerifiedVersion: "0.6.1",
		InstalledClients:       []string{"codex"},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	cacheReleaseInfo(paths, releaseInfo{Version: "0.6.1"})

	exit, _ := captureCommandOutput(t, func() int {
		return runCheckUpdate(paths, nil)
	})
	if exit != 0 {
		t.Fatalf("plain check-update exit = %d", exit)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if markerHasCarrierPending(marker, "0.6.1") || markerHasCarrierRunning(marker, "0.6.1") {
		t.Fatal("plain check-update created a first-use carrier")
	}
}
