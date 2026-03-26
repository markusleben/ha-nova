package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectInstallSourceKeepsLegacyWindowsPackageStateDetectable(t *testing.T) {
	originalPlatform := channelChecksUseWindowsPlatform
	originalExecutable := executablePathForInstallSource
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
		executablePathForInstallSource = originalExecutable
	}()

	channelChecksUseWindowsPlatform = func() bool { return true }
	executablePathForInstallSource = func() (string, error) {
		return filepath.Join(t.TempDir(), "ha-nova.exe"), nil
	}

	paths := runtimePaths{
		Home:        t.TempDir(),
		InstallRoot: filepath.Join(t.TempDir(), "Programs", "ha-nova"),
	}
	state := installState{InstallSource: "winget"}

	if got := detectInstallSource(paths, state); got != installSourceLegacyWindowsPackage {
		t.Fatalf("detectInstallSource() = %q, want %q", got, installSourceLegacyWindowsPackage)
	}
}

func TestRunUpdateGuidesLegacyWindowsPackageReinstall(t *testing.T) {
	originalPlatform := channelChecksUseWindowsPlatform
	originalExecutable := executablePathForInstallSource
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
		executablePathForInstallSource = originalExecutable
	}()

	channelChecksUseWindowsPlatform = func() bool { return true }
	executablePathForInstallSource = func() (string, error) {
		return filepath.Join(t.TempDir(), "ha-nova.exe"), nil
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	state := installState{InstallSource: "winget"}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUpdate(paths, nil)
	})
	if exitCode != 1 {
		t.Fatalf("runUpdate() exit = %d, want 1\n%s", exitCode, output)
	}
	for _, want := range []string{
		"Legacy private/test Windows package installs are no longer supported for in-place update.",
		"Installed Apps / App Installer",
		"install.ps1 | iex",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected update output %q:\n%s", want, output)
		}
	}
}

func TestRunUninstallGuidesLegacyWindowsPackageRemoval(t *testing.T) {
	originalPlatform := channelChecksUseWindowsPlatform
	originalExecutable := executablePathForInstallSource
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
		executablePathForInstallSource = originalExecutable
	}()

	channelChecksUseWindowsPlatform = func() bool { return true }
	executablePathForInstallSource = func() (string, error) {
		return filepath.Join(t.TempDir(), "ha-nova.exe"), nil
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	state := installState{InstallSource: "winget"}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runUninstall(paths, []string{"--yes"})
	})
	if exitCode != 1 {
		t.Fatalf("runUninstall() exit = %d, want 1\n%s", exitCode, output)
	}
	for _, want := range []string{
		"Legacy Windows package install (remove via Installed Apps / App Installer)",
		"Legacy private/test Windows package installs are no longer supported for in-place `ha-nova uninstall`.",
		"Remove the old HA NOVA app in Installed Apps / App Installer.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected uninstall output %q:\n%s", want, output)
		}
	}
}
