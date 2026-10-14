package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// `ha-nova mcp`: a stdio MCP server (typed-verbs spec §5). Newline-delimited
// JSON-RPC 2.0, stdlib only. Dual-era: modern clients (revision 2026-07-28)
// send the protocol version, client info, and capabilities in every
// request's _meta and may call server/discover; legacy clients (2025-11-25
// and earlier) open with initialize, which scopes legacy semantics to this
// process. Tools run the same verb functions as the CLI, in process.

const mcpUsage = "ha-nova mcp [--server <name>]"

const mcpModernVersion = "2026-07-28"

var mcpLegacyVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const mcpInstructions = `HA NOVA controls one Home Assistant through typed tools.
Read freely with ha_find, ha_get, ha_history, ha_trace, and ha_read.
Change things only with ha_call (devices) or ha_plan followed by ha_apply.
Ordinary everyday actions run immediately and are verified; anything else
returns status needs_confirmation with a preview: show that preview to the
user and wait. A preview plan runs with ha_apply after the user agrees in
their own words. A plan with a confirmation code (locks, gates, garage
doors, alarm disarm, deletes) needs the code typed by the user: pass it to
ha_apply verbatim as confirm_code. Never pass a confirmation code the user
did not type, never invent or repeat one on their behalf, and never retry a
call whose outcome is unknown. Refused calls name the skill that owns them;
the ha-nova-<skill> prompts carry those workflows.`

// JSON-RPC 2.0 error codes, plus MCP's UnsupportedProtocolVersion.
const (
	rpcParseError       = -32700
	rpcInvalidRequest   = -32600
	rpcMethodNotFound   = -32601
	rpcInvalidParams    = -32602
	rpcUnsupportedProto = -32022
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpServer struct {
	paths  runtimePaths
	out    io.Writer
	legacy string // negotiated legacy revision once initialize ran ("" = none)
}

func runMCPCommand(paths runtimePaths, args []string) int {
	var opts verbOptions
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.StringVar(&opts.server, "server", "", "server profile name (or HA_NOVA_SERVER)")
	positional, err := parseVerbArgs(fs, &opts, args, mcpUsage+"\n\nServes the HA NOVA tools over MCP (stdio). Configure your MCP client with command \"ha-nova\", args [\"mcp\"].")
	if errors.Is(err, errHelpShown) {
		return 0
	}
	if err == nil && len(positional) != 0 {
		err = errors.New("Usage: " + mcpUsage)
	}
	if err != nil {
		printErr("%s", err)
		return 1
	}
	if opts.serverSet {
		setServerSelectionOverride(opts.server) // every tool call uses this profile
	}
	// stdout carries JSON-RPC only: anything else printed in this process
	// (warnings, a stray fmt.Print) goes to stderr instead.
	protocolOut := os.Stdout
	os.Stdout = os.Stderr
	if err := serveMCP(paths, os.Stdin, protocolOut); err != nil {
		printErr("mcp: %s", err)
		return 1
	}
	return 0
}

// serveMCP answers one JSON-RPC message per line until in closes.
func serveMCP(paths runtimePaths, in io.Reader, out io.Writer) error {
	srv := &mcpServer{paths: paths, out: out}
	reader := bufio.NewReaderSize(in, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			srv.handle(trimmed)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (srv *mcpServer) handle(line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		var probe any
		if json.Unmarshal(line, &probe) != nil {
			srv.reply(nil, nil, &rpcError{Code: rpcParseError, Message: "parse error"})
		} else { // valid JSON, not a request object (batches are not part of MCP)
			srv.reply(nil, nil, &rpcError{Code: rpcInvalidRequest, Message: "invalid request"})
		}
		return
	}
	notification := len(req.ID) == 0
	if req.JSONRPC != "2.0" || req.Method == "" || string(req.ID) == "null" {
		if !notification {
			srv.reply(req.ID, nil, &rpcError{Code: rpcInvalidRequest, Message: "invalid request"})
		}
		return
	}
	if notification {
		return // notifications/initialized, cancellations, unknown ones: no reply, ever
	}
	result, rpcErr := srv.dispatch(req)
	srv.reply(req.ID, result, rpcErr)
}

// dispatch checks the era, then routes the method.
func (srv *mcpServer) dispatch(req rpcRequest) (map[string]any, *rpcError) {
	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &params) != nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "params must be an object"}
	}
	version := ""
	_ = json.Unmarshal(params.Meta["io.modelcontextprotocol/protocolVersion"], &version)
	switch {
	case req.Method == "initialize":
		return srv.initialize(req.Params)
	case req.Method == "ping":
		return map[string]any{}, nil
	case version != "" && version != mcpModernVersion:
		return nil, &rpcError{Code: rpcUnsupportedProto, Message: "Unsupported protocol version",
			Data: map[string]any{"supported": append([]string{mcpModernVersion}, mcpLegacyVersions...), "requested": version}}
	case version == "" && srv.legacy == "":
		return nil, &rpcError{Code: rpcInvalidParams, Message: "missing _meta io.modelcontextprotocol/protocolVersion (or an initialize handshake)"}
	case version != "" && params.Meta["io.modelcontextprotocol/clientCapabilities"] == nil:
		return nil, &rpcError{Code: rpcInvalidParams, Message: "missing _meta io.modelcontextprotocol/clientCapabilities"}
	}
	switch req.Method {
	case "server/discover":
		return map[string]any{"supportedVersions": append([]string{mcpModernVersion}, mcpLegacyVersions...),
			"capabilities": mcpCapabilities(), "instructions": mcpInstructions}, nil
	case "tools/list":
		return map[string]any{"tools": mcpToolList()}, nil
	case "tools/call":
		return srv.callTool(req.Params)
	case "prompts/list":
		return srv.listPrompts()
	case "prompts/get":
		return srv.getPrompt(req.Params)
	case "resources/list":
		return srv.listResources()
	case "resources/read":
		return srv.readResource(req.Params)
	}
	return nil, &rpcError{Code: rpcMethodNotFound, Message: "method not found: " + req.Method}
}

// initialize opens the legacy era: the client's revision when supported,
// else the newest legacy one (the client then decides).
func (srv *mcpServer) initialize(raw json.RawMessage) (map[string]any, *rpcError) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(raw, &params) != nil || params.ProtocolVersion == "" {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "initialize needs protocolVersion"}
	}
	srv.legacy = mcpLegacyVersions[0]
	if slices.Contains(mcpLegacyVersions, params.ProtocolVersion) {
		srv.legacy = params.ProtocolVersion
	}
	return map[string]any{"protocolVersion": srv.legacy, "capabilities": mcpCapabilities(),
		"serverInfo": mcpServerInfo(), "instructions": mcpInstructions}, nil
}

func mcpCapabilities() map[string]any {
	return map[string]any{"tools": map[string]any{}, "prompts": map[string]any{}, "resources": map[string]any{}}
}

func mcpServerInfo() map[string]any {
	return map[string]any{"name": "ha-nova", "title": "HA NOVA", "version": strings.TrimPrefix(Version, "v")}
}

// reply writes exactly one JSON-RPC line. Every result carries resultType
// (2026-07-28; legacy clients ignore it) and the server identity.
func (srv *mcpServer) reply(id json.RawMessage, result map[string]any, rpcErr *rpcError) {
	message := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage("null")}
	if len(id) > 0 {
		message["id"] = id
	}
	if rpcErr != nil {
		message["error"] = rpcErr
	} else {
		result["resultType"] = "complete"
		result["_meta"] = map[string]any{"io.modelcontextprotocol/serverInfo": mcpServerInfo()}
		message["result"] = result
	}
	line, err := json.Marshal(message)
	if err != nil {
		line, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": message["id"], "error": rpcError{Code: -32603, Message: fmt.Sprint("cannot encode the result: ", err)}})
	}
	_, _ = srv.out.Write(append(line, '\n'))
}
