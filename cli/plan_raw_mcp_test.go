package main

import (
	"strings"
	"testing"
)

// ha_plan kind raw takes its requests as a checked array of objects: no
// argument becomes a CLI flag, and a malformed item never reaches the verb.
func TestMCPPlanRawArguments(t *testing.T) {
	paths, sent := rawRelay(t, nil)
	c := startMCP(t, paths)
	mcpInit(c)
	item := map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "input_boolean/create", "name": "Guest mode"}}
	many := make([]any, 21)
	for i := range many {
		many[i] = item
	}
	for _, args := range []map[string]any{
		{"kind": "raw"},
		{"kind": "raw", "requests": []any{}},
		{"kind": "raw", "requests": many},
		{"kind": "raw", "requests": []any{map[string]any{"endpoint": "trace", "payload": map[string]any{}}}},
		{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": "--server=x"}}},
		{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws"}}},
		{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": map[string]any{}, "file": "x"}}},
		{"kind": "raw", "requests": []any{"--help"}},
		{"kind": "call", "service": "light.turn_on", "requests": []any{item}},
	} {
		if env, code := readEnvelope(c.tool("ha_plan", args)); code != "INVALID_ARGUMENTS" {
			t.Errorf("%v: %v", args, env)
		}
	}
	res := c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{item}})
	env, code := readEnvelope(res)
	plan, _ := env["plan"].(map[string]any)
	if code != "" || env["status"] != "needs_confirmation" || plan == nil || !strings.Contains(env["say"].(string), "“Guest mode”") {
		t.Fatalf("plan: %v", env)
	}
	if env, code := readEnvelope(c.tool("ha_apply", map[string]any{"plan_id": plan["id"]})); code != "" || env["status"] != "accepted" {
		t.Fatalf("apply: %v", env)
	}
	if got := strings.Join(sent(), " | "); got != "input_boolean/create" {
		t.Fatalf("sent %s", got)
	}
}

// Chat apps set to read only refuse kind raw like every plan, and ha_read
// points no change to a plan there.
func TestServeReadOnlyRefusesRawPlans(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"],"read_only":true}`)
	env := s.tool(userA, "ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "input_boolean/create", "name": "A"}}}})
	if envCode(env) != "READ_ONLY" {
		t.Fatalf("ha_plan kind raw: %v", env)
	}
	for _, args := range []map[string]any{
		{"endpoint": "ws", "payload": map[string]any{"type": "input_boolean/create", "name": "A"}},
		{"endpoint": "files", "payload": map[string]any{"action": "write_file", "path": "packages/a.yaml", "content": "a: 1"}},
		{"endpoint": "ws", "payload": map[string]any{"type": "camera/stream", "entity_id": "camera.door"}},
	} {
		env := s.tool(userA, "ha_read", args)
		message, _ := env["error"].(map[string]any)["message"].(string)
		if envCode(env) != "REFUSED" || !strings.Contains(message, "read only") || strings.Contains(message, "ha_plan") {
			t.Errorf("%v: %v", args, env)
		}
	}
	if len(s.posts()) != 0 {
		t.Fatalf("posts %d", len(s.posts()))
	}
}

// A typed raw plan is confirmed in the window like any typed plan; the
// window shows each step.
func TestPlanRawConfirmsInTheWindow(t *testing.T) {
	paths, sent := rawRelay(t, nil)
	w := useWindow(t, windowConfirmed)
	_, env := planRaw(t, paths, `[{"endpoint":"backups","payload":{"action":"save","category":"helpers","name":"auto-guest","data":{}}},
		{"endpoint":"ws","payload":{"type":"input_boolean/delete","input_boolean_id":"guest"}}]`)
	if env.Plan == nil || env.Plan.Channel != channelWindow || env.Plan.Code != "" || !strings.HasSuffix(env.Say, sayWindowAsk) {
		t.Fatalf("plan %+v say %q", env.Plan, env.Say)
	}
	exit, applied := applyRaw(t, paths, env)
	if exit != 0 || len(w.messages) != 1 || !strings.Contains(w.messages[0], "2. Delete the toggle helper “guest”.") || applied.Plan.Channel != channelWindow {
		t.Fatalf("apply: exit %d %+v windows %q", exit, applied.Error, w.messages)
	}
	if got := strings.Join(sent(), " | "); got != "backups save | input_boolean/delete" {
		t.Fatalf("sent %s", got)
	}
}
