package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"time"
)

// In the remote chat bridge (ha-nova serve) ha_read runs a jq filter in a
// child process with its own memory limit: one allocation such as
// "x" * 1e9 can't be stopped by the deadline, and the bridge's container
// would OOM-kill every user's session. Local ha-nova mcp, like the CLI, runs
// the filter in process with the same deadline and output cap.

const (
	jqChildCommand = "__jq-eval"        // hidden: no help, no usage
	jqChildEnv     = "HA_NOVA_JQ_CHILD" // set by the parent; the child refuses without it
	jqChildMemory  = 64 << 20           // what a filter may add to what the child maps at start (mcp_read_jq_linux.go)
)

// jqRun runs ha_read's filter on one input.
type jqRun func(input []byte) (jqResult, error)

var errJQChildFailed = errors.New("the jq child stopped")

// jqChildReply is the child's answer on stdout; code is "too_large",
// "deadline", or "error" (message in error).
type jqChildReply struct {
	Output string `json:"output"`
	Code   string `json:"code,omitempty"`
	Error  string `json:"error,omitempty"`
}

// jqChildCmd starts the child (a variable for tests).
var jqChildCmd = func(ctx context.Context) (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, executable, jqChildCommand)
	cmd.Env = []string{jqChildEnv + "=1"} // nothing else: the child needs no credential, path, or proxy
	return cmd, nil
}

// inProcessJQ: local ha-nova mcp, bounded in time and output only.
func inProcessJQ(program *jqProgram) jqRun {
	return func(input []byte) (jqResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), mcpJQDeadline)
		defer cancel()
		return applyCompiledJQFilterWithin(ctx, program, input, false, mcpReadMaxResult)
	}
}

// childJQ: the bridge's runner. Stdin is the filter as one JSON line, then
// the input; the child is killed one second after the deadline. Its stderr
// is dropped: a crash is reported only as errJQChildFailed.
func childJQ(filter string) jqRun {
	return func(input []byte) (jqResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), mcpJQDeadline+time.Second)
		defer cancel()
		cmd, err := jqChildCmd(ctx)
		if err != nil {
			return jqResult{}, errJQChildFailed
		}
		line, _ := json.Marshal(filter)
		cmd.Stdin = io.MultiReader(bytes.NewReader(append(line, '\n')), bytes.NewReader(input))
		var out bytes.Buffer
		cmd.Stdout = &limitedWriter{w: &out, left: 2*mcpReadMaxResult + 4096}
		cmd.WaitDelay = time.Second
		var reply jqChildReply
		if cmd.Run() != nil || json.Unmarshal(out.Bytes(), &reply) != nil {
			return jqResult{}, errJQChildFailed
		}
		switch reply.Code {
		case "too_large":
			return jqResult{}, errJQOutputLimit
		case "deadline":
			return jqResult{}, context.DeadlineExceeded
		case "error":
			return jqResult{}, errors.New(reply.Error)
		}
		return jqResult{output: reply.Output}, nil
	}
}

// limitedWriter keeps at most left bytes and fails after that.
type limitedWriter struct {
	w    io.Writer
	left int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > l.left {
		return 0, errors.New("jq child output over the limit")
	}
	l.left -= len(p)
	return l.w.Write(p)
}

// maybeRunJQChild is the child: memory limit first, then the filter under
// the same deadline and output cap as in process.
func maybeRunJQChild(args []string, input io.Reader, output io.Writer) (bool, int) {
	if len(args) != 1 || args[0] != jqChildCommand {
		return false, 0
	}
	if os.Getenv(jqChildEnv) != "1" {
		return true, 2
	}
	if limitJQChildMemory(jqChildMemory) != nil { // never run a filter uncapped
		return true, 2
	}
	debug.SetMemoryLimit(jqChildMemory)
	reader := bufio.NewReader(input)
	line, err := reader.ReadBytes('\n')
	var filter string
	if err != nil || json.Unmarshal(line, &filter) != nil || len(filter) > mcpJQMax {
		return true, 2
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return true, 2
	}
	reply := jqChildReply{}
	program, err := compileJQFilter(filter)
	if err == nil {
		var res jqResult
		res, err = inProcessJQ(program)(data)
		reply.Output = res.output
	}
	switch {
	case errors.Is(err, errJQOutputLimit) || err == nil && len(reply.Output) > mcpReadMaxResult+1:
		reply = jqChildReply{Code: "too_large"}
	case errors.Is(err, context.DeadlineExceeded):
		reply = jqChildReply{Code: "deadline"}
	case err != nil:
		reply = jqChildReply{Code: "error", Error: mcpMessage(err.Error(), mcpMessageMax)}
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false) // "<" stays one byte: the parent caps the reply's size
	if encoder.Encode(reply) != nil {
		return true, 1
	}
	return true, 0
}
