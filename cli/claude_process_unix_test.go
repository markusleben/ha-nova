//go:build unix

package main

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Ctrl+C in a terminal goes to ha-nova's process group, not to the one
// claude runs in. ha-nova stops claude's group and then ends as the signal
// says, as it did when claude shared its group.
func TestSignalStopsClaudeWithHANova(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			if signal.Ignored(sig) {
				t.Skipf("%v is ignored in this test run, so ha-nova ignores it too", sig)
			}
			logDir, _, _ := installFakeClaude(t, t.TempDir(), "plugin install", "hang orphan")
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			haNova := exec.Command(self)
			haNova.Env = append(os.Environ(), claudeCallerEnv+"=1")
			haNova.Stderr = &stderr
			haNova.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // the terminal's foreground group
			if err := haNova.Start(); err != nil {
				t.Fatal(err)
			}
			ended := make(chan struct{})
			go func() { _ = haNova.Wait(); close(ended) }()
			t.Cleanup(func() {
				_ = syscall.Kill(-haNova.Process.Pid, syscall.SIGKILL)
				<-ended
			})

			var holder int
			for start := time.Now(); holder == 0; time.Sleep(50 * time.Millisecond) {
				data, _ := os.ReadFile(filepath.Join(logDir, "holders.pid"))
				holder, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				if holder == 0 && time.Since(start) > 20*time.Second {
					t.Fatalf("claude never started\n%s", stderr.String())
				}
			}
			group, err := syscall.Getpgid(holder)
			if err != nil || group == haNova.Process.Pid {
				t.Fatalf("claude's group = %d, %v; want its own, not ha-nova's %d", group, err, haNova.Process.Pid)
			}
			t.Cleanup(func() { _ = syscall.Kill(-group, syscall.SIGKILL) })

			if err := syscall.Kill(-haNova.Process.Pid, sig); err != nil {
				t.Fatal(err)
			}
			select {
			case <-ended:
			case <-time.After(10 * time.Second):
				t.Fatalf("ha-nova still runs 10s after %v\n%s", sig, stderr.String())
			}
			if status, ok := haNova.ProcessState.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != sig {
				t.Fatalf("ha-nova ended with %v, want killed by %v\n%s", haNova.ProcessState, sig, stderr.String())
			}
			for _, pid := range []int{group, holder} { // claude leads its group
				if !windowGone(pid, 5*time.Second) {
					t.Fatalf("claude's process group %d outlived ha-nova (pid %d still runs)", group, pid)
				}
			}
		})
	}
}
