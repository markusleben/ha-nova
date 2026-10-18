//go:build !windows

package main

import (
	"os"
	"testing"
	"time"
)

// A lone Esc goes back at once; arrows work in normal (CSI) and application
// cursor mode (SS3) and are never read as Esc.
func TestReadSetupMenuKeyEscapeSequences(t *testing.T) {
	for input, want := range map[string]string{
		"\x1b":   "back",
		"\x1b[A": "up",
		"\x1b[B": "down",
		"\x1bOA": "up",
		"\x1bOB": "down",
		"\x1b[C": "",
		"\r":     "enter",
		"\x03":   "exit",
		"j":      "down",
		"\x1bx":  "back",
		"k":      "up",
	} {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.WriteString(input); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		got, err := readSetupMenuKeyFrom(reader) // writer stays open: a lone Esc must not wait for EOF
		if err != nil || got != want {
			t.Errorf("key %q = %q, %v; want %q", input, got, err, want)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("key %q took %v", input, elapsed)
		}
		reader.Close()
		writer.Close()
	}
}
