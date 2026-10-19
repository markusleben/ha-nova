package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"testing"
	"time"
)

// Processes the setup page starts: the typed verbs behind step 4, and the
// terminal running Claude Code.

// setupVerbEnvelope: the parts of a typed verb's envelope the page uses.
type setupVerbEnvelope struct {
	Server string               `json:"server"`
	Status string               `json:"status"`
	Tier   string               `json:"tier"`
	Plan   *verbPlan            `json:"plan"`
	Config *configAction        `json:"config"`
	Undo   *snapshotSaveReceipt `json:"undo"`
	Result json.RawMessage      `json:"result"`
	Error  *verbError           `json:"error"`
}

// pageVerb runs one typed verb for the server setup just connected (the
// child's own selection could differ): HA_NOVA_SERVER or default_server.
func pageVerb(args ...string) (setupVerbEnvelope, int, error) {
	return setupPageVerb(append(slices.Clone(args), "--server", activeServerProfile())...)
}

// setupPageVerb runs one typed verb as its own process, so its JSON never
// reaches the terminal, and returns its envelope and exit code.
var setupPageVerb = func(args ...string) (setupVerbEnvelope, int, error) {
	if testing.Testing() { // the test binary would run the tests again
		return setupVerbEnvelope{}, 0, errors.New("setup page verbs are stubbed in tests")
	}
	exe, err := os.Executable()
	if err != nil {
		return setupVerbEnvelope{}, 0, err
	}
	// Generous: killing an apply mid-save would skip its own rollback.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	ownSignalGroup(cmd)
	err = cmd.Run()
	exit := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exit, err = exitErr.ExitCode(), nil
	}
	if err != nil {
		return setupVerbEnvelope{}, 0, err
	}
	var env setupVerbEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		return setupVerbEnvelope{}, 0, errors.New("no envelope: " + args[0])
	}
	return env, exit, nil
}

// openClaudeTerminal: step 4's "Open Claude Code" (spec §16 D5 A). A
// browser can't start a terminal, so the CLI opens the platform's terminal
// running `claude` in the home folder. An error leaves the page's copyable
// hint instead.
func openClaudeTerminal() error {
	if _, err := exec.LookPath("claude"); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// Run, not Start: a denied Automation prompt fails here, and the
		// page then shows its hint.
		run := exec.Command("osascript", "-e", `tell application "Terminal" to do script "cd ~ && claude"`, "-e", `tell application "Terminal" to activate`)
		run.Dir = home
		return run.Run()
	case "windows":
		// The folder comes from Dir: cmd and wt would parse a path's & or ;.
		if _, err := exec.LookPath("wt.exe"); err == nil {
			cmd = exec.Command("wt.exe", "-d", ".", "cmd", "/k", "claude")
		} else {
			cmd = exec.Command("cmd", "/c", "start", "", "cmd", "/k", "claude")
		}
	default:
		for _, term := range [][]string{{"x-terminal-emulator", "-e"}, {"gnome-terminal", "--"}, {"konsole", "-e"}, {"xterm", "-e"}} {
			if _, err := exec.LookPath(term[0]); err == nil {
				cmd = exec.Command(term[0], term[1], "claude")
				break
			}
		}
	}
	if cmd == nil {
		return errors.New("no terminal to open")
	}
	cmd.Dir = home
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
