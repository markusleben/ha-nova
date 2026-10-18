package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func isInteractiveTTY() bool {
	if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		return true
	}
	return false
}

// stdoutIsInteractiveTTY reports whether stdout still reaches a terminal. A
// prompt written to a redirected stdout (`ha-nova update > update.log`) would
// block on invisible input — callers that ask questions must check BOTH ends.
func stdoutIsInteractiveTTY() bool {
	if fi, err := os.Stdout.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		return true
	}
	return false
}

func promptLine(label, defaultValue string) (string, error) {
	fmt.Fprint(os.Stdout, label)
	if defaultValue != "" {
		fmt.Fprintf(os.Stdout, " [%s]", defaultValue)
	}
	fmt.Fprint(os.Stdout, ": ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}

func printInfo(format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, "[ha-nova] "+format+"\n", args...)
}

func printWarn(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[ha-nova] WARNING: "+format+"\n", args...)
}

func printErr(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[ha-nova] ERROR: "+format+"\n", args...)
}

// openBrowser is the only browser launcher (setup, sign-in, Link OAuth,
// uninstall pages all reach it through openBrowserForSetup). Under go test it
// never launches anything: tests must not open pages on the developer's
// machine; a test that needs an opener injects its own.
func openBrowser(url string) error {
	if testing.Testing() || os.Getenv("HA_NOVA_NO_BROWSER") == "1" {
		return nil
	}
	if !isInteractiveTTY() {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// copyToClipboard: like openBrowser, never under go test (a test token must
// not land in the developer's clipboard).
func copyToClipboard(value string) error {
	if testing.Testing() || os.Getenv("HA_NOVA_NO_BROWSER") == "1" {
		return fmt.Errorf("clipboard disabled")
	}
	if !isInteractiveTTY() {
		return fmt.Errorf("clipboard disabled")
	}
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(value)
		return cmd.Run()
	case "windows":
		cmd := exec.Command("clip")
		cmd.Stdin = strings.NewReader(value)
		return cmd.Run()
	default:
		for _, candidate := range []string{"wl-copy", "xclip"} {
			if _, err := exec.LookPath(candidate); err == nil {
				var cmd *exec.Cmd
				if candidate == "wl-copy" {
					cmd = exec.Command(candidate)
				} else {
					cmd = exec.Command(candidate, "-selection", "clipboard")
				}
				cmd.Stdin = strings.NewReader(value)
				return cmd.Run()
			}
		}
	}
	return fmt.Errorf("clipboard helper unavailable")
}
