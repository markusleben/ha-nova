package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var autoRepairProcessLock = make(chan struct{}, 1)
var acquireAutoRepairPlatformLockForMutation = acquireAutoRepairPlatformLock

const legacyAutoRepairLockStaleAfter = 10 * time.Minute

// acquireAutoRepairLock serializes auto-repair runs across processes (two
// Claude Code sessions starting at once both fire the session-start hook).
// Returns a release func and whether the lock was acquired.
//
// Lock infrastructure problems fail closed. Client installers replace whole
// trees, so an unlocked repair is less safe than deferring to the next run.
func acquireAutoRepairLock(paths runtimePaths) (func(), bool) {
	return acquireAutoRepairLockWithFinalizer(paths, nil)
}

func acquireAutoRepairLockWithFinalizer(paths runtimePaths, finalizer func()) (func(), bool) {
	dir := strings.TrimSpace(paths.ConfigDir)
	if dir == "" && strings.TrimSpace(paths.StateFile) != "" {
		dir = filepath.Dir(paths.StateFile)
	}
	if dir == "" {
		return func() {}, false
	}
	select {
	case autoRepairProcessLock <- struct{}{}:
	default:
		return func() {}, false
	}
	releaseProcess := func() { <-autoRepairProcessLock }
	createdConfigDir := false
	cleanupCreatedConfigDir := func() {
		if createdConfigDir {
			_ = os.Remove(dir)
		}
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(dir)), 0o755); err != nil {
		releaseProcess()
		return func() {}, false
	}
	releasePlatform, acquired := acquireAutoRepairPlatformLockForMutation(dir)
	if !acquired {
		releaseProcess()
		return func() {}, false
	}
	if err := os.Mkdir(dir, 0o755); err == nil {
		createdConfigDir = true
	} else if !errors.Is(err, os.ErrExist) {
		releasePlatform()
		releaseProcess()
		return func() {}, false
	}
	lockPath := filepath.Join(dir, "auto-repair.lock")
	for attempt := 0; attempt < 2; attempt++ {
		err := os.Mkdir(lockPath, 0o700)
		if err == nil {
			ownerPath := filepath.Join(lockPath, "v2")
			if err := os.WriteFile(ownerPath, []byte("locked\n"), 0o600); err != nil {
				_ = os.Remove(ownerPath)
				_ = os.Remove(lockPath)
				cleanupCreatedConfigDir()
				releasePlatform()
				releaseProcess()
				return func() {}, false
			}
			return func() {
				_ = os.Remove(ownerPath)
				_ = os.Remove(lockPath)
				if finalizer != nil {
					finalizer()
				}
				cleanupCreatedConfigDir()
				releasePlatform()
				releaseProcess()
			}, true
		}
		if !os.IsExist(err) {
			cleanupCreatedConfigDir()
			releasePlatform()
			releaseProcess()
			return func() {}, false
		}
		info, statErr := os.Lstat(lockPath)
		if statErr != nil {
			cleanupCreatedConfigDir()
			releasePlatform()
			releaseProcess()
			return func() {}, false
		}
		if !info.IsDir() {
			elapsed := time.Since(info.ModTime())
			if elapsed < legacyAutoRepairLockStaleAfter {
				cleanupCreatedConfigDir()
				releasePlatform()
				releaseProcess()
				return func() {}, false
			}
			if err := os.Remove(lockPath); err != nil {
				cleanupCreatedConfigDir()
				releasePlatform()
				releaseProcess()
				return func() {}, false
			}
			continue
		}
		// The platform lock proves that no current binary owns a recognized v2
		// directory. Empty means a crash between mkdir and marker creation.
		entries, readErr := os.ReadDir(lockPath)
		if readErr != nil || len(entries) > 1 {
			cleanupCreatedConfigDir()
			releasePlatform()
			releaseProcess()
			return func() {}, false
		}
		if len(entries) == 1 {
			owner := entries[0]
			ownerPath := filepath.Join(lockPath, owner.Name())
			ownerInfo, ownerStatErr := os.Lstat(ownerPath)
			if ownerStatErr != nil || owner.Name() != "v2" || !ownerInfo.Mode().IsRegular() {
				cleanupCreatedConfigDir()
				releasePlatform()
				releaseProcess()
				return func() {}, false
			}
			data, ownerErr := os.ReadFile(ownerPath)
			if ownerErr != nil || string(data) != "locked\n" {
				cleanupCreatedConfigDir()
				releasePlatform()
				releaseProcess()
				return func() {}, false
			}
			if err := os.Remove(filepath.Join(lockPath, "v2")); err != nil {
				cleanupCreatedConfigDir()
				releasePlatform()
				releaseProcess()
				return func() {}, false
			}
		}
		if err := os.Remove(lockPath); err != nil {
			cleanupCreatedConfigDir()
			releasePlatform()
			releaseProcess()
			return func() {}, false
		}
	}
	cleanupCreatedConfigDir()
	releasePlatform()
	releaseProcess()
	return func() {}, false
}

func acquireAutoRepairLockUntil(paths runtimePaths, deadline time.Time) (func(), bool) {
	for {
		if release, acquired := acquireAutoRepairLock(paths); acquired {
			return release, true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return func() {}, false
		}
		pause := 10 * time.Millisecond
		if pause > remaining {
			pause = remaining
		}
		time.Sleep(pause)
	}
}
