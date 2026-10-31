//go:build windows

package main

import (
	"os/exec"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

// claudeProcessTree: claude runs in a job object, so a deadline stops what
// it started too. Behind npm's claude.cmd, the process HA NOVA starts is
// cmd.exe and Claude Code a node.exe under it.
type claudeProcessTree struct {
	cmd     *exec.Cmd
	job     windows.Handle
	adopted atomic.Bool
}

func newClaudeProcessTree(cmd *exec.Cmd) *claudeProcessTree {
	job, _ := windows.CreateJobObject(nil, nil) // 0 on failure: kill falls back to the process
	return &claudeProcessTree{cmd: cmd, job: job}
}

func (t *claudeProcessTree) adopt() {
	if t.job == 0 {
		return
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(t.cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(process)
	t.adopted.Store(windows.AssignProcessToJobObject(t.job, process) == nil)
}

func (t *claudeProcessTree) release() {
	if t.job != 0 {
		windows.CloseHandle(t.job)
	}
}

func (t *claudeProcessTree) kill() error {
	if t.adopted.Load() && windows.TerminateJobObject(t.job, 1) == nil {
		return nil
	}
	return t.cmd.Process.Kill()
}
