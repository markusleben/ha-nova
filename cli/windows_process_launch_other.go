//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func applyWindowsProcessLaunchProfile(cmd *exec.Cmd, profile windowsProcessLaunchProfile) {
	if profile.attachOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
}
