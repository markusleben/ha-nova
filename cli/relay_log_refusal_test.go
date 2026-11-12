package main

import (
	"errors"
	"strings"
	"testing"
)

// Logs stay in Home Assistant (Markus, 2026-11-07): every way to read one,
// its lines as events included, is refused before anything is sent, at the
// CLI and at the gate alike, and a plan names the log page, not a read tool.
// The one exception, the exact log facts read, passes the gate as a read (its
// answer is facts, log_facts_test.go) and still never goes through the raw
// guard or a raw plan.
func TestLogReadsAreRefusedEverywhere(t *testing.T) {
	for _, tc := range []struct{ endpoint, body string }{
		{"core", `{"method":"GET","path":"/api/error_log"}`},
		{"core", `{"method":"GET","path":"/api/%65rror_log?x=1"}`},
		{"ws", `{"type":"system_log/list","limit":5}`},
		{"ws", `{"message":{"type":"subscribe_events","event_type":"system_log_event"},"collect_events":{"max_events":1}}`},
		{"ws", `{"message":{"type":"subscribe_events","event_type":"SYSTEM_LOG_EVENT"},"collect_events":{"max_events":1}}`},
		{"ws", `{"message":{"type":"subscribe_trigger","trigger":{"trigger":"event","event_type":["state_changed","system_log_event"]}},"collect_events":{"max_events":1}}`},
		{"supervisor", `{"method":"GET","path":"/core/logs"}`},
		{"supervisor", `{"method":"GET","path":"/addons/core_mosquitto/logs","lines":5}`},
	} {
		if _, refusal, err := guardRaw(tc.endpoint, []byte(tc.body), nil); err != nil || refusal == nil || !strings.Contains(refusal.Error(), "Settings > System > Logs") {
			t.Errorf("%s %s: guard %v %v", tc.endpoint, tc.body, refusal, err)
		}
		check, err := gateVerdict(tc.endpoint, []byte(tc.body), func() (*relaySession, error) { return nil, errors.New("no session") })
		if err != nil || check.Verdict != gateRefuse {
			t.Errorf("%s %s: gate %+v %v", tc.endpoint, tc.body, check, err)
		}
	}
	facts := `{"type":"system_log/list"}`
	if _, refusal, err := guardRaw("ws", []byte(facts), nil); err != nil || refusal == nil {
		t.Errorf("log facts read: guard %v %v", refusal, err)
	}
	if check, err := gateVerdict("ws", []byte(facts), func() (*relaySession, error) { return nil, errors.New("no session") }); err != nil || check.Verdict != gatePass || check.Kind != "read" {
		t.Errorf("log facts read: gate %+v %v", check, err)
	}
	paths, calls := readRelay(t, nil)
	c := startMCP(t, paths)
	mcpInit(c)
	env, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "core", "payload": map[string]any{"method": "GET", "path": "/api/error_log"}}}}))
	errInfo, _ := env["error"].(map[string]any)
	message, _ := errInfo["message"].(string)
	next, _ := errInfo["next"].(string)
	if code != "REFUSED" || !strings.Contains(message, "Settings > System > Logs") || strings.Contains(next, "ha_read") || len(calls()) != 0 {
		t.Errorf("plan raw: %s %q %q, sent %v", code, message, next, calls())
	}
	env, code = readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "system_log/list"}}}}))
	if code != "REFUSED" || len(calls()) != 0 {
		t.Errorf("plan raw log facts: %s %v, sent %v", code, env, calls())
	}
}
