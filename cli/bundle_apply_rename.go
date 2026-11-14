package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
)

// Windows refuses to rename a folder while any process holds a file in it
// open: the ha-nova.exe that started the update (still exiting), a virus
// scanner, or the search indexer reading the files just copied. Those locks
// pass within seconds, so the install-root renames retry them for a bounded
// time; any other error, or a lock that outlasts the window, fails at once
// and the install stays as it was (MAR-161).
const (
	windowsErrorAccessDenied     = syscall.Errno(5)
	windowsErrorSharingViolation = syscall.Errno(32)
	windowsErrorLockViolation    = syscall.Errno(33)
)

var (
	installRenamePlatformOS = runtime.GOOS
	installRenameAttempts   = 40
	installRenameDelay      = 250 * time.Millisecond
	renameInstallPath       = os.Rename
)

func windowsFolderInUse(err error) bool {
	var errno syscall.Errno
	return installRenamePlatformOS == "windows" && errors.As(err, &errno) &&
		(errno == windowsErrorAccessDenied || errno == windowsErrorSharingViolation || errno == windowsErrorLockViolation)
}

func renameInstallPathWithRetry(from, to string) error {
	err := renameInstallPath(from, to)
	for attempt := 1; err != nil && windowsFolderInUse(err) && attempt < installRenameAttempts; attempt++ {
		time.Sleep(installRenameDelay)
		err = renameInstallPath(from, to)
	}
	return err
}

// installRootInUse words the first swap step's failure: nothing was moved yet.
func installRootInUse(installRoot string, err error) error {
	if !windowsFolderInUse(err) {
		return err
	}
	return fmt.Errorf("Windows refused to move %s for %s, usually because a file in it is still open (another HA NOVA window, a virus scan, or the search indexer). Nothing was changed. Close other HA NOVA windows and run ha-nova update again: %w",
		installRoot, time.Duration(installRenameAttempts-1)*installRenameDelay, err)
}
