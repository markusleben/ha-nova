//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// claudeProcessTree: claude runs in its own process group, so a deadline
// stops what it started too (git during a marketplace fetch, a wrapper's
// child), not only the process HA NOVA started. That group doesn't get the
// terminal's Ctrl+C, so while claude runs HA NOVA takes SIGINT, SIGTERM and
// SIGHUP itself (one ignored since start, as under nohup, stays ignored),
// stops the group, and hands the signal back.
type claudeProcessTree struct {
	cmd      *exec.Cmd
	signals  chan os.Signal
	released chan struct{}
	watched  chan struct{}
}

func newClaudeProcessTree(cmd *exec.Cmd) *claudeProcessTree {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	t := &claudeProcessTree{cmd: cmd, signals: make(chan os.Signal, 1), released: make(chan struct{})}
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			signal.Notify(t.signals, sig)
		}
	}
	return t
}

func (t *claudeProcessTree) adopt() {
	t.watched = make(chan struct{})
	go func() {
		defer close(t.watched)
		select {
		case sig := <-t.signals:
			_ = t.kill()
			t.handBack(sig)
		case <-t.released:
		}
	}()
}

func (t *claudeProcessTree) release() {
	close(t.released)
	if t.watched != nil {
		<-t.watched
	}
	signal.Stop(t.signals)
	select {
	case sig := <-t.signals:
		t.handBack(sig)
	default:
	}
}

// handBack sends sig to HA NOVA again with its own handler gone, so it ends
// HA NOVA as it would have without one, exit status included.
func (t *claudeProcessTree) handBack(sig os.Signal) {
	signal.Stop(t.signals)
	_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))
}

func (t *claudeProcessTree) kill() error {
	if err := syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL); !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return os.ErrProcessDone
}
