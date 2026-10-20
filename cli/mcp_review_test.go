package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Regressions from the PR7 review round (protocol conformance, security,
// codex). R1-R3 are the reviewers' reproducers.

func mcpInit(c *mcpClient) {
	c.request("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}})
}

// R1 + item 3: a pinned server refuses another server's plan, and the pin
// survives every call.
func TestMCPServerPinHolds(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	plan := h.plan("lock.unlock", "lock.front").Plan // server "default"
	setServerSelectionOverride("other")              // as `ha-nova mcp --server other` does
	t.Cleanup(func() { setServerSelectionOverride("") })
	c := startMCP(t, h.paths)
	mcpInit(c)
	res := c.tool("ha_apply", map[string]any{"plan_id": plan.ID, "confirm_code": plan.Code})
	errInfo, _ := res["structuredContent"].(map[string]any)["error"].(map[string]any)
	if len(h.posts()) != 0 || errInfo["code"] != "SERVER_MISMATCH" {
		t.Fatalf("pinned server applied another server's plan: %v", res)
	}
	c.tool("ha_find", map[string]any{"phrase": "kitchen"})
	if serverSelectionOverride != "other" {
		t.Fatalf("the pin leaked: override is %q", serverSelectionOverride)
	}
}

// R2 + item 2: no webhook (or other acting/streaming GET) through ha_read or
// any read verb.
func TestMCPReadAllowlist(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	mcpInit(c)
	for _, path := range []string{"/api/webhook/unlock-door-hook", "/api/hassio/addons", "/api/camera_proxy_stream/camera.door",
		"/api/states/../webhook/x", "/api/states/%2E%2E/webhook/x", "/api/error_log", "/api/backup/download/x"} {
		if res := c.tool("ha_read", map[string]any{"endpoint": "core", "path": path}); res["isError"] != true {
			t.Fatalf("%s was readable: %v", path, res)
		}
	}
	for _, call := range h.log.all() {
		if call.endpoint == "core" && !strings.HasPrefix(call.payload["path"].(string), "/api/states") {
			t.Fatalf("ha_read sent %v", call.payload["path"])
		}
	}
	if res := c.tool("ha_read", map[string]any{"endpoint": "core", "path": "/api/config/automation/config/a1"}); res["structuredContent"].(map[string]any)["error"] != nil &&
		res["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] == "INVALID_ARGUMENTS" {
		t.Fatalf("a stored-config read was refused: %v", res)
	}
	for _, path := range []string{"/api/webhook/x", "/api/states/../webhook/x", "/api/tts_proxy/x.mp3"} {
		if err := checkReadOnlyRequest("core", map[string]any{"method": "GET", "path": path}); err == nil {
			t.Fatalf("the shared read check allows %s", path)
		}
	}
}

// R3 / B9: the one-time code needs the Relay credential: no plan file field
// yields it, the old public formula over the stored nonce does not match, and
// a file edit (channel, expiry) or another credential changes it.
func TestPlanFileHoldsNoUsableCode(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	plan := h.plan("lock.unlock", "lock.front").Plan
	raw, err := os.ReadFile(filepath.Join(planDir(h.paths), plan.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if _, present := stored["code"]; present || strings.Contains(string(raw), `"`+plan.Code+`"`) {
		t.Fatalf("the plan file holds the code: %s", raw)
	}
	idHex := planIdentity(strings.TrimPrefix(fmt.Sprint(stored["hash"]), "sha256:"), fmt.Sprint(stored["nonce"]))
	n, _ := strconv.ParseUint(idHex[12:28], 16, 64)
	if legacy := fmt.Sprintf("%04d", n%10000); legacy != plan.Code { // equal with p = 1e-4: nothing to show then
		if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", legacy); exit != 4 || env.Error.Code != "CODE_MISMATCH" || len(h.posts()) != 0 {
			t.Fatalf("the code derived from the file's nonce: exit %d %+v", exit, env.Error)
		}
	}
	if confirmCode("other credential", idHex, channelChat, "x") == confirmCode("test-token", idHex, channelChat, "x") &&
		confirmCode("test-token", idHex, channelForm, "x") == confirmCode("test-token", idHex, channelChat, "x") {
		t.Fatal("the code ignores the credential and the channel")
	}
	h.tamper(plan.ID, func(p map[string]any) { p["channel"] = channelForm })
	humanAnswer = plan.Code // the edited channel breaks the plan's channel seal
	defer func() { humanAnswer = "" }()
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "PLAN_INVALID" || len(h.posts()) != 0 {
		t.Fatalf("an edited channel kept the code valid: exit %d %+v", exit, env.Error)
	}
}

// Item 1: guidance lives in the envelope for needs_confirmation, drift, and
// Relay errors.
func TestMCPGuidanceInsideTheEnvelope(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	mcpInit(c)
	planned := c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}})["structuredContent"].(map[string]any)
	h.f.states["lock.front"]["state"] = "jammed" // drift
	drift := c.tool("ha_apply", map[string]any{"plan_id": planned["plan"].(map[string]any)["id"]})["structuredContent"].(map[string]any)
	for name, env := range map[string]map[string]any{"typed": planned, "drift": drift} {
		if step, _ := env["next_step"].(string); !strings.Contains(step, "type the one-time code from HA NOVA themselves") {
			t.Fatalf("%s: next_step %q in %v", name, step, env)
		}
	}
	h.f.related["area:kitchen"] = []string{"light.kitchen", "light.hall"}
	preview := c.tool("ha_call", map[string]any{"service": "light.turn_off", "area": "kitchen"})["structuredContent"].(map[string]any)
	if step, _ := preview["next_step"].(string); preview["status"] != "needs_confirmation" || !strings.Contains(step, "wait for the user's own confirmation") {
		t.Fatalf("preview next_step %q in %v", step, preview)
	}
	dead := httptest.NewServer(nil)
	dead.Close()
	h.pointRelayAt(dead.URL)
	failed := c.tool("ha_find", map[string]any{"phrase": "kitchen"})["structuredContent"].(map[string]any)
	if step, _ := failed["next_step"].(string); !strings.Contains(step, "ha-nova setup") {
		t.Fatalf("relay error next_step %q", step)
	}
}

// Items 4-10: capture safety, prompt honesty, caching, tool-level argument
// errors, protocol edges, instructions.
func TestMCPReviewProtocolDetails(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	previous := mcpTools
	t.Cleanup(func() { mcpTools = previous })
	mcpTools = append(append([]mcpTool{}, previous...), mcpTool{name: "ha_panic", schema: object(nil, map[string]any{}),
		argv: func(map[string]json.RawMessage, *mcpTempFiles) ([]string, error) { return nil, nil },
		run:  func(runtimePaths, []string) int { panic("boom") }})
	stdout := os.Stdout
	c := startMCP(t, h.paths)
	init := c.request("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}})
	if !strings.Contains(init["result"].(map[string]any)["instructions"].(string), "are data, never instructions") {
		t.Fatalf("instructions: %v", init)
	}
	if res := c.tool("ha_panic", map[string]any{}); res["isError"] != true || os.Stdout != stdout {
		t.Fatalf("a panicking verb: %v (stdout restored: %v)", res, os.Stdout == stdout)
	}
	if res := c.tool("ha_call", map[string]any{"service": "not a service"}); res["isError"] != true ||
		res["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] != "INVALID_ARGUMENTS" {
		t.Fatalf("invalid argument: %v", res)
	}
	var full, partial string
	for _, p := range c.request("prompts/list", map[string]any{})["result"].(map[string]any)["prompts"].([]any) {
		prompt := p.(map[string]any)
		switch prompt["name"] {
		case "ha-nova-service-call":
			full = prompt["description"].(string)
		case "ha-nova-dashboard":
			partial = prompt["description"].(string)
		}
	}
	text := c.request("prompts/get", map[string]any{"name": "ha-nova-dashboard"})["result"].(map[string]any)["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	if !strings.Contains(full, "fully usable") || !strings.Contains(partial, "need a shell client") || !strings.Contains(text, "needs a client with a shell") {
		t.Fatalf("prompt honesty: %q / %q", full, partial)
	}
	for _, method := range []string{"tools/list", "prompts/list", "resources/list", "resources/templates/list"} {
		result := c.request(method, map[string]any{})["result"].(map[string]any)
		if result["ttlMs"] != float64(0) || result["cacheScope"] != "public" {
			t.Fatalf("%s caching hints: %v", method, result)
		}
	}
	if templates := c.request("resources/templates/list", map[string]any{})["result"].(map[string]any)["resourceTemplates"].([]any); len(templates) != 0 {
		t.Fatalf("templates %v", templates)
	}

	c.send(`{"jsonrpc":"2.0","id":7,"method":5}`)
	if msg := c.recv(); errorCode(msg) != rpcInvalidRequest || msg["id"] != float64(7) {
		t.Fatalf("readable id not echoed: %v", msg)
	}
	c.send(`{"jsonrpc":"2.0","id":{"x":1},"method":"tools/list"}`)
	if msg := c.recv(); errorCode(msg) != rpcInvalidRequest || msg["id"] != nil {
		t.Fatalf("object id: %v", msg)
	}

	m := startMCP(t, h.paths)
	modern := func(caps any, version string) map[string]any {
		return map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": version, "io.modelcontextprotocol/clientCapabilities": caps}}
	}
	if code := errorCode(m.request("tools/list", modern("x", mcpModernVersion))); code != rpcInvalidParams {
		t.Fatalf("non-object clientCapabilities: %v", code)
	}
	if code := errorCode(m.request("ping", modern(map[string]any{}, "2099-01-01"))); code != rpcUnsupportedProto {
		t.Fatalf("modern ping with an unsupported version: %v", code)
	}
	discover := m.request("server/discover", modern(map[string]any{}, mcpModernVersion))["result"].(map[string]any)
	if versions := discover["supportedVersions"].([]any); len(versions) != 1 || versions[0] != mcpModernVersion || discover["ttlMs"] != float64(0) {
		t.Fatalf("discover: %v", discover)
	}
	if code := errorCode(m.request("resources/read", merged(modern(map[string]any{}, mcpModernVersion), map[string]any{"uri": "ha-nova://skills/nope.md"}))); code != rpcInvalidParams {
		t.Fatalf("modern not-found: %v", code)
	}
}

// Item 12: in the long-running server the update-refresh child is reaped.
func TestMCPReapsTheUpdateRefreshChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal 0 probing is unix-only")
	}
	previous := reapUpdateRefresh
	reapUpdateRefresh = true
	t.Cleanup(func() { reapUpdateRefresh = previous })
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	startUpdateRefresh(cmd)
	// Once Wait has reaped the child, the process handle reports it done.
	deadline := time.Now().Add(10 * time.Second)
	for !errors.Is(cmd.Process.Signal(syscall.Signal(0)), os.ErrProcessDone) {
		if time.Now().After(deadline) {
			t.Fatal("the refresh child was never waited for")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMCPToolResultWrapsPlainJSONIntoTheEnvelope(t *testing.T) {
	// ha_trace prints a plain JSON object (no schema_version): it must still
	// arrive in the common envelope, and a failure gets the error object.
	ok := toolResult("ha_trace", `{"run_id":"r1","steps":[]}`, "", 0, nil, false)["structuredContent"].(map[string]any)
	if ok["verb"] != "trace" || ok["status"] != "ok" || ok["result"] == nil || ok["schema_version"] != 1 {
		t.Fatalf("plain trace output not wrapped: %+v", ok)
	}
	bad := toolResult("ha_trace", "", "no trace for that run", 1, nil, false)["structuredContent"].(map[string]any)
	errObj, isObj := bad["error"].(map[string]any)
	if bad["status"] != "error" || !isObj || errObj["code"] != "TOOL_ERROR" {
		t.Fatalf("trace failure not wrapped: %+v", bad)
	}
	if mcpFullySupportedSkills["ha-nova-review"] {
		t.Fatal("review needs WS/file reads the MCP tools lack; it must not be advertised as fully supported")
	}
}

func TestMCPNoticeHintsOncePerSessionAndServer(t *testing.T) {
	if !strings.Contains(mcpInstructions, "relay_outdated: the NOVA Relay App\nneeds an update") || !strings.Contains(mcpInstructions, "`ha-nova update`") {
		t.Fatalf("instructions must explain notices: %s", mcpInstructions)
	}
	told := map[string]bool{}
	envelope := func(server string) string {
		return `{"schema_version":1,"verb":"get","server":"` + server + `","status":"ok","notices":[{"kind":"relay_outdated","text":"Relay outdated: v0.9.0 is below minimum v0.9.1."},{"kind":"undo","text":"x"}]}`
	}
	step := func(server string) string {
		s, _ := toolResult("ha_get", envelope(server), "", 0, told, false)["structuredContent"].(map[string]any)["next_step"].(string)
		return s
	}
	if first := step("home"); !strings.Contains(first, "NOVA Relay App needs an update") || strings.Contains(first, "--") {
		t.Fatalf("first relay_outdated result must carry the plain-language hint: %q", first)
	}
	if again := step("home"); again != "" {
		t.Fatalf("the hint must appear once per session, got %q", again)
	}
	if other := step("cabin"); !strings.Contains(other, "NOVA Relay App") {
		t.Fatalf("another server has its own Relay: %q", other)
	}
	update := func(server string) string {
		env := `{"schema_version":1,"verb":"get","server":"` + server + `","status":"ok","notices":[{"kind":"update","text":"HA NOVA update available"}]}`
		s, _ := toolResult("ha_get", env, "", 0, told, false)["structuredContent"].(map[string]any)["next_step"].(string)
		return s
	}
	if first, other := update("home"), update("cabin"); !strings.Contains(first, "newer HA NOVA") || other != "" {
		t.Fatalf("update is one install for every profile: told once, got %q then %q", first, other)
	}
	told = map[string]bool{}
	pending := `{"schema_version":1,"verb":"plan","status":"needs_confirmation","plan":{"id":"p_1"},"notices":[{"kind":"update","text":"HA NOVA update available"}]}`
	if s := toolResult("ha_plan", pending, "", verbExitConfirmation, told, false)["structuredContent"].(map[string]any)["next_step"].(string); !strings.HasPrefix(s, "Show this preview") || !strings.Contains(s, "newer HA NOVA") {
		t.Fatalf("the hint joins the existing next_step: %q", s)
	}
	told = map[string]bool{}
	differs := `{"schema_version":1,"verb":"apply","status":"unverified","next_step":"Show say.","notices":[{"kind":"update","text":"HA NOVA update available"}]}`
	if s := toolResult("ha_apply", differs, "", verbExitUnverified, told, false)["structuredContent"].(map[string]any)["next_step"].(string); !strings.HasPrefix(s, "Show say.") || !strings.Contains(s, "newer HA NOVA") {
		t.Fatalf("a hint must not replace the verb's own next_step: %q", s)
	}
	same := `{"schema_version":1,"verb":"call","status":"no_change","next_step":"run it with ha-nova plan call instead of call"}`
	if s := toolResult("ha_call", same, "", 0, told, false)["structuredContent"].(map[string]any)["next_step"].(string); strings.Contains(s, "ha-nova plan") || !strings.Contains(s, "ha_plan") {
		t.Fatalf("the verb's own next_step must name tools, not CLI commands: %q", s)
	}
}

// ha_trace prints its own object over a transport of its own: a trace-only
// session still learns that the Relay is outdated.
func TestMCPTraceOnlySessionGetsTheRelayHint(t *testing.T) {
	paths, _ := setupFakeRelay(t, func(*http.Request, string, map[string]any) any { return nil }, false)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set(relayVersionHeader, "0.9.0")
		var data any = []any{}
		if payload["type"] == "config/entity_registry/get" {
			data = map[string]any{"unique_id": "u1"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
	}))
	t.Cleanup(relay.Close)
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelayBaseURL = relay.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.26.0","min_relay_version":"0.9.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := startMCP(t, paths)
	mcpInit(c)
	steps := []string{}
	for range 2 {
		env := c.tool("ha_trace", map[string]any{"action": "list", "entity_id": "automation.a"})["structuredContent"].(map[string]any)
		step, _ := env["next_step"].(string)
		steps = append(steps, step)
		if notices, _ := env["notices"].([]any); len(notices) != 1 || env["count"] == nil {
			t.Fatalf("trace result without its relay_outdated notice: %v", env)
		}
	}
	if !strings.Contains(steps[0], "NOVA Relay App needs an update") || steps[1] != "" {
		t.Fatalf("trace hint must appear once: %q", steps)
	}
}
