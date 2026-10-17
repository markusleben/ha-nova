package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// rawRelayCounter is a Relay that counts every request it receives.
func rawRelayCounter(t *testing.T) (runtimePaths, *atomic.Int32) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))
	var hits atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"data":{"status":200,"body":[]}}`)
	}))
	t.Cleanup(relay.Close)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: relay.URL}); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayAuthToken("test-token"); err != nil {
		t.Fatal(err)
	}
	return paths, &hits
}

// The reproduced bypass: a raw unlock reached Home Assistant with no plan.
func TestRawRelayRefusesServiceCalls(t *testing.T) {
	paths, hits := rawRelayCounter(t)
	cases := [][]string{
		{"core", "--method", "POST", "--path", "/api/services/lock/unlock", "--body", `{"entity_id":"lock.front_door"}`},
		{"core", "--method", "post", "--path", "/api/services/lock/unlock?return_response"},
		{"core", "--method", "POST", "--path", "/api/%73ervices/lock/unlock"},
		{"core", "--method", "POST", "--path", "/api/%2573ervices/cover/open_cover"},
		{"core", "--method", "POST", "--path", "/api/x/../services/lock/unlock"},
		{"ws", "--data", `{"type":"call_service","domain":"lock","service":"unlock","target":{"entity_id":"lock.front_door"}}`},
		{"ws", "--data", `{"type":" Call_Service ","domain":"lock","service":"unlock"}`},
		{"ws", "--data", `{"type":"execute_script","sequence":[{"action":"lock.unlock"}]}`},
		{"ws", "--data", `{"message":{"type":"call_service","domain":"lock","service":"unlock"},"collect_events":{"max_events":1}}`},
	}
	for _, args := range cases {
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, args) })
		if exit != verbExitRefused || !strings.Contains(stderr, "nothing was sent") || !strings.Contains(stderr, "ha-nova call") ||
			strings.Contains(stderr, "ha-nova setup") {
			t.Errorf("%v: exit %d, stderr %q; want a refusal naming ha-nova call", args, exit, stderr)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the Relay received %d requests; a refused raw request must send nothing", n)
	}
}

func TestRawRelayRefusesEventsAndWebhooks(t *testing.T) {
	rawRefused(t, "core", `{"method":"POST","path":"/api/events/my_event","body":{}}`)
	rawRefused(t, "core", `{"method":"POST","path":"/api/webhook/abc"}`)
	rawRefused(t, "core", `{"method":"GET","path":"/api/webhook/abc"}`)
	rawRefused(t, "ws", `{"type":"fire_event","event_type":"my_event"}`)
	rawRefused(t, "ws", `{"type":"webhook/list"}`)
	rawRefused(t, "ws", `{"type":"webhook/handle","webhook_id":"x","body":"{}"}`)
	rawAllowed(t, "core", `{"method":"GET","path":"/api/events"}`)
}

func TestRawRelayRefusesOtherActuationSurfaces(t *testing.T) {
	for _, path := range []string{"/api/states/person.me", "/api/intent/handle", "/api/conversation/process", "/api/hassio/core/restart",
		"/api/google_assistant", "/api/alexa/smart_home", "/api/mobile_app/registrations", "/api/cloud/google_actions/sync"} {
		rawRefused(t, "core", `{"method":"POST","path":"`+path+`"}`)
	}
	rawRefused(t, "ws", `{"type":"conversation/process","text":"open the garage"}`)
	rawRefused(t, "ws", `{"type":"assist_pipeline/run","start_stage":"intent"}`)
	rawRefused(t, "ws", `{"type":"backup/restore","backup_id":"x"}`)
	rawRefused(t, "ws", `{"type":"supervisor/api","endpoint":"/host/reboot","method":"post"}`)
	rawAllowed(t, "ws", `{"type":"supervisor/api","endpoint":"/addons","method":"get"}`)
	rawAllowed(t, "core", `{"method":"GET","path":"/api/states/lock.front_door"}`)
}

func TestRawRelayRefusesConfigPlanBypass(t *testing.T) {
	rawRefused(t, "core", `{"method":"POST","path":"/api/config/automation/config/1","body":{}}`)
	rawRefused(t, "core", `{"method":"DELETE","path":"/api/config/script/config/leave"}`)
	rawRefused(t, "ws", `{"type":"lovelace/dashboards/delete","dashboard_id":"x"}`)
	rawAllowed(t, "core", `{"method":"GET","path":"/api/config/automation/config/1"}`)
	rawAllowed(t, "core", `{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"trend"}}`)
	rawAllowed(t, "core", `{"method":"POST","path":"/api/config/scene/config/1","body":{}}`)
	rawAllowed(t, "core", `{"method":"POST","path":"/api/template","body":{"template":"{{ 1 }}"}}`)
}

func TestRawRelayRefusesCoverClassOverride(t *testing.T) {
	rawRefused(t, "ws", `{"type":"config/entity_registry/update","entity_id":"cover.garage","device_class":"curtain"}`)
	rawAllowed(t, "ws", `{"type":"config/entity_registry/update","entity_id":"cover.garage","name":"Garage"}`)
	rawAllowed(t, "ws", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.door","device_class":"door"}`)
	rawRefused(t, "ws", `{"type":"blueprint/save","domain":"automation","path":"a.yaml","yaml":"x","allow_override":true}`)
	rawAllowed(t, "ws", `{"type":"blueprint/save","domain":"automation","path":"a.yaml","yaml":"x"}`)
}

func TestRawRelayRefusesCallServiceSubscriptions(t *testing.T) {
	rawRefused(t, "ws", `{"message":{"type":"subscribe_events"},"collect_events":{"max_events":1}}`)
	rawRefused(t, "ws", `{"message":{"type":"subscribe_events","event_type":"call_service"},"collect_events":{"max_events":1}}`)
	rawRefused(t, "ws", `{"message":{"type":"subscribe_events","event_type":"*"},"collect_events":{"max_events":1}}`)
	rawRefused(t, "ws", `{"message":{"type":"subscribe_trigger","trigger":{"platform":"event","event_type":"call_service"}},"collect_events":{}}`)
	rawAllowed(t, "ws", `{"message":{"type":"subscribe_events","event_type":"zha_event"},"collect_events":{"max_events":1}}`)
	rawAllowed(t, "ws", `{"message":{"type":"subscribe_trigger","trigger":{"platform":"state","entity_id":"event.x"}},"collect_events":{}}`)
}

func rawRefused(t *testing.T, endpoint, body string) {
	t.Helper()
	if err := checkRawRelayRequest(endpoint, []byte(body)); err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Errorf("%s %s: got %v, want a refusal", endpoint, body, err)
	}
}

func rawAllowed(t *testing.T, endpoint, body string) {
	t.Helper()
	if err := checkRawRelayRequest(endpoint, []byte(body)); err != nil {
		t.Errorf("%s %s: refused (%v), want allowed", endpoint, body, err)
	}
}

// Codex P1: the Relay parses the core path as a URL, dropping the #fragment;
// the guard must drop it too, or `/api/services/lock/unlock#/../../template`
// reads as /api/template (allowed) while HA gets the unlock.
func TestRawRelayRefusesFragmentPathTricks(t *testing.T) {
	rawRefused(t, "core", `{"method":"POST","path":"/api/services/lock/unlock#/../../../template"}`)
	rawRefused(t, "core", `{"method":"POST","path":"/api/services/lock/unlock?x=1#frag"}`)
	rawRefused(t, "core", `{"method":"POST","path":"/api/events/e#/../../api/template"}`)
	// A real template render still works (fragment dropped, path unchanged).
	rawAllowed(t, "core", `{"method":"POST","path":"/api/template#x"}`)
}

// Codex P1: a JSON number that overflows Go's float64 (but JS JSON.parse
// accepts) must not fail the guard open. UseNumber keeps call_service visible.
func TestRawRelayRefusesNumberOverflow(t *testing.T) {
	rawRefused(t, "ws", `{"type":"call_service","domain":"lock","service":"unlock","id":1e1000}`)
	rawRefused(t, "core", `{"method":"POST","path":"/api/services/lock/unlock","id":99999999999999999999999}`)
}

// The Relay's URL parse strips whitespace and control characters (and turns
// `\` into `/`) before HA sees the path; neither the guard nor the read
// allowlist may check a different path than the one sent.
func TestRawRelayRefusesWhitespaceAndControlPaths(t *testing.T) {
	for _, path := range []string{" /api/services/lock/unlock", "/api/serv\tices/lock/unlock", "/api/services/lock/unlock\n", "\x01/api/events/e"} {
		body, _ := json.Marshal(map[string]any{"method": "POST", "path": path})
		if err := checkRawRelayRequest("core", body); err == nil {
			t.Errorf("%q: allowed, want refused", path)
		}
	}
	for _, path := range []string{"/api/states/.\t./webhook/abc", "/api/states/..\\webhook/abc", " /api/webhook/abc", "/api/states/x#/../../webhook/a"} {
		if readablePath(path) || mcpReadablePath(path) {
			t.Errorf("read allowlist accepted %q", path)
		}
	}
	if !readablePath("/api/states/lock.front_door") || !mcpReadablePath("/api/history/period?filter_entity_id=light.x") {
		t.Error("plain reads must stay allowed")
	}
}
