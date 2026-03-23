package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectInstallSourcePrefersLiveBundleRuntimeOverStaleWingetState(t *testing.T) {
	home := t.TempDir()
	bundleRoot := windowsBundleInstallRoot(home)
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		t.Fatalf("mkdir bundle root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, publicBinaryName()), []byte("bundle"), 0o755); err != nil {
		t.Fatalf("write bundle binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "bundle.json"), []byte(`{"version":"0.3.0"}`), 0o644); err != nil {
		t.Fatalf("write bundle metadata: %v", err)
	}

	originalPlatform := channelChecksUseWindowsPlatform
	originalExec := executablePathForInstallSource
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
		executablePathForInstallSource = originalExec
	}()
	channelChecksUseWindowsPlatform = func() bool { return true }
	executablePathForInstallSource = func() (string, error) {
		return filepath.Join(bundleRoot, publicBinaryName()), nil
	}

	paths := runtimePaths{
		Home:        home,
		InstallRoot: bundleRoot,
	}
	got := detectInstallSource(paths, installState{InstallSource: installSourceWinget})
	if got != installSourceBundle {
		t.Fatalf("detectInstallSource() = %q, want %q", got, installSourceBundle)
	}
}

func TestDetectInstallSourceIgnoresStaleWingetStateWithoutLiveWingetInstall(t *testing.T) {
	home := t.TempDir()

	originalPlatform := channelChecksUseWindowsPlatform
	originalExec := executablePathForInstallSource
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
		executablePathForInstallSource = originalExec
	}()
	channelChecksUseWindowsPlatform = func() bool { return true }
	executablePathForInstallSource = func() (string, error) {
		return filepath.Join(windowsBundleInstallRoot(home), publicBinaryName()), nil
	}

	paths := runtimePaths{
		Home:        home,
		InstallRoot: windowsBundleInstallRoot(home),
	}
	got := detectInstallSource(paths, installState{InstallSource: installSourceWinget})
	if got != installSourceBundle {
		t.Fatalf("detectInstallSource() = %q, want %q", got, installSourceBundle)
	}
}
