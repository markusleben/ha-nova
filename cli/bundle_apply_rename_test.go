package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// lockedRename stands in for os.Rename on goos: a call for which fails
// returns true fails with errno, wrapped as os.Rename wraps it; the others
// rename for real. It returns the number of calls so far.
func lockedRename(t *testing.T, goos string, errno syscall.Errno, fails func(call int, from string) bool) *int {
	t.Helper()
	originalOS, originalRename, originalAttempts, originalDelay := installRenamePlatformOS, renameInstallPath, installRenameAttempts, installRenameDelay
	t.Cleanup(func() {
		installRenamePlatformOS, renameInstallPath, installRenameAttempts, installRenameDelay = originalOS, originalRename, originalAttempts, originalDelay
	})
	installRenamePlatformOS, installRenameAttempts, installRenameDelay = goos, 5, time.Millisecond
	calls := 0
	renameInstallPath = func(from, to string) error {
		calls++
		if fails(calls, from) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: errno}
		}
		return os.Rename(from, to)
	}
	return &calls
}

func firstCalls(n int) func(int, string) bool {
	return func(call int, _ string) bool { return call <= n }
}

func installRootFixture(t *testing.T) (installRoot, stageRoot string) {
	t.Helper()
	dir := t.TempDir()
	installRoot, stageRoot = filepath.Join(dir, "ha-nova"), filepath.Join(dir, "stage")
	for root, version := range map[string]string{installRoot: "old", stageRoot: "new"} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "version"), []byte(version), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return installRoot, stageRoot
}

func installedVersion(t *testing.T, installRoot string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(installRoot, "version"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func swapLeftovers(t *testing.T, installRoot string) []string {
	t.Helper()
	found, _ := filepath.Glob(filepath.Join(filepath.Dir(installRoot), ".ha-nova-*"))
	return found
}

// A Windows lock on the install root (access denied, sharing or lock
// violation) that passes within the window is retried, and the update lands.
func TestInstallRootRenameRetriesAWindowsLockThatPasses(t *testing.T) {
	for _, errno := range []syscall.Errno{windowsErrorAccessDenied, windowsErrorSharingViolation, windowsErrorLockViolation} {
		installRoot, stageRoot := installRootFixture(t)
		calls := lockedRename(t, "windows", errno, firstCalls(3))
		replacement, err := replaceInstallRootWithBackup(installRoot, stageRoot)
		if err != nil {
			t.Fatalf("errno %d: %v", errno, err)
		}
		if *calls != 5 || installedVersion(t, installRoot) != "new" || !replacement.hadOld {
			t.Fatalf("errno %d: %d renames, installed %q", errno, *calls, installedVersion(t, installRoot))
		}
		if err := replacement.commit(); err != nil {
			t.Fatal(err)
		}
	}
}

// A lock that outlasts the window fails after a bounded number of tries
// with a message that says what to do, and the old install stays in place.
func TestInstallRootRenameGivesUpOnALockThatStays(t *testing.T) {
	installRoot, stageRoot := installRootFixture(t)
	calls := lockedRename(t, "windows", windowsErrorAccessDenied, firstCalls(1000))
	_, err := replaceInstallRootWithBackup(installRoot, stageRoot)
	if err == nil || !errors.Is(err, windowsErrorAccessDenied) || !strings.Contains(err.Error(), "Nothing was changed") || !strings.Contains(err.Error(), "run ha-nova update again") {
		t.Fatalf("err = %v", err)
	}
	if *calls != 5 || installedVersion(t, installRoot) != "old" || len(swapLeftovers(t, installRoot)) != 0 {
		t.Fatalf("%d renames, installed %q, left %v", *calls, installedVersion(t, installRoot), swapLeftovers(t, installRoot))
	}
}

// Other errors, and the same errno numbers off Windows (5 is EIO on Linux
// and macOS), fail on the first try.
func TestInstallRootRenameDoesNotRetryOtherErrors(t *testing.T) {
	for _, tc := range []struct {
		goos  string
		errno syscall.Errno
	}{
		{"windows", syscall.Errno(2)},
		{"linux", windowsErrorAccessDenied},
		{"darwin", windowsErrorSharingViolation},
	} {
		installRoot, stageRoot := installRootFixture(t)
		calls := lockedRename(t, tc.goos, tc.errno, firstCalls(1000))
		if _, err := replaceInstallRootWithBackup(installRoot, stageRoot); !errors.Is(err, tc.errno) || strings.Contains(err.Error(), "run ha-nova update again") {
			t.Fatalf("%s errno %d: err = %v", tc.goos, tc.errno, err)
		}
		if *calls != 1 || installedVersion(t, installRoot) != "old" {
			t.Fatalf("%s errno %d: %d renames, installed %q", tc.goos, tc.errno, *calls, installedVersion(t, installRoot))
		}
	}
}

// When the new version can't be moved in, the old one goes back; when that
// fails too, the error names where both versions are and nothing is deleted.
func TestInstallRootSwapPutsTheOldVersionBack(t *testing.T) {
	installRoot, stageRoot := installRootFixture(t)
	lockedRename(t, "windows", windowsErrorSharingViolation, func(_ int, from string) bool {
		return strings.Contains(filepath.Base(from), installBackupPrefixNext)
	})
	if _, err := replaceInstallRootWithBackup(installRoot, stageRoot); !errors.Is(err, windowsErrorSharingViolation) || strings.Contains(err.Error(), "Nothing was changed") {
		t.Fatalf("err = %v", err)
	}
	if installedVersion(t, installRoot) != "old" || len(swapLeftovers(t, installRoot)) != 0 {
		t.Fatalf("installed %q, left %v", installedVersion(t, installRoot), swapLeftovers(t, installRoot))
	}

	installRoot, stageRoot = installRootFixture(t)
	lockedRename(t, "windows", windowsErrorSharingViolation, func(call int, _ string) bool { return call > 1 })
	_, err := replaceInstallRootWithBackup(installRoot, stageRoot)
	left := swapLeftovers(t, installRoot)
	if err == nil || !strings.Contains(err.Error(), "putting the previous version back failed too") || len(left) != 2 {
		t.Fatalf("err = %v, left %v", err, left)
	}
	for _, path := range left {
		if !strings.Contains(err.Error(), path) {
			t.Fatalf("error does not name %s: %v", path, err)
		}
	}
}

// A rollback retries its renames through the same bounded window.
func TestInstallRootRollbackRetriesAWindowsLock(t *testing.T) {
	installRoot, stageRoot := installRootFixture(t)
	replacement, err := replaceInstallRootWithBackup(installRoot, stageRoot)
	if err != nil {
		t.Fatal(err)
	}
	calls := lockedRename(t, "windows", windowsErrorAccessDenied, func(call int, _ string) bool { return call == 1 || call == 3 })
	binDir := filepath.Join(filepath.Dir(installRoot), "bin")
	paths := runtimePaths{InstallRoot: installRoot, BinDir: binDir, PublicBinary: filepath.Join(binDir, "ha-nova")}
	if err := replacement.rollback(paths); err != nil {
		t.Fatal(err)
	}
	if *calls != 4 || installedVersion(t, installRoot) != "old" {
		t.Fatalf("%d renames, installed %q", *calls, installedVersion(t, installRoot))
	}
}

// The helper's wait for the ha-nova.exe that started it is bounded and says
// when it runs out instead of going on silently.
func TestWaitForParentExitWarnsWhenTheParentOutlastsTheWait(t *testing.T) {
	originalTimeout, originalPoll, originalRunning := parentReleaseTimeout, parentReleasePoll, parentProcessRunning
	t.Cleanup(func() {
		parentReleaseTimeout, parentReleasePoll, parentProcessRunning = originalTimeout, originalPoll, originalRunning
	})
	parentReleaseTimeout, parentReleasePoll = time.Minute, time.Millisecond

	probes := 0
	parentProcessRunning = func(int) bool { probes++; return probes < 3 }
	if _, output := captureCommandOutput(t, func() int { waitForParentExit(4242); return 0 }); strings.Contains(output, "still runs") || probes != 3 {
		t.Fatalf("parent that exits: %d probes\n%s", probes, output)
	}

	parentReleaseTimeout = 20 * time.Millisecond
	parentProcessRunning = func(int) bool { return true }
	start := time.Now()
	_, output := captureCommandOutput(t, func() int { waitForParentExit(4242); return 0 })
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("wait took %s", elapsed)
	}
	if !strings.Contains(output, "process 4242") || !strings.Contains(output, "still runs") {
		t.Fatalf("no warning:\n%s", output)
	}
}
