package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// verbCall is one verb run's own context: where it writes its result (out)
// and its failure and progress lines (err), when it came in and must answer
// (check_budget.go), and in ha-nova serve the chat app's user. nil is the
// CLI's: the process's stdout and stderr as they are when written to, and
// the command's commandStart, checkDeadline, and chatCall. The MCP servers
// give each call its own (runLocked), so no call reads another's.
type verbCall struct {
	out, err        io.Writer
	start, deadline time.Time   // zero: windowWait and the session budget alone bound it (tests)
	chat            *chatPolicy // ha-nova serve: the user and the owner's settings at this call; nil in ha-nova mcp
}

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

func (c *verbCall) began() time.Time {
	if c == nil {
		return commandStart
	}
	return c.start
}

func (c *verbCall) answerBy() time.Time {
	if c == nil {
		return checkDeadline
	}
	return c.deadline
}

func (c *verbCall) chatPolicy() *chatPolicy {
	if c == nil {
		return chatCall
	}
	return c.chat
}

// errorf is printErr for this call.
func (c *verbCall) errorf(format string, args ...any) {
	fmt.Fprintf(c.stderr(), "[ha-nova] ERROR: "+format+"\n", args...)
}

// runCaptured runs a verb as call (nil: a new one) with its own output and
// returns what it wrote. A panic becomes exit verbExitUnknown without
// output: a change may have been sent (runLocked).
func runCaptured(call *verbCall, run func(*verbCall) int) (out, errOut string, exit int) {
	if call == nil {
		call = &verbCall{}
	}
	var outBuf, errBuf callBuffer
	var crash string
	func() {
		defer func() {
			if p := recover(); p != nil {
				exit, crash = verbExitUnknown, fmt.Sprint("internal error: ", p)
			}
		}()
		call.out, call.err = &outBuf, &errBuf
		exit = run(call)
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
