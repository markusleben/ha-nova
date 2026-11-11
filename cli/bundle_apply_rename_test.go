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

// lockedRename stands in for os.Rename on goos: the first failures calls
// fail with errno, wrapped as os.Rename wraps it, then the real rename runs.
func lockedRename(t *testing.T, goos string, errno syscall.Errno, failures int) *int {
	t.Helper()
	originalOS, originalRename, originalAttempts, originalDelay := installRenamePlatformOS, renameInstallPath, installRenameAttempts, installRenameDelay
	t.Cleanup(func() {
		installRenamePlatformOS, renameInstallPath, installRenameAttempts, installRenameDelay = originalOS, originalRename, originalAttempts, originalDelay
	})
	installRenamePlatformOS, installRenameAttempts, installRenameDelay = goos, 5, time.Millisecond
	calls := 0
	renameInstallPath = func(from, to string) error {
		calls++
		if calls <= failures {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: errno}
		}
		return os.Rename(from, to)
	}
	return &calls
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

// A Windows lock on the install root (access denied, sharing or lock
// violation) that passes within the window is retried, and the update lands.
func TestInstallRootRenameRetriesAWindowsLockThatPasses(t *testing.T) {
	for _, errno := range []syscall.Errno{windowsErrorAccessDenied, windowsErrorSharingViolation, windowsErrorLockViolation} {
		installRoot, stageRoot := installRootFixture(t)
		calls := lockedRename(t, "windows", errno, 3)
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
	calls := lockedRename(t, "windows", windowsErrorAccessDenied, 1000)
	_, err := replaceInstallRootWithBackup(installRoot, stageRoot)
	if err == nil || !errors.Is(err, windowsErrorAccessDenied) || !strings.Contains(err.Error(), "run ha-nova update again") {
		t.Fatalf("err = %v", err)
	}
	if *calls != 5 || installedVersion(t, installRoot) != "old" {
		t.Fatalf("%d renames, installed %q", *calls, installedVersion(t, installRoot))
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(installRoot), installBackupPrefixNext+"*"))
	if len(leftovers) != 0 {
		t.Fatalf("next root left behind: %v", leftovers)
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
		calls := lockedRename(t, tc.goos, tc.errno, 1000)
		if _, err := replaceInstallRootWithBackup(installRoot, stageRoot); !errors.Is(err, tc.errno) || strings.Contains(err.Error(), "run ha-nova update again") {
			t.Fatalf("%s errno %d: err = %v", tc.goos, tc.errno, err)
		}
		if *calls != 1 || installedVersion(t, installRoot) != "old" {
			t.Fatalf("%s errno %d: %d renames, installed %q", tc.goos, tc.errno, *calls, installedVersion(t, installRoot))
		}
	}
}

// The helper's wait for the ha-nova.exe that started it is bounded and says
// when it runs out instead of going on silently.
func TestWaitForParentExitWarnsWhenTheParentOutlastsTheWait(t *testing.T) {
	originalTimeout, originalPoll, originalRunning := parentReleaseTimeout, parentReleasePoll, parentProcessRunning
	t.Cleanup(func() {
		parentReleaseTimeout, parentReleasePoll, parentProcessRunning = originalTimeout, originalPoll, originalRunning
	})
	parentReleaseTimeout, parentReleasePoll = 20*time.Millisecond, time.Millisecond

	probes := 0
	parentProcessRunning = func(int) bool { probes++; return probes < 3 }
	if _, output := captureCommandOutput(t, func() int { waitForParentExit(4242); return 0 }); strings.Contains(output, "still running") || probes != 3 {
		t.Fatalf("parent that exits: %d probes\n%s", probes, output)
	}

	parentProcessRunning = func(int) bool { return true }
	start := time.Now()
	_, output := captureCommandOutput(t, func() int { waitForParentExit(4242); return 0 })
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("wait took %s", elapsed)
	}
	if !strings.Contains(output, "process 4242") || !strings.Contains(output, "still running") {
		t.Fatalf("no warning:\n%s", output)
	}
}
