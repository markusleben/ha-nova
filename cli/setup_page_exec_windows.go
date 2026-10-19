//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// ownSignalGroup keeps a step 4 verb out of the console's Ctrl+C: an apply
// stopped between two room writes would skip its own rollback.
func ownSignalGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
