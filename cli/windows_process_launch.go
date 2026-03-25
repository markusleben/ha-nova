package main

import (
	"fmt"
	"os/exec"
	"strings"
)

type windowsProcessLaunchProfile struct {
	attachOutput          bool
	createNewProcessGroup bool
	createNoWindow        bool
	detached              bool
	hideWindow            bool
	inheritHandles        bool
}

func windowsHelperLaunchProfile() windowsProcessLaunchProfile {
	return windowsProcessLaunchProfile{
		attachOutput:          true,
		createNewProcessGroup: true,
		inheritHandles:        true,
	}
}

func windowsDetachedHelperLaunchProfile() windowsProcessLaunchProfile {
	return windowsProcessLaunchProfile{
		createNoWindow:        true,
		hideWindow:            true,
		inheritHandles:        true,
	}
}

func windowsBackgroundCleanupLaunchProfile() windowsProcessLaunchProfile {
	return windowsProcessLaunchProfile{
		createNoWindow: true,
		detached:       true,
		hideWindow:     true,
		inheritHandles: false,
	}
}

func windowsHiddenPowerShellLaunchProfile() windowsProcessLaunchProfile {
	return windowsProcessLaunchProfile{
		createNoWindow: true,
		hideWindow:     true,
		inheritHandles: true,
	}
}

func buildWindowsHelperCommand(helperPath string, args ...string) *exec.Cmd {
	return buildWindowsCommandWithProfile(helperPath, args, windowsHelperLaunchProfile())
}

func buildWindowsDetachedHelperCommand(helperPath string, statusPath string, statusTicks int64, args ...string) *exec.Cmd {
	command := fmt.Sprintf(
		`$statusPath = '%s'; $statusTicks = %d; $deadline = [DateTime]::UtcNow.AddSeconds(5); $p = Start-Process -FilePath '%s' -ArgumentList @(%s) -WindowStyle Hidden -PassThru -ErrorAction Stop; if ($null -eq $p) { throw 'failed to start detached helper' }; while ([DateTime]::UtcNow -lt $deadline) { if (Test-Path -LiteralPath $statusPath) { if ($statusTicks -lt 0) { exit 0 }; $item = Get-Item -LiteralPath $statusPath -ErrorAction Stop; if ($item.LastWriteTimeUtc.Ticks -gt $statusTicks) { exit 0 } }; $p.Refresh(); if ($p.HasExited) { throw 'detached helper exited before signaling readiness' }; Start-Sleep -Milliseconds 100 }; throw 'detached helper did not signal readiness'`,
		quotePowerShellSingleString(statusPath),
		statusTicks,
		quotePowerShellSingleString(helperPath),
		joinPowerShellStringArray(args),
	)
	return buildWindowsCommandWithProfile(
		"powershell.exe",
		[]string{
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			command,
		},
		windowsDetachedHelperLaunchProfile(),
	)
}

func launchWindowsDetachedHelper(helperPath string, statusPath string, statusTicks int64, args ...string) error {
	return buildWindowsDetachedHelperCommand(helperPath, statusPath, statusTicks, args...).Run()
}

func buildWindowsCleanupCommand(path string) *exec.Cmd {
	quotedPath := strings.ReplaceAll(path, `'`, `''`)
	return buildWindowsCommandWithProfile(
		"powershell.exe",
		[]string{
			"-NoProfile",
			"-WindowStyle",
			"Hidden",
			"-Command",
			fmt.Sprintf(`Start-Sleep -Seconds 2; Remove-Item -LiteralPath '%s' -Force -ErrorAction SilentlyContinue`, quotedPath),
		},
		windowsBackgroundCleanupLaunchProfile(),
	)
}

func buildWindowsHiddenPowerShellCommand(command string) *exec.Cmd {
	return buildWindowsCommandWithProfile(
		"powershell.exe",
		[]string{
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			command,
		},
		windowsHiddenPowerShellLaunchProfile(),
	)
}

func buildWindowsCommandWithProfile(name string, args []string, profile windowsProcessLaunchProfile) *exec.Cmd {
	cmd := exec.Command(name, args...)
	applyWindowsProcessLaunchProfile(cmd, profile)
	return cmd
}

func quotePowerShellSingleString(value string) string {
	return strings.ReplaceAll(value, `'`, `''`)
}

func joinPowerShellStringArray(values []string) string {
	if len(values) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf(`'%s'`, quotePowerShellSingleString(value)))
	}
	return strings.Join(quoted, ", ")
}
