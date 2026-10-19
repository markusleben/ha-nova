package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Regression (rc2 lane A, D1): Windows installer detection elevates an
// unmanifested exe by file name, so `update` failed and `uninstall` raised UAC.
func TestWindowsHelperNamesAvoidInstallerDetectionKeywords(t *testing.T) {
	for _, prefix := range []string{windowsUpdateHelperPrefix, windowsUninstallHelperPrefix} {
		name := strings.ToLower(prefix + "1234.exe")
		for _, keyword := range []string{"install", "setup", "update", "patch", "upgrade"} {
			if strings.Contains(name, keyword) {
				t.Errorf("helper name %q contains %q: Windows would demand elevation", name, keyword)
			}
		}
	}
	if windowsUpdateHelperPrefix == windowsUninstallHelperPrefix {
		t.Fatal("update and uninstall helpers must have distinct names")
	}
}

func TestSweepStaleWindowsHelpersRemovesOldLeftoversOnly(t *testing.T) {
	tmp := isolateTempDir(t)
	old := time.Now().Add(-2 * staleWindowsHelperAge)
	write := func(name string, mod time.Time) string {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
		return path
	}
	stale := []string{
		write("ha-nova-updater-9672.exe", old),
		write("HA-NOVA-UNINSTALL-2692.exe", old),
		write("ha-nova-helper-1.exe", old),
		write("ha-nova-remover-2.exe", old),
	}
	stageDir := filepath.Join(tmp, "ha-nova-stage-1890731207")
	if err := os.MkdirAll(filepath.Join(stageDir, "ha-nova"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stageDir, old, old); err != nil {
		t.Fatal(err)
	}
	stale = append(stale, stageDir)
	// This run's own staged bundle survives however old it is.
	ownStage := filepath.Join(tmp, "ha-nova-stage-own")
	if err := os.MkdirAll(filepath.Join(ownStage, "ha-nova"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(ownStage, old, old); err != nil {
		t.Fatal(err)
	}
	kept := []string{
		write("ha-nova-helper-3.exe", time.Now()), // another run's fresh copy
		write("ha-nova-helper-4.txt", old),
		write("other-updater-5.exe", old),
		ownStage,
	}

	sweepStaleWindowsHelpers(filepath.Join(ownStage, "ha-nova"))

	for _, path := range stale {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("stale leftover survived: %s (%v)", path, err)
		}
	}
	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("sweep removed %s: %v", path, err)
		}
	}
}

func TestLaunchWindowsUninstallRemovesHelperCopyWhenLaunchFails(t *testing.T) {
	isolateTempDir(t)
	root := t.TempDir()
	// A text file named like the CLI: the copy succeeds, the start fails.
	if err := os.WriteFile(filepath.Join(root, publicBinaryName()), []byte("not an executable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := runtimePaths{
		InstallRoot:         root,
		UninstallStatusFile: filepath.Join(root, "uninstall-status.json"),
	}
	if err := launchWindowsUninstall(paths, uninstallModeStandard, false, nil); err == nil {
		t.Fatal("launch of a non-executable helper unexpectedly succeeded")
	}
	helper := filepath.Join(os.TempDir(), windowsUninstallHelperPrefix+strconv.Itoa(os.Getpid())+".exe")
	if _, err := os.Stat(helper); !os.IsNotExist(err) {
		t.Fatalf("helper copy %s must be removed after a failed launch (%v)", helper, err)
	}
}

// isolateTempDir points os.TempDir() at a test-owned dir, so the helper sweep
// never touches the shared machine temp.
func isolateTempDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("TMP", tmp)
	return tmp
}
