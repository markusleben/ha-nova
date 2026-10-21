package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const uninstallMarkerName = ".ha-nova-uninstalled"

// The uninstall marker lives beside (not inside) a persistent device/user-local
// root, so managed-directory and cache cleanup cannot remove it. It contains
// only an opaque random nonce: no user, timestamp, or process data. A setup
// that started after that exact marker was written may remove it on successful
// completion; stale update/setup processes cannot.
func uninstallMarkerPath(paths runtimePaths) string {
	// Cache roots are disposable and cannot uphold an uninstall barrier. Keep
	// the marker beside the managed config root on Unix, and beside the
	// device-local data root on Windows (never roaming APPDATA).
	base := paths.ConfigDir
	if runtime.GOOS == "windows" {
		base = paths.LocalDataDir
	}
	if base == "" {
		if paths.Home == "" {
			return ""
		}
		if runtime.GOOS == "windows" {
			base = filepath.Join(paths.Home, "AppData", "Local", "ha-nova")
		} else {
			base = filepath.Join(paths.Home, ".config", "ha-nova")
		}
	}
	return filepath.Join(filepath.Dir(base), uninstallMarkerName)
}

func installLifecycleStopped(paths runtimePaths) bool {
	marker := uninstallMarkerPath(paths)
	if marker == "" {
		return true
	}
	_, err := os.Stat(marker)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func captureUninstallMarker(paths runtimePaths) []byte {
	data, _ := readUninstallMarker(paths)
	return data
}

func readUninstallMarker(paths runtimePaths) ([]byte, error) {
	marker := uninstallMarkerPath(paths)
	if marker == "" {
		return nil, fmt.Errorf("uninstall marker path unknown")
	}
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("uninstall marker is empty")
	}
	return data, nil
}

func installLifecycleGenerationPath(paths runtimePaths) string {
	marker := uninstallMarkerPath(paths)
	if marker == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(marker), ".ha-nova-install-generation")
}

func captureInstallLifecycleGeneration(paths runtimePaths) []byte {
	data, _ := readInstallLifecycleGeneration(paths)
	return data
}

func readInstallLifecycleGeneration(paths runtimePaths) ([]byte, error) {
	path := installLifecycleGenerationPath(paths)
	if path == "" {
		return nil, fmt.Errorf("install lifecycle generation path unknown")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("install lifecycle generation is empty")
	}
	return data, nil
}

func rotateInstallLifecycleGeneration(paths runtimePaths) error {
	path := installLifecycleGenerationPath(paths)
	if path == "" {
		return fmt.Errorf("install lifecycle generation path unknown")
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate install lifecycle nonce: %w", err)
	}
	return writeJSONFile(path, fmt.Sprintf("%x", nonce[:]), 0o600)
}

func markInstallLifecycleStopped(paths runtimePaths) error {
	marker := uninstallMarkerPath(paths)
	if marker == "" {
		return fmt.Errorf("uninstall marker path unknown")
	}
	if err := rotateInstallLifecycleGeneration(paths); err != nil {
		return err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate uninstall marker nonce: %w", err)
	}
	value := fmt.Sprintf("%x", nonce[:])
	if err := writeJSONFile(marker, value, 0o600); err != nil {
		return err
	}
	// Census-era binaries still running across the upgrade check only the
	// legacy path; give them the same barrier. Best effort. Keep this mirror
	// until the release after next (v0.25.1 processes can still be running
	// during an upgrade-then-uninstall); then stop writing it and keep only
	// the read/cleanup paths. uninstallMarkerStale relies on this mirror too:
	// retiring it needs a content-based staleness check there first.
	_ = writeJSONFile(legacyUninstallMarkerPath(paths), value, 0o600)
	return nil
}

func clearUninstallMarkerAfterSetup(paths runtimePaths, captured []byte) (bool, error) {
	// A completed setup supersedes any uninstall recorded by an older release;
	// only now is the legacy marker dropped.
	_ = os.Remove(legacyUninstallMarkerPath(paths))
	if len(captured) == 0 {
		return false, nil
	}
	marker := uninstallMarkerPath(paths)
	current, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !bytes.Equal(current, captured) {
		return false, nil
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}

// clearStaleUninstallMarker drops an uninstall marker that a later completed
// setup already superseded. A release older than this marker (0.25.1 knows
// only the Census-era one) can be reinstalled and set up after an uninstall:
// its setup clears its own marker and rotates the shared generation, but
// leaves this one, which then blocks a working, paired install.
//
// Stale means: the Census-era marker is gone, a config exists, and both the
// generation and the install state were written after the marker. Uninstall
// rotates the generation before it writes the marker, under the same lock
// taken here, so a live marker is never older than the generation; and only
// a setup that started after the uninstall can rotate it again (a process
// from before holds the old generation and fails closed). Equal or unknown
// times keep the marker.
func clearStaleUninstallMarker(paths runtimePaths) {
	if !uninstallMarkerStale(paths) { // the usual case: no lock taken
		return
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		return
	}
	defer release()
	if uninstallMarkerStale(paths) {
		_ = os.Remove(uninstallMarkerPath(paths))
	}
}

func uninstallMarkerStale(paths runtimePaths) bool {
	path := uninstallMarkerPath(paths)
	if path == "" {
		return false
	}
	marker, err := os.Lstat(path)
	if err != nil || !marker.Mode().IsRegular() {
		return false
	}
	if _, err := os.Lstat(legacyUninstallMarkerPath(paths)); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	if paths.ConfigFile == "" || paths.StateFile == "" {
		return false
	}
	if _, err := os.Stat(paths.ConfigFile); err != nil {
		return false
	}
	for _, newer := range []string{installLifecycleGenerationPath(paths), paths.StateFile} {
		info, err := os.Stat(newer)
		if err != nil || !info.ModTime().After(marker.ModTime()) {
			return false
		}
	}
	return true
}

// removeLegacyCensusState deletes local state left by releases that shipped
// the opt-in Census. Best effort and silent: nothing is sent anywhere. The old
// uninstall marker carries the same opaque nonce semantics: it is copied, not
// moved, so Census-era processes still running keep seeing the barrier until
// a setup completes. census.lock stays: deleting a lock file another process
// may hold would break that process's locking.
func removeLegacyCensusState(paths runtimePaths) {
	for _, dir := range []string{paths.ConfigDir, paths.LocalDataDir} {
		if dir != "" {
			_ = os.Remove(filepath.Join(dir, "census.json"))
		}
	}
	if paths.CacheDir != "" {
		markers, _ := filepath.Glob(filepath.Join(paths.CacheDir, "census-*"))
		for _, marker := range markers {
			_ = os.Remove(marker)
		}
	}
	marker := uninstallMarkerPath(paths)
	if marker == "" {
		return
	}
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if value, err := os.ReadFile(legacyUninstallMarkerPath(paths)); err == nil {
		_ = writeFileAtomic(marker, value, 0o600)
	}
}

// legacyUninstallMarkerPath is where releases with the opt-in Census kept the
// uninstall barrier.
func legacyUninstallMarkerPath(paths runtimePaths) string {
	return filepath.Join(filepath.Dir(uninstallMarkerPath(paths)), ".ha-nova-census-uninstalled")
}
