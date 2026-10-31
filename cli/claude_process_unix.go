//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// claudeProcessTree: claude runs in its own process group, so a deadline
// stops what it started too (git during a marketplace fetch, a wrapper's
// child), not only the process HA NOVA started.
type claudeProcessTree struct{ cmd *exec.Cmd }

func newClaudeProcessTree(cmd *exec.Cmd) *claudeProcessTree {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &claudeProcessTree{cmd: cmd}
}

func (t *claudeProcessTree) adopt()   {}
func (t *claudeProcessTree) release() {}

func (t *claudeProcessTree) kill() error {
	if err := syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL); !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return os.ErrProcessDone
}
