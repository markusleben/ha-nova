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
	return writeJSONFile(marker, value, 0o600)
}

func clearUninstallMarkerAfterSetup(paths runtimePaths, captured []byte) (bool, error) {
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

// removeLegacyCensusState deletes local state left by releases that shipped
// the opt-in Census. Best effort and silent: nothing is sent anywhere. The old
// uninstall marker carried the same opaque nonce semantics, so it is renamed
// rather than dropped and keeps blocking stale processes after an uninstall.
func removeLegacyCensusState(paths runtimePaths) {
	for _, dir := range []string{paths.ConfigDir, paths.LocalDataDir} {
		if dir != "" {
			_ = os.Remove(filepath.Join(dir, "census.json"))
			_ = os.Remove(filepath.Join(dir, "census.lock"))
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
	legacy := filepath.Join(filepath.Dir(marker), ".ha-nova-census-uninstalled")
	if _, err := os.Stat(marker); err == nil {
		_ = os.Remove(legacy)
		return
	}
	_ = os.Rename(legacy, marker)
}
