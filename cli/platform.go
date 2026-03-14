package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func isInteractiveTTY() bool {
	if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
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

func openBrowser(url string) error {
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

func copyToClipboard(value string) error {
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
