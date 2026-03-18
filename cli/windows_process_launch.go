package main

import (
	"fmt"
	"os/exec"
	"strings"
)

type windowsProcessLaunchProfile struct {
	attachOutput          bool
	createNewProcessGroup bool
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

func windowsBackgroundCleanupLaunchProfile() windowsProcessLaunchProfile {
	return windowsProcessLaunchProfile{
		detached:       true,
		hideWindow:     true,
		inheritHandles: false,
	}
}

func buildWindowsHelperCommand(helperPath string, args ...string) *exec.Cmd {
	return buildWindowsCommandWithProfile(helperPath, args, windowsHelperLaunchProfile())
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

func buildWindowsCommandWithProfile(name string, args []string, profile windowsProcessLaunchProfile) *exec.Cmd {
	cmd := exec.Command(name, args...)
	applyWindowsProcessLaunchProfile(cmd, profile)
	return cmd
}
