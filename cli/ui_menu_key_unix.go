//go:build !windows

package main

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// menuInputPending reports whether in has a byte to read within wait: an
// escape sequence's next byte arrives at once, a lone Esc has none.
func menuInputPending(in *os.File, wait time.Duration) bool {
	fds := []unix.PollFd{{Fd: int32(in.Fd()), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, int(wait/time.Millisecond))
		if err == unix.EINTR {
			continue
		}
		return err == nil && n > 0
	}
}
