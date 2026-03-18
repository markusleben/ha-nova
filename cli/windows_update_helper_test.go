package main

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsHelperLaunchProfileKeepsConsoleOutput(t *testing.T) {
	profile := windowsHelperLaunchProfile()
	if !profile.attachOutput {
		t.Fatalf("expected helper launch profile to keep parent output attached")
	}
	if !profile.createNewProcessGroup {
		t.Fatalf("expected helper launch profile to create a new process group")
	}
	if profile.detached {
		t.Fatalf("expected helper launch profile to stay attached to the console for final status output")
	}
	if !profile.inheritHandles {
		t.Fatalf("expected helper launch profile to inherit stdio handles")
	}
	if profile.hideWindow {
		t.Fatalf("expected helper launch profile to avoid hidden-window mode")
	}
}

func TestWindowsBackgroundCleanupLaunchProfileDetachesFromConsole(t *testing.T) {
	profile := windowsBackgroundCleanupLaunchProfile()
	if profile.attachOutput {
		t.Fatalf("expected cleanup launch profile to avoid parent output streams")
	}
	if !profile.detached {
		t.Fatalf("expected cleanup launch profile to run detached")
	}
	if !profile.hideWindow {
		t.Fatalf("expected cleanup launch profile to hide the cleanup window")
	}
	if profile.inheritHandles {
		t.Fatalf("expected cleanup launch profile to avoid inheriting handles")
	}
}

func TestBuildWindowsHelperCommandMirrorsParentOutputStreams(t *testing.T) {
	cmd := buildWindowsHelperCommand("C:\\Temp\\ha-nova-updater.exe", "internal-replace", "--parent-pid", "123", "--stage-root", "C:\\Temp\\stage")
	if cmd.Stdout != os.Stdout {
		t.Fatalf("expected helper stdout to mirror parent stdout")
	}
	if cmd.Stderr != os.Stderr {
		t.Fatalf("expected helper stderr to mirror parent stderr")
	}
	if got, want := strings.Join(cmd.Args, " "), `C:\Temp\ha-nova-updater.exe internal-replace --parent-pid 123 --stage-root C:\Temp\stage`; got != want {
		t.Fatalf("command args = %q, want %q", got, want)
	}
}

func TestBuildWindowsCleanupCommandStaysDetached(t *testing.T) {
	cmd := buildWindowsCleanupCommand(`C:\Temp\ha-nova-uninstall.exe`)
	if cmd.Stdout != nil || cmd.Stderr != nil {
		t.Fatalf("expected cleanup command to avoid parent output streams")
	}
	if got, want := strings.Join(cmd.Args, " "), `powershell.exe -NoProfile -WindowStyle Hidden -Command Start-Sleep -Seconds 2; Remove-Item -LiteralPath 'C:\Temp\ha-nova-uninstall.exe' -Force -ErrorAction SilentlyContinue`; got != want {
		t.Fatalf("cleanup args = %q, want %q", got, want)
	}
}

func TestRunInternalReplacePrintsFinalSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalWait := waitForParentReleaseForReplace
	originalApply := applyStagedBundleWithRollbackForReplace
	originalSync := postUpdateSyncForReplace
	defer func() {
		waitForParentReleaseForReplace = originalWait
		applyStagedBundleWithRollbackForReplace = originalApply
		postUpdateSyncForReplace = originalSync
	}()
	waitForParentReleaseForReplace = func(parentPID int) {}
	applyStagedBundleWithRollbackForReplace = func(paths runtimePaths, stageRoot string) (func() error, func() error, error) {
		return func() error { return nil }, func() error { return nil }, nil
	}
	postUpdateSyncForReplace = func(paths runtimePaths) error { return nil }

	exitCode, output := captureCommandOutput(t, func() int {
		return runInternalReplace(paths, []string{"--parent-pid", "0", "--stage-root", t.TempDir()})
	})
	if exitCode != 0 {
		t.Fatalf("runInternalReplace() exit = %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Updated to v") {
		t.Fatalf("expected final update success output:\n%s", output)
	}
}

func TestBuildWindowsHelperCommandUsesParentOutputStreamsOnAllPlatforms(t *testing.T) {
	cmd := buildWindowsHelperCommand("helper.exe", "internal-uninstall")
	if cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
		t.Fatalf("expected helper command to wire parent output streams on every platform")
	}
}
