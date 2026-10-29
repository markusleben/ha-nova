package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Linux (untested on a real desktop): zenity (Cancel the default; kdialog
// has no such mode, so a KDE desktop without zenity uses chat), by absolute
// path only (a PATH entry could be a fake that "confirms"), with an
// allowlisted environment (no LD_PRELOAD or GTK_MODULES from the caller).
// On X11 any same-user process can click it; Wayland blocks that.

const linuxDialog = "/usr/bin/zenity"

var linuxDialogEnv = []string{"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "HOME", "LANG", "LC_ALL"}

func dialogAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

// nativeWindowAvailable: a graphical session and zenity.
func nativeWindowAvailable() bool {
	return (os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "") && executable(linuxDialog)
}

func nativeWindow(ctx context.Context, message, lang string, wait time.Duration) windowAnswer {
	args := []string{"--question", "--title=HA NOVA", "--text=" + message, "--ok-label=" + confirmT(lang, "window.confirm"), "--cancel-label=" + confirmT(lang, "window.cancel"),
		"--default-cancel", "--no-markup", "--timeout=" + strconv.Itoa(int(wait.Seconds()))}
	// The window dies with this process, even a SIGKILLed one (lane E2).
	// Pdeathsig follows the OS thread that started zenity, so hold it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cmd := exec.CommandContext(ctx, linuxDialog, args...)
	cmd.SysProcAttr = dialogAttr()
	cmd.Env = []string{}
	for _, key := range linuxDialogEnv {
		if value, set := os.LookupEnv(key); set {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	code := 0
	switch {
	case ctx.Err() != nil:
		return windowNoAnswer
	case err != nil && !errors.As(err, &exitErr):
		return windowFailed // could not start
	case err != nil:
		code = exitErr.ExitCode()
	}
	return zenityAnswer(code, stderr.String())
}

// zenityAnswer maps zenity's exit code (-1: ended by a signal) and stderr.
func zenityAnswer(code int, stderr string) windowAnswer {
	failure := strings.ToLower(stderr)
	switch {
	case code == 0:
		return windowConfirmed
	case strings.Contains(failure, "open display") || strings.Contains(failure, "connect to display"): // GTK at startup only
		return windowFailed
	case code == 1: // Cancel or close
		return windowCancelled
	case code == -1: // ended by a signal: closed from outside
		return windowClosed
	}
	return windowNoAnswer // zenity 5 = timeout; anything else: no answer
}
