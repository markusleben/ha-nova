package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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
// A failure leaves one line in the terminal, which the page points to.
func pageVerb(args ...string) (setupVerbEnvelope, int, error) {
	env, exit, err := setupPageVerb(append(slices.Clone(args), "--server", activeServerProfile())...)
	switch {
	case err != nil:
		printHumanErr("ha-nova %s: %s", strings.Join(args, " "), err)
	case env.Error != nil:
		printHumanErr("ha-nova %s: %s (%s)", strings.Join(args, " "), env.Error.Message, env.Error.Code)
	}
	return env, exit, err
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

// claudeFolderName: "Open Claude Code" starts Claude Code in this folder
// in the home folder. Claude Code asks once whether to trust a folder; for
// the home folder itself it asks on every start, and trusting it would
// cover every file there (0.27 lane F5).
const claudeFolderName = "HA NOVA"

// claudeFolder returns the folder Claude Code starts in, a real folder (no
// link or junction that could lead back to the home folder). One it
// creates is recorded, so uninstall removes only that one.
func claudeFolder(paths runtimePaths) (string, error) {
	if paths.Home == "" {
		return "", errors.New("no home folder")
	}
	dir := filepath.Join(paths.Home, claudeFolderName)
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return "", err
		}
		_ = withClientMutationLockWaiting(paths, func() error { // unrecorded: uninstall leaves it
			state, err := loadState(paths)
			if err != nil {
				return err
			}
			state.ClaudeFolder = dir
			return saveState(paths, state)
		})
	}
	if info, err := os.Lstat(dir); err != nil || info.Mode().Type() != fs.ModeDir {
		return "", fmt.Errorf("%s is not a plain folder", dir)
	}
	return dir, nil
}

// openClaudeTerminal: step 4's "Open Claude Code" (spec §16 D5 A). A
// browser can't start a terminal, so the CLI opens the platform's terminal
// running `claude` in claudeFolder. An error leaves the page's copyable
// hint instead.
func openClaudeTerminal(paths runtimePaths) error {
	dir, err := claudeFolder(paths) // first: the manual hint names it too
	if err != nil {
		return err
	}
	claude, err := claudeRuntimePath()
	if err != nil {
		return err
	}
	profile := ""
	if strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")) != "" {
		profile, err = filepath.Abs(claudeConfigRoot(paths.Home))
		if err != nil {
			return err
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// Run, not Start: a denied Automation prompt fails here, and the
		// page then shows its hint.
		replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
		command := `quoted form of "` + replacer.Replace(claude) + `"`
		if profile != "" {
			command = `"env " & quoted form of "CLAUDE_CONFIG_DIR=` + replacer.Replace(profile) + `" & " " & ` + command
		}
		run := exec.Command("osascript", "-e", `tell application "Terminal" to do script "cd " & quoted form of "`+replacer.Replace(dir)+`" & " && " & `+command, "-e", `tell application "Terminal" to activate`)
		run.Dir = dir
		return run.Run()
	case "windows":
		// The folder comes from Dir: cmd and wt would parse a path's & or ;.
		if _, err := exec.LookPath("wt.exe"); err == nil {
			cmd = exec.Command("wt.exe", "-d", ".", "cmd", "/k", "claude")
		} else {
			cmd = exec.Command("cmd", "/c", "start", "", "cmd", "/k", "claude")
		}
		cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(claude)+string(os.PathListSeparator)+os.Getenv("PATH"))
	default:
		for _, term := range [][]string{{"x-terminal-emulator", "-e"}, {"gnome-terminal", "--"}, {"konsole", "-e"}, {"xterm", "-e"}} {
			if _, err := exec.LookPath(term[0]); err == nil {
				cmd = exec.Command(term[0], term[1], claude)
				break
			}
		}
	}
	if cmd == nil {
		return errors.New("no terminal to open")
	}
	cmd.Dir = dir
	if profile != "" {
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+profile)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// removeClaudeFolder: uninstall removes the folder setup created, only
// while it is still an empty plain folder; anything put there stays.
func removeClaudeFolder(home string, state installState, report *uninstallReport) {
	dir := state.ClaudeFolder
	if info, err := os.Lstat(dir); dir == "" || dir != filepath.Join(home, claudeFolderName) ||
		err != nil || info.Mode().Type() != fs.ModeDir {
		return
	}
	if os.Remove(dir) == nil {
		report.addRemoved(dir)
	}
}
