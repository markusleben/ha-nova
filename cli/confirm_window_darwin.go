package main

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// macOS: /usr/bin/osascript `display dialog` (SIP-protected, by absolute
// path, with an empty environment). Clicking it from another process needs
// Accessibility or input-posting permission, which a model can't grant
// itself; Cancel is the default button, so Return cancels.

const windowShowing = "ha-nova-window-showing"

// windowScript prints exactly one answer line; the message and the wait
// arrive as arguments, so no Home Assistant text is ever script source.
var windowScript = []string{
	"on run argv",
	"activate", // in front of the terminal or app that started it
	"try",
	`log "` + windowShowing + `"`, // stderr: the script runs and is about to show the window
	"with timeout of 600 seconds",
	`set r to display dialog (item 1 of argv) with title "HA NOVA" buttons {"Cancel", "Confirm"} default button "Cancel" cancel button "Cancel" with icon caution giving up after ((item 2 of argv) as integer)`,
	"end timeout",
	"on error number n",
	`if n is -128 then return "cancel"`,
	`return "error " & n`,
	"end try",
	`if gave up of r then return "timeout"`,
	`return "button " & (button returned of r)`,
	"end run",
}

// nativeWindowAvailable: a logged-in desktop session (Aqua); SSH and
// background sessions are not.
func nativeWindowAvailable() bool {
	cmd := exec.Command("/bin/launchctl", "managername")
	cmd.Env = []string{}
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "Aqua"
}

func nativeWindow(ctx context.Context, message string, wait time.Duration) windowAnswer {
	args := []string{}
	for _, line := range windowScript {
		args = append(args, "-e", line)
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", append(args, message, strconv.Itoa(int(wait.Seconds())))...)
	cmd.Env = []string{} // nothing from the caller's environment reaches the window
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return darwinAnswer(strings.TrimSpace(string(out)), err, ctx.Err() != nil, strings.Contains(stderr.String(), windowShowing))
}

// darwinAnswer reads osascript's result. Only a window that provably never
// showed is windowFailed (fallback E): osascript could not start, the script
// never reached the window (it did not compile: the scripting addition can't
// load in a sandbox), or display dialog refused at once (-1713: no user
// interaction, e.g. no window server). Anything after the script reached the
// window — killed, interrupted, another error — is no answer, never a fallback.
func darwinAnswer(out string, err error, ended, showing bool) windowAnswer {
	var exitErr *exec.ExitError
	switch {
	case ended:
		return windowNoAnswer
	case err != nil && !errors.As(err, &exitErr): // osascript could not be started
		return windowFailed
	case !showing && exitErr != nil && exitErr.ExitCode() > 0: // never reached the window
		return windowFailed
	case err == nil && out == "button Confirm":
		return windowConfirmed
	case err == nil && out == "cancel":
		return windowCancelled
	case err == nil && out == "error -1713":
		return windowFailed
	}
	return windowNoAnswer
}
