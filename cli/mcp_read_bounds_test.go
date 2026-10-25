package main

import (
	"strings"
	"testing"
	"time"
)

// A jq filter that never ends, or never stops printing, returns promptly
// with an error: ha_read runs under mcpVerbLock.
func TestMCPReadJQIsBounded(t *testing.T) {
	prev := mcpJQDeadline
	mcpJQDeadline = 300 * time.Millisecond
	t.Cleanup(func() { mcpJQDeadline = prev })
	paths, _ := readRelay(t, func(endpoint string, body map[string]any) any {
		if body["type"] == "get_states" {
			return states(2000)
		}
		return nil
	})
	c := startMCP(t, paths)
	mcpInit(c)
	for filter, want := range map[string]string{"repeat(0)": "RESULT_TOO_LARGE", "def f: f; f": "JQ_FAILED", "[range(1e9)]": "JQ_FAILED", "[limit(1e9; repeat(1))]": "JQ_FAILED"} {
		start := time.Now()
		env, code := readEnvelope(c.tool("ha_read", map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "get_states"}, "jq": filter}))
		if took := time.Since(start); code != want || took > 5*time.Second {
			t.Errorf("jq %s: %s after %s: %v", filter, code, took, env["error"])
		}
	}
	// An error value is the whole input: the message stays short and redacted.
	env, code := readEnvelope(c.tool("ha_read", map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "get_states"}, "jq": ".data | tojson | error"}))
	message, _ := env["error"].(map[string]any)["message"].(string)
	if code != "JQ_FAILED" || len(message) > mcpMessageMax+8 || !strings.HasPrefix(message, "jq: ") {
		t.Fatalf("jq error(v): %s %d %q", code, len(message), message)
	}
}

// Home Assistant's reason comes back with an HTTP error; Relay and verb
// error texts are short and hide live phone confirmation ids.
func TestMCPReadErrorReasons(t *testing.T) {
	long := strings.Repeat("x", 5000)
	paths, _ := readRelay(t, func(endpoint string, body map[string]any) any {
		switch {
		case body["path"] == "/api/template":
			return map[string]any{"status": 400, "body": map[string]any{"message": "Error rendering template: UndefinedError: 'foo' is undefined"}}
		case body["path"] == "/api/error_log":
			return map[string]any{"status": 500, "body": long}
		case body["type"] == "system_log/list":
			return fakeRelayFailure{"UPSTREAM_ERROR", "boom " + long}
		case body["type"] == "logger/log_info":
			return fakeRelayFailure{"UPSTREAM_ERROR", "echo HANOVA_OK_0123456789abcdef"}
		}
		return nil
	})
	c := startMCP(t, paths)
	mcpInit(c)
	check := func(args map[string]any, code, says string, max int) {
		t.Helper()
		env, got := readEnvelope(c.tool("ha_read", args))
		errInfo, _ := env["error"].(map[string]any)
		message, _ := errInfo["message"].(string)
		if got != code || !strings.Contains(message, says) || len(message) > max || strings.Contains(message, "HANOVA_OK_") {
			t.Errorf("%v: %s %d %.200q", args, got, len(message), message)
		}
	}
	check(map[string]any{"endpoint": "core", "method": "POST", "path": "/api/template", "payload": map[string]any{"template": "{{ foo.bar }}"}}, "UPSTREAM_400", "HTTP 400: Error rendering template: UndefinedError", 300)
	check(map[string]any{"endpoint": "core", "path": "/api/error_log"}, "UPSTREAM_500", "HTTP 500: xxx", mcpReasonMax+64)
	check(map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "system_log/list"}}, "READ_FAILED", "boom", mcpMessageMax+8)
	check(map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "logger/log_info"}}, "READ_FAILED", "hidden", mcpMessageMax)
	env, _ := readEnvelope(c.tool("ha_read", map[string]any{"endpoint": "core", "method": "POST", "path": "/api/template", "payload": map[string]any{"template": "x"}}))
	if next, _ := env["error"].(map[string]any)["next"].(string); !strings.Contains(next, "Home Assistant's reason") {
		t.Fatalf("next: %v", env)
	}
}

// The size refusal names a filter that fits the endpoint.
func TestMCPReadTooLargeHints(t *testing.T) {
	big := strings.Repeat("line\n", 20000)
	paths, _ := readRelay(t, func(endpoint string, body map[string]any) any {
		switch endpoint {
		case "files":
			return map[string]any{"content": big, "size": len(big)}
		case "backups":
			return states(2000)
		}
		return map[string]any{"status": 200, "body": big}
	})
	c := startMCP(t, paths)
	mcpInit(c)
	for hint, args := range map[string]map[string]any{
		"split(":                 {"endpoint": "core", "path": "/api/error_log"},
		".data.content[0:60000]": {"endpoint": "files", "payload": map[string]any{"action": "read_file", "path": "/config/configuration.yaml"}},
		".data[0:20]":            {"endpoint": "backups", "payload": map[string]any{"action": "list"}},
	} {
		env, code := readEnvelope(c.tool("ha_read", args))
		if next, _ := env["error"].(map[string]any)["next"].(string); code != "RESULT_TOO_LARGE" || !strings.Contains(next, hint) {
			t.Errorf("%v: %s %q", args, code, next)
		}
	}
}

// The hosted demo shares one administrator: its log, with other visitors'
// failed sign-ins, stays out of ha_read there.
func TestMCPReadDemoKeepsTheLog(t *testing.T) {
	for _, tc := range []struct {
		endpoint, body string
		refused        bool
	}{
		{"core", `{"method":"GET","path":"/api/error_log"}`, true},
		{"core", `{"method":"GET","path":"/api/error_log?x=1"}`, true},
		{"ws", `{"type":"system_log/list"}`, true},
		{"ws", `{"message":{"type":"system_log/list"},"collect_events":{}}`, true},
		{"core", `{"method":"GET","path":"/api/states"}`, false},
		{"ws", `{"type":"get_states"}`, false},
	} {
		if refusal := mcpDemoRefusal(true, tc.endpoint, []byte(tc.body)); (refusal != nil) != tc.refused || refusal != nil && !strings.Contains(refusal.Error(), "other visitors") {
			t.Errorf("demo %s %s: %v", tc.endpoint, tc.body, refusal)
		}
		if refusal := mcpDemoRefusal(false, tc.endpoint, []byte(tc.body)); refusal != nil {
			t.Errorf("home %s %s: %v", tc.endpoint, tc.body, refusal)
		}
	}
}
