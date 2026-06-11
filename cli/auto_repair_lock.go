package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A lock left behind by a crashed run must not block repair forever; anything
// older than this is treated as stale and stolen.
const autoRepairLockStaleAfter = 10 * time.Minute

// acquireAutoRepairLock serializes auto-repair runs across processes (two
// Claude Code sessions starting at once both fire the session-start hook).
// Returns a release func and whether the lock was acquired.
//
// Lock infrastructure problems fail open: a missing config dir or an
// unexpected filesystem error must not permanently disable repair, so those
// cases report acquired with a no-op release. Only a fresh lock held by
// another run reports not acquired.
func acquireAutoRepairLock(paths runtimePaths) (func(), bool) {
	dir := strings.TrimSpace(paths.ConfigDir)
	if dir == "" {
		return func() {}, true
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return func() {}, true
	}
	lockPath := filepath.Join(dir, "auto-repair.lock")
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			file.Close()
			return func() { os.Remove(lockPath) }, true
		}
		if !os.IsExist(err) {
			return func() {}, true
		}
		info, statErr := os.Stat(lockPath)
		if statErr != nil {
			if isNotExist(statErr) {
				continue
			}
			return func() {}, true
		}
		if time.Since(info.ModTime()) < autoRepairLockStaleAfter {
			return func() {}, false
		}
		os.Remove(lockPath)
	}
	return func() {}, false
}
