//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func applyWindowsProcessLaunchProfile(cmd *exec.Cmd, profile windowsProcessLaunchProfile) {
	if profile.attachOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	var creationFlags uint32
	if profile.createNewProcessGroup {
		creationFlags |= windows.CREATE_NEW_PROCESS_GROUP
	}
	if profile.createNoWindow {
		creationFlags |= windows.CREATE_NO_WINDOW
	}
	if profile.detached {
		creationFlags |= windows.DETACHED_PROCESS
	}
	if creationFlags == 0 && !profile.hideWindow && profile.inheritHandles {
		return
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:       profile.hideWindow,
		CreationFlags:    creationFlags,
		NoInheritHandles: !profile.inheritHandles,
	}
}
