package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
	start, deadline time.Time    // zero: windowWait and the session budget alone bound it (tests)
	chat            *chatPolicy  // ha-nova serve: the user and the owner's settings at this call; nil in ha-nova mcp
	server          string       // the profile it runs against (mcp --server); "": HA_NOVA_SERVER, else default_server
	serial          bool         // runs under mcpVerbLock (runLocked): its loads set the process's selection, which the confirmation code reads
	relayVersion    atomic.Value // ha_trace: the Relay version header of its last request (relay_outdated)
	// ha-nova serve's apply job (mcp_serve_relay_ask.go): asking is the
	// per-call twin of ha-nova mcp's gateApproval, and outside runs a Relay
	// ask without mcpVerbLock. Both nil elsewhere.
	asking  *atomic.Int32
	outside func(ask func() (approved bool)) error
}

// sawRelayVersion keeps the Relay version a trace request answered with.
func (c *verbCall) sawRelayVersion(version string) {
	if c != nil {
		c.relayVersion.Store(version)
	}
}

// relayVersionSeen: the version sawRelayVersion kept, else "".
func (c *verbCall) relayVersionSeen() string {
	version, _ := c.relayVersion.Load().(string)
	return version
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

// verbConfig loads the configuration a verb run uses. The CLI (nil call)
// selects through the process, --server included; an MCP call selects its
// own profile: its --server, else the call's server, else HA_NOVA_SERVER,
// else default_server. The config names the profile (profileName).
func verbConfig(paths runtimePaths, opts verbOptions) (runtimeConfig, error) {
	if opts.call == nil {
		if opts.serverSet {
			setServerSelectionOverride(opts.server)
		}
		return loadConfig(paths)
	}
	if bridgeRelay != nil {
		return *bridgeRelay, nil
	}
	pick := opts.call.server
	if opts.serverSet {
		pick = opts.server
	}
	if pick != "" {
		return loadSelectedConfig(paths, pick, "--server", opts.call.serial)
	}
	return loadSelectedConfig(paths, serverSelectionFromEnv(), serverSelectionEnvVar, opts.call.serial)
}

// profileName: the profile cfg was loaded for, else the process's.
func (c runtimeConfig) profileName() string {
	if c.profile != "" {
		return c.profile
	}
	return activeServerProfile()
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
