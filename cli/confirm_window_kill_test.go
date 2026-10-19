//go:build unix

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Lane E2: a CLI ended by a signal never leaves its confirmation window
// behind. A sleep stands in for the window process; the CLI is this test
// binary, re-run as a helper.

var windowHelperAttr func() *syscall.SysProcAttr // Linux: the real window's Pdeathsig

func TestWindowHelperProcess(t *testing.T) {
	mode := os.Getenv("HA_NOVA_WINDOW_HELPER")
	if mode == "" {
		t.Skip("runs only as the helper process")
	}
	windowRunner = func(ctx context.Context, _ string, _ time.Duration) windowAnswer {
		runtime.LockOSThread() // as nativeWindow: Pdeathsig follows this thread
		defer runtime.UnlockOSThread()
		cmd := exec.CommandContext(ctx, "/bin/sleep", "60")
		if mode == "pdeathsig" {
			cmd.SysProcAttr = windowHelperAttr()
		}
		if err := cmd.Start(); err != nil {
			fmt.Println("error", err)
			return windowFailed
		}
		fmt.Println(cmd.Process.Pid)
		_ = cmd.Wait()
		fmt.Println("closed") // the window was withdrawn before the process ended
		return windowNoAnswer
	}
	askWindow(context.Background(), "test", time.Minute)
	fmt.Println("returned") // only where the signal can't end the process
	os.Exit(0)
}

// startWindowHelper runs the helper and returns it, its window's pid, and
// the rest of its output once it ends.
func startWindowHelper(t *testing.T, mode string) (*exec.Cmd, int, <-chan string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowHelperProcess$")
	cmd.Env = append(os.Environ(), "HA_NOVA_WINDOW_HELPER="+mode)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(out)
	first := make(chan string, 1)
	go func() { line, _ := reader.ReadString('\n'); first <- strings.TrimSpace(line) }()
	var line string
	select {
	case line = <-first:
	case <-time.After(30 * time.Second):
	}
	pid, err := strconv.Atoi(line)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("helper said %q", line)
	}
	rest := make(chan string, 1)
	go func() { all, _ := io.ReadAll(reader); rest <- string(all) }()
	t.Cleanup(func() {
		if !windowGone(pid, 0) { // only a window still running: never a reused pid
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return cmd, pid, rest
}

// windowGone waits up to wait until pid is gone (or a zombie nobody reaped yet).
func windowGone(pid int, wait time.Duration) bool {
	for deadline := time.Now().Add(wait); ; time.Sleep(20 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		if stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil && zombieStat(string(stat)) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
	}
}

func zombieStat(stat string) bool {
	i := strings.LastIndexByte(stat, ')') // the state follows the last ")"
	return i > 0 && len(stat) > i+2 && stat[i+2] == 'Z'
}

func TestWindowClosesWhenTheCLIGetsASignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			if signal.Ignored(sig) {
				t.Skipf("%v is ignored here (nohup, a background job): the helper inherits that", sig)
			}
			cli, window, rest := startWindowHelper(t, "signal")
			if err := cli.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			out := <-rest
			err := cli.Wait()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != sig {
				t.Fatalf("the CLI did not end by %v: %v (%q)", sig, err, out)
			}
			if !strings.Contains(out, "closed") || !windowGone(window, 5*time.Second) {
				t.Fatalf("the window outlived the CLI (%v): %q", sig, out)
			}
		})
	}
}
