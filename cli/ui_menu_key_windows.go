//go:build windows

package main

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// menuInputPending reports whether in has input within wait: an escape
// sequence's next byte arrives at once, a lone Esc has none. A console
// handle also signals for non-key events; then the read waits for the next
// key, as before.
func menuInputPending(in *os.File, wait time.Duration) bool {
	event, err := windows.WaitForSingleObject(windows.Handle(in.Fd()), uint32(wait/time.Millisecond))
	return err == nil && event == windows.WAIT_OBJECT_0
}
