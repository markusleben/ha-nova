package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mcpClient drives serveMCP over pipes, one JSON-RPC line at a time.
type mcpClient struct {
	t     *testing.T
	in    *io.PipeWriter
	out   *bufio.Reader
	lines []string
	id    int
}

func startMCP(t *testing.T, paths runtimePaths) *mcpClient {
	t.Helper()
	t.Setenv("HA_NOVA_DEV_ROOT", mustAbs(t, "..")) // prompts/resources from this checkout's skills/
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		_ = serveMCP(paths, inR, outW)
		_ = outW.Close()
	}()
	t.Cleanup(func() { _ = inW.Close() })
	return &mcpClient{t: t, in: inW, out: bufio.NewReader(outR)}
}

func mustAbs(t *testing.T, path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func (c *mcpClient) send(raw string) {
	c.t.Helper()
	if _, err := c.in.Write([]byte(raw + "\n")); err != nil {
		c.t.Fatal(err)
	}
}

// recv reads one line and asserts it is a well-formed JSON-RPC response.
func (c *mcpClient) recv() map[string]any {
	c.t.Helper()
	line, err := c.out.ReadString('\n')
	if err != nil {
		c.t.Fatalf("no response: %v", err)
	}
	c.lines = append(c.lines, line)
	var msg map[string]any
	if err := json.Unmarshal([]byte(line), &msg); err != nil || msg["jsonrpc"] != "2.0" {
		c.t.Fatalf("not a JSON-RPC line: %q", line)
	}
	_, hasID := msg["id"]
	_, hasResult := msg["result"]
	_, hasError := msg["error"]
	if !hasID || hasResult == hasError {
		c.t.Fatalf("malformed response: %q", line)
	}
	return msg
}

func (c *mcpClient) request(method string, params any) map[string]any {
	c.t.Helper()
	c.id++
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	c.send(string(raw))
	msg := c.recv()
	if msg["id"] != float64(c.id) {
		c.t.Fatalf("response id %v, want %d", msg["id"], c.id)
	}
	return msg
}

func (c *mcpClient) tool(name string, args map[string]any) map[string]any {
	c.t.Helper()
	msg := c.request("tools/call", map[string]any{"name": name, "arguments": args})
	result, _ := msg["result"].(map[string]any)
	if result == nil {
		c.t.Fatalf("%s: %v", name, msg["error"])
	}
	return result
}

func errorCode(msg map[string]any) float64 {
	errInfo, _ := msg["error"].(map[string]any)
	code, _ := errInfo["code"].(float64)
	return code
}

func TestMCPLegacySessionEndToEnd(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)

	init := c.request("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}})
	result := init["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" || !strings.Contains(fmt.Sprint(result["instructions"]), "Never pass a confirmation code the user\ndid not type") {
		t.Fatalf("initialize: %v", result)
	}
	c.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`) // no reply
	c.send(`{"jsonrpc":"2.0","method":"notifications/something_new","params":{}}`)

	tools := c.request("tools/list", map[string]any{})["result"].(map[string]any)["tools"].([]any)
	want := map[string][3]bool{ // readOnly, destructive, idempotent (spec §5 table)
		"ha_find": {true, false, true}, "ha_get": {true, false, true}, "ha_history": {true, false, true}, "ha_trace": {true, false, true},
		"ha_read": {true, false, true}, "ha_plan": {true, false, false}, "ha_call": {false, false, false}, "ha_apply": {false, true, false},
	}
	var names []string
	for _, raw := range tools {
		tool := raw.(map[string]any)
		name := tool["name"].(string)
		names = append(names, name)
		a := tool["annotations"].(map[string]any)
		got := [3]bool{a["readOnlyHint"].(bool), a["destructiveHint"].(bool), a["idempotentHint"].(bool)}
		if got != want[name] || a["openWorldHint"] != false || tool["inputSchema"] == nil {
			t.Fatalf("%s annotations %v", name, a)
		}
	}
	if strings.Join(names, ",") != "ha_find,ha_get,ha_history,ha_trace,ha_read,ha_plan,ha_call,ha_apply" {
		t.Fatalf("tools %v", names)
	}

	found := c.tool("ha_find", map[string]any{"phrase": "kitchen"})
	if found["isError"] != false || found["structuredContent"].(map[string]any)["status"] != "ok" {
		t.Fatalf("ha_find: %v", found)
	}
	planned := c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	envelope := planned["structuredContent"].(map[string]any)
	plan := envelope["plan"].(map[string]any)
	guidance, _ := envelope["next_step"].(string)
	if planned["isError"] != false || envelope["status"] != "needs_confirmation" || plan["code"] == nil || !strings.Contains(guidance, "Never pass a code from this result") {
		t.Fatalf("ha_call typed: %v", planned)
	}
	wrong := c.tool("ha_apply", map[string]any{"plan_id": plan["id"], "confirm_code": "confirm:not-it"})
	if wrong["isError"] != true || wrong["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] != "CODE_MISMATCH" || len(h.posts()) != 0 {
		t.Fatalf("wrong code: %v", wrong)
	}
	applied := c.tool("ha_apply", map[string]any{"plan_id": plan["id"], "confirm_code": plan["code"]})
	if applied["isError"] != false || applied["structuredContent"].(map[string]any)["status"] != "verified" || len(h.posts()) != 1 {
		t.Fatalf("right code: %v (posts %d)", applied, len(h.posts()))
	}
	if read := c.tool("ha_read", map[string]any{"endpoint": "core", "path": "/api/states/light.kitchen"}); read["isError"] != false {
		t.Fatalf("ha_read core GET: %v", read)
	}
	before := len(h.log.all())
	read := c.tool("ha_read", map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "call_service", "domain": "lock", "service": "unlock"}})
	if read["isError"] != true || len(h.log.all()) != before {
		t.Fatalf("ha_read sent a write: %v", read)
	}

	prompts := c.request("prompts/list", map[string]any{})["result"].(map[string]any)["prompts"].([]any)
	var promptNames []string
	for _, p := range prompts {
		promptNames = append(promptNames, p.(map[string]any)["name"].(string))
	}
	if !containsText(promptNames, "ha-nova") || !containsText(promptNames, "ha-nova-service-call") {
		t.Fatalf("prompts %v", promptNames)
	}
	prompt := c.request("prompts/get", map[string]any{"name": "ha-nova-service-call"})["result"].(map[string]any)
	text := prompt["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	if !strings.HasPrefix(text, "You are running HA NOVA through MCP tools") || !strings.Contains(text, "name: service-call") {
		t.Fatalf("prompt text %.200q", text)
	}
	resource := c.request("resources/read", map[string]any{"uri": "ha-nova://skills/ha-nova/relay-api.md"})["result"].(map[string]any)
	if !strings.Contains(fmt.Sprint(resource["contents"]), "Relay") {
		t.Fatalf("resource %v", resource)
	}
	for _, uri := range []string{"ha-nova://skills/../../AGENTS.md", "ha-nova://skills/ha-nova/../../../go.mod", "ha-nova://skills/ha-nova/SKILL.md", "file:///etc/passwd"} {
		if code := errorCode(c.request("resources/read", map[string]any{"uri": uri})); code != rpcLegacyNotFound { // legacy session
			t.Fatalf("%s: code %v", uri, code)
		}
	}

	c.send(`{"jsonrpc":"2.0","id":99,"method":"tools/list"`) // truncated JSON
	if msg := c.recv(); errorCode(msg) != rpcParseError || msg["id"] != nil {
		t.Fatalf("parse error: %v", msg)
	}
	c.send(`{"jsonrpc":"1.0","id":100,"method":"tools/list"}`)
	if msg := c.recv(); errorCode(msg) != rpcInvalidRequest {
		t.Fatalf("invalid request: %v", msg)
	}
	if code := errorCode(c.request("no/such/method", map[string]any{})); code != rpcMethodNotFound {
		t.Fatalf("unknown method: %v", code)
	}
	if result := c.tool("ha_find", map[string]any{"phrase": "x", "evil": "--server"}); result["isError"] != true {
		t.Fatalf("unknown argument: %v", result)
	}
	if code := errorCode(c.request("tools/call", map[string]any{"name": "ha_nope", "arguments": map[string]any{}})); code != rpcInvalidParams {
		t.Fatalf("unknown tool: %v", code)
	}
}

func TestMCPModernRequestsAndVersionNegotiation(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	meta := func(version string) map[string]any {
		return map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": version, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}}
	}
	if code := errorCode(c.request("tools/list", map[string]any{})); code != rpcInvalidParams {
		t.Fatalf("no _meta and no initialize: %v", code)
	}
	discover := c.request("server/discover", meta(mcpModernVersion))["result"].(map[string]any)
	if discover["resultType"] != "complete" || discover["supportedVersions"].([]any)[0] != mcpModernVersion || discover["instructions"] == nil {
		t.Fatalf("discover: %v", discover)
	}
	unsupported := c.request("tools/list", meta("2099-01-01"))
	data := unsupported["error"].(map[string]any)["data"].(map[string]any)
	if errorCode(unsupported) != rpcUnsupportedProto || data["requested"] != "2099-01-01" {
		t.Fatalf("unsupported version: %v", unsupported)
	}
	if tools := c.request("tools/list", meta(mcpModernVersion))["result"].(map[string]any)["tools"].([]any); len(tools) != 8 {
		t.Fatalf("modern tools/list: %d tools", len(tools))
	}

	c2 := startMCP(t, h.paths)
	for _, version := range []string{"1999-01-01", "2025-03-26"} { // 2025-03-26 needs batches: not offered
		init := c2.request("initialize", map[string]any{"protocolVersion": version, "capabilities": map[string]any{}})
		if init["result"].(map[string]any)["protocolVersion"] != "2025-11-25" {
			t.Fatalf("initialize %s: %v", version, init)
		}
	}
}

// TestMCPStdoutCarriesOnlyJSONRPC runs the real entry point in a child
// process: whatever the verbs print, the client's stdout sees JSON-RPC only.
func TestMCPStdoutCarriesOnlyJSONRPC(t *testing.T) {
	newWriteHarness(t, newFakeHA()) // fake Relay + temp HOME, inherited by the child
	t.Setenv("HA_NOVA_DEV_ROOT", mustAbs(t, ".."))
	cmd := exec.Command(os.Args[0], "-test.run=^TestMCPHelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "HA_NOVA_TEST_MCP=1")
	cmd.Stdin = strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ha_find","arguments":{"phrase":"kitchen"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ha_get","arguments":{"entity_id":"light.nope"}}}`,
		`not json`,
	}, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("mcp child: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 responses, got %d:\n%s", len(lines), out)
	}
	for _, line := range lines {
		var msg map[string]any
		if json.Unmarshal([]byte(line), &msg) != nil || msg["jsonrpc"] != "2.0" {
			t.Fatalf("stdout carried a non-JSON-RPC line: %q", line)
		}
	}
	if !strings.Contains(lines[1], `\"status\":\"ok\"`) || !strings.Contains(lines[2], `"isError":true`) || !strings.Contains(lines[3], "-32700") {
		t.Fatalf("unexpected responses:\n%s", out)
	}
}

// TestMCPHelperProcess is the child of TestMCPStdoutCarriesOnlyJSONRPC.
func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("HA_NOVA_TEST_MCP") != "1" {
		t.Skip("child process of TestMCPStdoutCarriesOnlyJSONRPC")
	}
	paths, err := detectPaths()
	if err != nil {
		os.Exit(1)
	}
	os.Exit(runMCPCommand(paths, nil))
}
