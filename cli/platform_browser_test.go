package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The real opener is inert under go test even when every other gate is open:
// an interactive stdin, no HA_NOVA_NO_BROWSER, and a launcher on PATH.
func TestOpenBrowserNeverLaunchesUnderGoTest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rundll32 is not resolved through PATH")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "launched")
	script := "#!/bin/sh\n: > '" + marker + "'\n" // shell builtin: PATH holds only the fakes
	for _, name := range []string{"open", "xdg-open"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("HA_NOVA_NO_BROWSER", "")
	tty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	originalStdin := os.Stdin
	os.Stdin = tty
	defer func() { os.Stdin = originalStdin }()
	if !isInteractiveTTY() {
		t.Skip("the null device is not a character device here")
	}

	if err := openBrowser("http://192.0.2.10:8123/"); err != nil {
		t.Fatalf("openBrowser() error = %v", err)
	}
	// A launched fake needs up to a second to start on a busy machine.
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("openBrowser launched a browser under go test")
		}
	}
}
