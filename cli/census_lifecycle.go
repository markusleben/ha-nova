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

// The uninstall marker lives beside (not inside) a persistent device/user-local
// root, so managed-directory and cache cleanup cannot remove it. It contains only an opaque
// random nonce: no user, consent, timestamp, process, or census data. A setup
// that started after that exact marker was written may
// remove it on successful completion; stale update/setup processes cannot.
func censusLifecycleMarkerPath(paths runtimePaths) string {
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
	return filepath.Join(filepath.Dir(base), ".ha-nova-census-uninstalled")
}

func censusLifecycleStopped(paths runtimePaths) bool {
	marker := censusLifecycleMarkerPath(paths)
	if marker == "" {
		return true
	}
	_, err := os.Stat(marker)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func captureCensusLifecycleMarker(paths runtimePaths) []byte {
	data, _ := readCensusLifecycleMarker(paths)
	return data
}

func readCensusLifecycleMarker(paths runtimePaths) ([]byte, error) {
	marker := censusLifecycleMarkerPath(paths)
	if marker == "" {
		return nil, fmt.Errorf("census lifecycle marker path unknown")
	}
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("census lifecycle marker is empty")
	}
	return data, nil
}

func installLifecycleGenerationPath(paths runtimePaths) string {
	marker := censusLifecycleMarkerPath(paths)
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

func markCensusLifecycleStopped(paths runtimePaths) error {
	marker := censusLifecycleMarkerPath(paths)
	if marker == "" {
		return fmt.Errorf("census lifecycle marker path unknown")
	}
	if err := rotateInstallLifecycleGeneration(paths); err != nil {
		return err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate census lifecycle nonce: %w", err)
	}
	value := fmt.Sprintf("%x", nonce[:])
	return writeJSONFile(marker, value, 0o600)
}

func reactivateCensusAfterSetup(paths runtimePaths, captured []byte) (bool, error) {
	if len(captured) == 0 {
		return false, nil
	}
	release, ok := acquireCensusLock(paths)
	if !ok {
		return false, fmt.Errorf("cannot acquire census lifecycle lock")
	}
	defer release()
	return reactivateCensusAfterSetupLocked(paths, captured)
}

func reactivateCensusAfterSetupLocked(paths runtimePaths, captured []byte) (bool, error) {
	if len(captured) == 0 {
		return false, nil
	}
	marker := censusLifecycleMarkerPath(paths)
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
