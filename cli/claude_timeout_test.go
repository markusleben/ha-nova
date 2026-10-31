package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A process the tests start as `claude` (this test binary, copied under
// that name) has fakeClaudeEnv set to the dir it logs its calls to.
const (
	fakeClaudeEnv      = "HA_NOVA_TEST_FAKE_CLAUDE"
	fakeClaudeStallEnv = "HA_NOVA_TEST_FAKE_CLAUDE_STALL" // the call that misbehaves, e.g. "plugin install"
	fakeClaudeHowEnv   = "HA_NOVA_TEST_FAKE_CLAUDE_HOW"   // "hang", "orphan" (a child keeps its output open), or both
	fakeClaudeHomeEnv  = "HA_NOVA_TEST_FAKE_CLAUDE_HOME"  // the only home it writes to: never the developer's own
	fakeClaudeHold     = "--hold-output"
)

// runFakeClaude acts like `claude` on the plugin files in the home dir,
// except for the call fakeClaudeStallEnv names.
func runFakeClaude(dir string, args []string) int {
	if len(args) == 1 && args[0] == fakeClaudeHold {
		time.Sleep(2 * time.Minute)
		return 0
	}
	call := strings.Join(args, " ")
	appendFakeClaudeLine(filepath.Join(dir, "calls.log"), call)
	stall := os.Getenv(fakeClaudeStallEnv) != "" && strings.HasPrefix(call, os.Getenv(fakeClaudeStallEnv))
	how := os.Getenv(fakeClaudeHowEnv)
	if stall && strings.Contains(how, "orphan") {
		self, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		holder := exec.Command(self, fakeClaudeHold)
		holder.Stdout, holder.Stderr = os.Stdout, os.Stderr
		if err := holder.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		appendFakeClaudeLine(filepath.Join(dir, "holders.pid"), strconv.Itoa(holder.Process.Pid))
	}
	if stall && strings.Contains(how, "hang") {
		time.Sleep(2 * time.Minute)
		return 0
	}
	home := os.Getenv(fakeClaudeHomeEnv)
	if home == "" {
		fmt.Fprintln(os.Stderr, "fake claude: no test home")
		return 1
	}
	plugins := filepath.Join(claudeConfigRoot(home), "plugins")
	var err error
	switch {
	case strings.HasPrefix(call, "plugin marketplace add "):
		data, _ := json.Marshal(map[string]any{"ha-nova": map[string]string{"source": strings.TrimPrefix(call, "plugin marketplace add ")}})
		err = os.WriteFile(filepath.Join(plugins, "known_marketplaces.json"), data, 0o644)
	case call == "plugin marketplace remove ha-nova":
		err = os.Remove(filepath.Join(plugins, "known_marketplaces.json"))
	case call == "plugin remove ha-nova@ha-nova":
		err = os.Remove(filepath.Join(plugins, "installed_plugins.json"))
	case call == "plugin install ha-nova@ha-nova":
		cache := filepath.Join(plugins, "cache", "ha-nova", "ha-nova", "0.1.12")
		data, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{"ha-nova@ha-nova": []any{map[string]string{"scope": "user", "installPath": cache, "version": "0.1.12"}}}})
		if err = os.MkdirAll(cache, 0o755); err == nil {
			err = os.WriteFile(filepath.Join(plugins, "installed_plugins.json"), data, 0o644)
		}
	case strings.HasPrefix(call, "plugin list"):
		err = errors.New("unknown option '--json'")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("ok")
	return 0
}

func appendFakeClaudeLine(path, line string) {
	if file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintln(file, line)
		file.Close()
	}
}

// installFakeClaude puts the fake first on PATH with short deadlines
// (installs 3s, the rest 20s, 2s for held output), makes the temp dir home
// the home of this test and of the fake, and returns the fake's log dir.
// Children it leaves behind die before the temp dirs go.
func installFakeClaude(t *testing.T, home, stall, how string) (string, time.Duration, time.Duration) {
	t.Helper()
	requireTestHome(t, home)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, logDir := t.TempDir(), t.TempDir()
	fake := filepath.Join(bin, "claude")
	if runtime.GOOS == "windows" {
		fake += ".exe"
	}
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenFile(fake, os.O_CREATE|os.O_WRONLY, 0o755)
	if err == nil {
		_, err = io.Copy(dst, src)
		if closeErr := dst.Close(); err == nil {
			err = closeErr
		}
	}
	src.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"HOME", "USERPROFILE", fakeClaudeHomeEnv} {
		t.Setenv(key, home)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv(fakeClaudeEnv, logDir)
	t.Setenv(fakeClaudeStallEnv, stall)
	t.Setenv(fakeClaudeHowEnv, how)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// A new executable's first start can be slow (a malware scan on
	// Windows): pay it before any deadline runs.
	if output, err := exec.Command(fake, "--version").CombinedOutput(); err != nil {
		t.Fatalf("fake claude: %v %s", err, output)
	}
	quick, slow, delay := claudeQuickTimeout, claudeSlowTimeout, claudeWaitDelay
	claudeQuickTimeout, claudeSlowTimeout, claudeWaitDelay = 20*time.Second, 3*time.Second, 2*time.Second
	t.Cleanup(func() {
		claudeQuickTimeout, claudeSlowTimeout, claudeWaitDelay = quick, slow, delay
		claudeStalled.Store(nil)
		pids, _ := os.ReadFile(filepath.Join(logDir, "holders.pid"))
		for _, field := range strings.Fields(string(pids)) {
			pid, _ := strconv.Atoi(field)
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
				_, _ = process.Wait() // Windows: the exe stays locked until it exits
			}
		}
	})
	return logDir, claudeSlowTimeout, claudeWaitDelay
}

func fakeClaudeCalls(t *testing.T, logDir, call string) int {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(logDir, "calls.log"))
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == call {
			count++
		}
	}
	return count
}

// A hung claude is stopped at its deadline with everything it started; one
// that exited but left a child holding its output counts as done once the
// wait delay closes it. Either way the call returns within timeout plus
// wait delay.
func TestClaudeCallStopsOnTime(t *testing.T) {
	const slack = 5 * time.Second
	for _, tc := range []struct {
		how      string
		timedOut bool
	}{
		{"hang", true},
		{"orphan", false},
		{"hang orphan", true}, // returns before the wait delay: the child holding the output died with it
	} {
		t.Run(tc.how, func(t *testing.T) {
			logDir, timeout, delay := installFakeClaude(t, t.TempDir(), "plugin install", tc.how)
			minimum, maximum := delay, delay+slack
			if tc.timedOut {
				minimum, maximum = timeout, timeout+delay
			}
			start := time.Now()
			output, err := claudeOutput("plugin", "install", "ha-nova@ha-nova")
			elapsed := time.Since(start)
			if elapsed < minimum || elapsed >= maximum {
				t.Fatalf("claude call took %s, want %s to %s", elapsed, minimum, maximum)
			}
			if !tc.timedOut {
				if err != nil || strings.TrimSpace(string(output)) != "ok" {
					t.Fatalf("claude call = %q, %v; want its output and no error", output, err)
				}
				return
			}
			var stalled *claudeTimeoutError
			if !errors.As(err, &stalled) {
				t.Fatalf("claude call error = %v, want a timeout", err)
			}
			for _, want := range []string{"`claude plugin install ha-nova@ha-nova` didn't finish within 3s", "run the same ha-nova command again"} {
				if !strings.Contains(claudeFailureText(output, err), want) {
					t.Fatalf("timeout message %q lacks %q", claudeFailureText(output, err), want)
				}
			}
			start = time.Now()
			if _, err := claudeOutput("plugin", "list", "--json"); !errors.As(err, &stalled) || time.Since(start) > time.Second {
				t.Fatalf("call after the timeout = %v after %s, want the timeout at once", err, time.Since(start))
			}
			if got := fakeClaudeCalls(t, logDir, "plugin list --json"); got != 0 {
				t.Fatalf("claude ran %d more times after it stopped answering", got)
			}
		})
	}
}

// The Windows update helper holds the update lock while it syncs the AI
// apps. A Claude Code that stops answering mid-install fails the sync:
// the helper goes back to the old version, lets the lock go, and names the
// next step. One that leaves a child holding its output still finishes the
// update.
func TestRunInternalReplaceSurvivesStalledClaude(t *testing.T) {
	const slack = 10 * time.Second
	for _, tc := range []struct {
		how      string
		wantExit int
	}{
		{"hang", 1},
		{"orphan", 0},
	} {
		t.Run(tc.how, func(t *testing.T) {
			home := t.TempDir()
			for _, key := range []string{"HOME", "USERPROFILE"} {
				t.Setenv(key, home)
			}
			t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
			t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
			paths, err := detectPaths()
			if err != nil {
				t.Fatalf("detectPaths() error: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeInstalledClaudePluginFixture(t, home)
			if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"), []byte(`{"ha-nova":{"source":"https://github.com/markusleben/ha-nova"}}`), 0o644); err != nil {
				t.Fatal(err)
			}
			writeClaudeMarketplaceFixture(t, paths.InstallRoot)
			if err := saveState(paths, installState{Version: "0.1.12", ClientsVerifiedVersion: "0.1.12", InstalledClients: []string{"claude"}}); err != nil {
				t.Fatal(err)
			}
			logDir, timeout, delay := installFakeClaude(t, home, "plugin install", tc.how)

			originalWait, originalApply := waitForParentReleaseForReplace, applyStagedBundleWithRollbackForReplace
			t.Cleanup(func() {
				waitForParentReleaseForReplace, applyStagedBundleWithRollbackForReplace = originalWait, originalApply
			})
			waitForParentReleaseForReplace = func(int) {}
			rollbacks := 0
			applyStagedBundleWithRollbackForReplace = func(runtimePaths, string) (func() error, func() error, error) {
				return func() error { rollbacks++; return nil }, func() error { return nil }, nil
			}

			start := time.Now()
			exitCode, output := captureCommandOutput(t, func() int {
				return runInternalReplace(paths, []string{"--parent-pid", "0", "--stage-root", t.TempDir(), "--lifecycle-marker", "none"})
			})
			if elapsed := time.Since(start); elapsed > timeout+delay+slack {
				t.Fatalf("update helper took %s with a stalled claude\n%s", elapsed, output)
			}
			if exitCode != tc.wantExit {
				t.Fatalf("runInternalReplace() exit = %d, want %d\n%s", exitCode, tc.wantExit, output)
			}
			release, acquired := acquireAutoRepairLock(paths)
			if !acquired {
				t.Fatalf("update lock still held after the helper returned\n%s", output)
			}
			release()
			if tc.wantExit == 0 {
				if rollbacks != 0 || !strings.Contains(output, "Updated HA NOVA to ") {
					t.Fatalf("expected a finished update, rollbacks = %d\n%s", rollbacks, output)
				}
				return
			}
			if rollbacks != 1 {
				t.Fatalf("rollbacks = %d, want 1\n%s", rollbacks, output)
			}
			if state, err := loadState(paths); err != nil || state.ClientsVerifiedVersion != "" {
				t.Fatalf("clients-verified marker = %q, %v; want it cleared so the next check-update or doctor run repairs Claude Code", state.ClientsVerifiedVersion, err)
			}
			for _, want := range []string{"didn't finish within 3s", "run the same ha-nova command again", "update aborted", "went back to the old version"} {
				if !strings.Contains(output, want) {
					t.Fatalf("helper output lacks %q:\n%s", want, output)
				}
			}
			if got := fakeClaudeCalls(t, logDir, "plugin install ha-nova@ha-nova"); got != 1 {
				t.Fatalf("claude plugin install ran %d times, want 1 (no second wait on a hung Claude Code)\n%s", got, output)
			}
		})
	}
}
