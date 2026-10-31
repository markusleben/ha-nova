package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

var claudeDesktopPlatformOS = runtime.GOOS

func claudeRuntimePath() (string, error) {
	path, err := exec.LookPath("claude")
	if err == nil || !errors.Is(err, exec.ErrNotFound) {
		return path, err
	}
	home, homeErr := os.UserHomeDir()
	if homeErr == nil && executableInUserLocalBin("claude") {
		return filepath.Join(home, ".local", "bin", "claude"), nil
	}
	if homeErr == nil && claudeDesktopPlatformOS == "darwin" {
		if bundled := claudeDesktopRuntime(home); bundled != "" {
			return bundled, nil
		}
	}
	return "claude", err
}

// Every `claude` call has a deadline: Claude Code can stop answering (a
// prompt nobody sees, a stuck download, a lock another session holds), and
// the update helper calls it while it holds the update lock. Installs and
// downloads get minutes, everything else seconds. Test seams.
var (
	claudeQuickTimeout = 30 * time.Second
	claudeSlowTimeout  = 3 * time.Minute
	// claudeWaitDelay: how long a claude that exited or was stopped may
	// leave a child holding its output open before HA NOVA closes it.
	claudeWaitDelay = 5 * time.Second
)

// claudeStalled is the first claude call of this process that ran out of
// time. Every later call fails at once with it, so one hung Claude Code
// costs a run one timeout, not one per step and rollback step.
var claudeStalled atomic.Pointer[claudeTimeoutError]

type claudeTimeoutError struct {
	step    string
	timeout time.Duration
}

func (e *claudeTimeoutError) Error() string {
	return fmt.Sprintf("`claude %s` didn't finish within %s, so HA NOVA stopped it and skips Claude Code for the rest of this run. Open Claude Code once and finish any prompt it shows, then run `ha-nova doctor`", e.step, e.timeout)
}

// claudeOutput runs one claude CLI step and returns its combined output.
func claudeOutput(args ...string) ([]byte, error) {
	var output bytes.Buffer
	err := runClaude(nil, &output, &output, args...)
	return output.Bytes(), err
}

// claudeStep runs one claude CLI step that must succeed.
func claudeStep(args ...string) error {
	if output, err := claudeOutput(args...); err != nil {
		return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(args, " "), claudeFailureText(output, err))
	}
	return nil
}

// claudeFailureText: why a claude step failed, for its error message: the
// timeout, or what Claude Code printed.
func claudeFailureText(output []byte, err error) string {
	var stalled *claudeTimeoutError
	if errors.As(err, &stalled) {
		return stalled.Error()
	}
	return strings.TrimSpace(string(output))
}

// runClaude runs one claude CLI step with env (nil: this process's) and an
// empty stdin. Installs, updates, and marketplace adds get
// claudeSlowTimeout; lists, validation, and removals claudeQuickTimeout.
func runClaude(env []string, stdout, stderr io.Writer, args ...string) error {
	if stalled := claudeStalled.Load(); stalled != nil {
		return stalled
	}
	path, err := claudeRuntimePath()
	if err != nil {
		return err
	}
	verb := ""
	if len(args) > 1 {
		verb = args[1]
	}
	if verb == "marketplace" && len(args) > 2 {
		verb = args[2]
	}
	timeout := claudeQuickTimeout
	if verb == "install" || verb == "update" || verb == "add" {
		timeout = claudeSlowTimeout
	}
	timedOut, err := runWithDeadline(path, args, timeout, env, stdout, stderr)
	if timedOut {
		claudeStalled.CompareAndSwap(nil, &claudeTimeoutError{step: strings.Join(args, " "), timeout: timeout})
		return claudeStalled.Load()
	}
	return err
}

// runWithDeadline runs path with an empty stdin (os.DevNull) and kills it
// after timeout. A process that exited cleanly but left a child holding its
// output open counts as done once claudeWaitDelay closes that output.
func runWithDeadline(path string, args []string, timeout time.Duration, env []string, stdout, stderr io.Writer) (timedOut bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env, cmd.Stdout, cmd.Stderr = env, stdout, stderr
	cmd.WaitDelay = claudeWaitDelay
	err = cmd.Run()
	if err == nil || errors.Is(err, exec.ErrWaitDelay) {
		return false, nil
	}
	return ctx.Err() != nil, err
}

func claudeDesktopRuntime(home string) string {
	root := filepath.Join(home, "Library", "Application Support", "Claude", "claude-code")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	versions := []string{}
	for _, entry := range entries {
		if _, err := parseReleaseVersion(entry.Name()); err == nil && entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		cmp, _ := compareReleaseVersions(versions[i], versions[j])
		return cmp > 0
	})
	for _, version := range versions {
		installs, _ := os.ReadDir(filepath.Join(root, version))
		for _, install := range installs {
			if !install.IsDir() || len(install.Name()) != 12 {
				continue
			}
			dir := filepath.Join(root, version, install.Name())
			marker, err := os.ReadFile(filepath.Join(dir, ".verified"))
			checksum := strings.TrimSpace(string(marker))
			if err != nil || len(checksum) != 64 || !strings.HasPrefix(checksum, install.Name()) || strings.Trim(checksum, "0123456789abcdef") != "" {
				continue
			}
			path := filepath.Join(dir, "claude.app", "Contents", "MacOS", "claude")
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
				continue
			}
			if claudeDesktopRuntimeWorks(path, version) {
				return path
			}
		}
	}
	return ""
}

func claudeDesktopRuntimeWorks(path, version string) bool {
	for _, probe := range []struct {
		args []string
		want []string
	}{
		{[]string{"--version"}, []string{version + " (Claude Code)"}},
		{[]string{"plugin", "--help"}, []string{"install", "update", "remove", "list", "validate"}},
		{[]string{"plugin", "marketplace", "--help"}, []string{"add", "remove", "update"}},
	} {
		var output bytes.Buffer
		if _, err := runWithDeadline(path, probe.args, 3*time.Second, nil, &output, nil); err != nil {
			return false
		}
		for _, want := range probe.want {
			if !strings.Contains(output.String(), want) {
				return false
			}
		}
	}
	return true
}
