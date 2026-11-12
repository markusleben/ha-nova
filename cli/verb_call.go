package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// verbCall is one verb run's own context: where it writes its result (out)
// and its failure and progress lines (err). nil is the CLI's: the process's
// stdout and stderr as they are when written to. The MCP servers give each
// call its own (runCaptured), so no call swaps os.Stdout for another.
type verbCall struct{ out, err io.Writer }

func (c *verbCall) stdout() io.Writer {
	if c == nil {
		return os.Stdout
	}
	return c.out
}

func (c *verbCall) stderr() io.Writer {
	if c == nil {
		return os.Stderr
	}
	return c.err
}

// errorf is printErr for this call.
func (c *verbCall) errorf(format string, args ...any) {
	fmt.Fprintf(c.stderr(), "[ha-nova] ERROR: "+format+"\n", args...)
}

// runCaptured runs a verb with its own output and returns what it wrote. A
// panic becomes exit verbExitUnknown without output: a change may have been
// sent (runLocked).
func runCaptured(run func(*verbCall) int) (out, errOut string, exit int) {
	var outBuf, errBuf callBuffer
	var crash string
	func() {
		defer func() {
			if p := recover(); p != nil {
				exit, crash = verbExitUnknown, fmt.Sprint("internal error: ", p)
			}
		}()
		exit = run(&verbCall{out: &outBuf, err: &errBuf})
	}()
	out, errOut = outBuf.String(), errBuf.String()
	if crash != "" {
		out, errOut = "", strings.TrimSpace(errOut+"\n"+crash)
	}
	return out, errOut, exit
}

// callBuffer holds one call's output: a window and a phone asking at once
// write their progress lines from two goroutines (confirm_both.go).
type callBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *callBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *callBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
