package main

import (
	"errors"
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
	if profile.createNoWindow {
		t.Fatalf("expected helper launch profile to keep the console window visible")
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
	if !profile.createNoWindow {
		t.Fatalf("expected cleanup launch profile to avoid creating a console window")
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
	postUpdateSyncForReplace = func(paths runtimePaths) error {
		state := loadStateOrDefault(paths)
		state.ClientsVerifiedVersion = localVersion(paths)
		return saveState(paths, state)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runInternalReplace(paths, []string{"--parent-pid", "0", "--stage-root", t.TempDir()})
	})
	if exitCode != 0 {
		t.Fatalf("runInternalReplace() exit = %d\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Updated to v") {
		t.Fatalf("expected final update success output:\n%s", output)
	}
	if !strings.HasSuffix(strings.TrimSpace(output), postUpdateSessionInstruction) {
		t.Fatalf("expected final new-session instruction:\n%s", output)
	}
}

func TestRunInternalReplaceOmitsSessionInstructionWhenClientSyncFails(t *testing.T) {
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
	syncCalls := 0
	postUpdateSyncForReplace = func(paths runtimePaths) error {
		syncCalls++
		if syncCalls == 1 {
			return errors.New("sync failed")
		}
		return nil
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runInternalReplace(paths, []string{"--parent-pid", "0", "--stage-root", t.TempDir()})
	})
	if exitCode == 0 {
		t.Fatalf("runInternalReplace() exit = 0, want failure\n%s", output)
	}
	if strings.Contains(output, postUpdateSessionInstruction) {
		t.Fatalf("did not expect new-session instruction after rollback:\n%s", output)
	}
}

func TestBuildWindowsHelperCommandUsesParentOutputStreamsOnAllPlatforms(t *testing.T) {
	cmd := buildWindowsHelperCommand("helper.exe", "internal-uninstall")
	if cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
		t.Fatalf("expected helper command to wire parent output streams on every platform")
	}
}

func TestWindowsDetachedHelperLaunchProfileKeepsWrapperHidden(t *testing.T) {
	profile := windowsDetachedHelperLaunchProfile()
	if profile.attachOutput {
		t.Fatalf("expected detached helper launch profile to avoid parent output streams")
	}
	if !profile.createNoWindow {
		t.Fatalf("expected detached helper launch profile to avoid creating a console window")
	}
	if !profile.hideWindow {
		t.Fatalf("expected detached helper launch profile to hide the helper window")
	}
	if !profile.inheritHandles {
		t.Fatalf("expected detached helper launch profile to keep hidden wrapper pipes available")
	}
}

func TestBuildWindowsDetachedHelperCommandStartsHiddenHelperViaWrapper(t *testing.T) {
	cmd := buildWindowsDetachedHelperCommand("helper.exe", `C:\Users\markus\AppData\Local\ha-nova\uninstall-status.json`, 1234, "internal-uninstall")
	if cmd.Stdout != nil || cmd.Stderr != nil {
		t.Fatalf("expected detached helper command to avoid parent output streams")
	}
	if got, want := strings.Join(cmd.Args, " "), `powershell.exe -NoProfile -NonInteractive -Command $statusPath = 'C:\Users\markus\AppData\Local\ha-nova\uninstall-status.json'; $statusTicks = 1234; $baselineTicks = $statusTicks; if (Test-Path -LiteralPath $statusPath) { $baselineTicks = (Get-Item -LiteralPath $statusPath -ErrorAction Stop).LastWriteTimeUtc.Ticks }; $deadline = [DateTime]::UtcNow.AddSeconds(5); $p = Start-Process -FilePath 'helper.exe' -ArgumentList @('internal-uninstall') -WindowStyle Hidden -PassThru -ErrorAction Stop; if ($null -eq $p) { throw 'failed to start detached helper' }; while ([DateTime]::UtcNow -lt $deadline) { if (Test-Path -LiteralPath $statusPath) { if ($baselineTicks -lt 0) { exit 0 }; $item = Get-Item -LiteralPath $statusPath -ErrorAction Stop; if ($item.LastWriteTimeUtc.Ticks -gt $baselineTicks) { exit 0 } }; $p.Refresh(); if ($p.HasExited) { throw 'detached helper exited before signaling readiness' }; Start-Sleep -Milliseconds 100 }; throw 'detached helper did not signal readiness'`; got != want {
		t.Fatalf("detached helper args = %q, want %q", got, want)
	}
}

func TestBuildWindowsDetachedHelperCommandPropagatesInstallRootEnv(t *testing.T) {
	cmd := buildWindowsDetachedHelperCommandWithEnv(
		"helper.exe",
		`C:\Users\markus\AppData\Local\ha-nova\uninstall-status.json`,
		1234,
		[]string{
			`HA_NOVA_ALLOW_INSTALL_ROOT_OVERRIDE=1`,
			`HA_NOVA_INSTALL_ROOT=C:\Users\markus\.local\share\ha-nova`,
		},
		"internal-uninstall",
	)
	allowWant := `$env:HA_NOVA_ALLOW_INSTALL_ROOT_OVERRIDE = '1'; `
	if !strings.Contains(strings.Join(cmd.Args, " "), allowWant) {
		t.Fatalf("expected detached helper command to set install-root override allow env, got %q", strings.Join(cmd.Args, " "))
	}
	want := `$env:HA_NOVA_INSTALL_ROOT = 'C:\Users\markus\.local\share\ha-nova'; `
	if !strings.Contains(strings.Join(cmd.Args, " "), want) {
		t.Fatalf("expected detached helper command to set install-root env, got %q", strings.Join(cmd.Args, " "))
	}
}

func TestHelperInstallRootEnvExportsCallerInstallRoot(t *testing.T) {
	got := helperInstallRootEnv(`C:\Users\markus\.local\share\ha-nova`)
	want := []string{
		`HA_NOVA_ALLOW_INSTALL_ROOT_OVERRIDE=1`,
		`HA_NOVA_INSTALL_ROOT=C:\Users\markus\.local\share\ha-nova`,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("helperInstallRootEnv() = %q, want %q", got, want)
	}
}

func TestWindowsHiddenPowerShellLaunchProfileKeepsPipesButHidesWindow(t *testing.T) {
	profile := windowsHiddenPowerShellLaunchProfile()
	if profile.attachOutput {
		t.Fatalf("expected hidden PowerShell profile to avoid parent output streams")
	}
	if !profile.createNoWindow {
		t.Fatalf("expected hidden PowerShell profile to avoid creating a console window")
	}
	if !profile.hideWindow {
		t.Fatalf("expected hidden PowerShell profile to hide the window")
	}
	if !profile.inheritHandles {
		t.Fatalf("expected hidden PowerShell profile to keep pipe handles available")
	}
}

func TestBuildWindowsHiddenPowerShellCommandAvoidsVisibleConsoleArgs(t *testing.T) {
	cmd := buildWindowsHiddenPowerShellCommand(`Get-Process -Id 123`)
	if got, want := strings.Join(cmd.Args, " "), `powershell.exe -NoProfile -NonInteractive -Command Get-Process -Id 123`; got != want {
		t.Fatalf("hidden powershell args = %q, want %q", got, want)
	}
	if cmd.Stdout != nil || cmd.Stderr != nil {
		t.Fatalf("expected hidden PowerShell command to leave output wiring to the caller")
	}
}

func TestJoinPowerShellStringArrayQuotesArguments(t *testing.T) {
	got := joinPowerShellStringArray([]string{"internal-uninstall", "--self-path", `C:\Temp\ha'nova.exe`})
	want := `'internal-uninstall', '--self-path', 'C:\Temp\ha''nova.exe'`
	if got != want {
		t.Fatalf("joinPowerShellStringArray() = %q, want %q", got, want)
	}
}
