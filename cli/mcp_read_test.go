package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type readCall struct{ endpoint, body string }

// readRelay is a Relay that logs every request and answers reply's data
// (nil: an empty core body; a fakeRelayFailure: ok false).
func readRelay(t *testing.T, reply func(endpoint string, body map[string]any) any) (runtimePaths, func() []readCall) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))
	t.Setenv(updateNudgeOptOutEnv, "1")
	var mu sync.Mutex
	var calls []readCall
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		endpoint := strings.TrimPrefix(r.URL.Path, "/")
		mu.Lock()
		calls = append(calls, readCall{endpoint, string(raw)})
		mu.Unlock()
		var data any = map[string]any{"status": 200, "body": []any{}}
		if reply != nil {
			if d := reply(endpoint, body); d != nil {
				data = d
			}
		}
		w.Header().Set("content-type", "application/json")
		if failure, ok := data.(fakeRelayFailure); ok {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]any{"code": failure.code, "message": failure.message}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
	}))
	t.Cleanup(relay.Close)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: relay.URL}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayAuthToken("test-token"); err != nil {
		t.Fatal(err)
	}
	return paths, func() []readCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]readCall(nil), calls...)
	}
}

// readEnvelope returns a tool result's envelope and its error code ("" ok).
func readEnvelope(res map[string]any) (map[string]any, string) {
	env, _ := res["structuredContent"].(map[string]any)
	errInfo, _ := env["error"].(map[string]any)
	code, _ := errInfo["code"].(string)
	return env, code
}

func TestMCPReadEndpoints(t *testing.T) {
	paths, calls := readRelay(t, nil)
	c := startMCP(t, paths)
	mcpInit(c)
	ws := func(payload string) map[string]any {
		var p map[string]any
		_ = json.Unmarshal([]byte(payload), &p)
		return map[string]any{"endpoint": "ws", "payload": p}
	}
	payload := func(endpoint, action string) map[string]any {
		return map[string]any{"endpoint": endpoint, "payload": map[string]any{"action": action, "path": "/config/packages/a.yaml", "file": "automations/a.json.gz", "content": "x"}}
	}
	planIt := mcpPlanIt + "; nothing was sent"
	cases := []struct {
		args       map[string]any
		code, says string // code "" = read and sent to sent
		sent       string
	}{
		{map[string]any{"endpoint": "core", "path": "/api/error_log"}, "", "", "core"},
		{map[string]any{"endpoint": "core", "path": "/api/states", "method": "GET"}, "", "", "core"},
		{map[string]any{"endpoint": "core", "method": "POST", "path": "/api/template", "payload": map[string]any{"template": "{{ 1 }}"}}, "", "", "core"},
		{map[string]any{"endpoint": "core", "method": "POST", "path": "/api/config/core/check_config"}, "", "", "core"},
		{map[string]any{"endpoint": "core", "method": "POST", "path": "/api/config/config_entries/flow", "payload": map[string]any{"handler": "trend"}}, "REFUSED", planIt, ""},
		{map[string]any{"endpoint": "core", "method": "POST", "path": "/api/config/scene/config/1", "payload": map[string]any{}}, "REFUSED", planIt, ""},
		{map[string]any{"endpoint": "core", "method": "POST", "path": "/api/services/lock/unlock"}, "REFUSED", "ha-nova call", ""},
		{map[string]any{"endpoint": "core", "path": "/api/camera_proxy/camera.door"}, "REFUSED", "endpoint camera", ""},
		{map[string]any{"endpoint": "core", "path": "/api/camera_proxy_stream/camera.door"}, "REFUSED", "media proxies, or streams", ""},
		{map[string]any{"endpoint": "core", "path": "/api/tts_proxy/x.mp3"}, "REFUSED", "media proxies, or streams", ""},
		{map[string]any{"endpoint": "core", "path": "/api/config/config_entries/flow/0123abcd"}, "REFUSED", "a GET on a flow step can finish the flow: " + mcpPlanIt, ""},
		{map[string]any{"endpoint": "core", "path": "/api/config/config_entries/options/flow/0123abcd"}, "REFUSED", mcpPlanIt, ""},
		{map[string]any{"endpoint": "core", "path": "/api/webhook/x"}, "REFUSED", "webhook", ""},
		{map[string]any{"endpoint": "core", "path": "/api/states/light%2Ekitchen"}, "REFUSED", "send the plain path", ""},
		{map[string]any{"endpoint": "core", "path": "/api/config/config_entries/flow/0123abcd?x=1"}, "REFUSED", "flow step", ""},
		{map[string]any{"endpoint": "core", "path": "/api/config/config_entries/subentries/flow/0123abcd"}, "REFUSED", "a GET on a flow step can finish the flow", ""},
		{map[string]any{"endpoint": "core", "path": "/api/repairs/issues/fix/0123abcd"}, "REFUSED", "a GET on a flow step can finish the flow", ""},
		{map[string]any{"endpoint": "core", "path": "/api/repairs/issues"}, "", "", "core"},
		{map[string]any{"endpoint": "core", "path": "/api/hassio/addons"}, "REFUSED", "Supervisor", ""},
		{map[string]any{"endpoint": "core", "path": "/api/states", "payload": map[string]any{}}, "INVALID_ARGUMENTS", "core POST only", ""},
		{map[string]any{"endpoint": "core", "method": "DELETE", "path": "/api/states"}, "INVALID_ARGUMENTS", "wrong type", ""},
		{map[string]any{"endpoint": "core"}, "INVALID_ARGUMENTS", "needs path", ""},
		{map[string]any{"endpoint": "core", "path": "/api/states", "entity_id": "camera.x"}, "INVALID_ARGUMENTS", "does not apply to endpoint core", ""},
		{ws(`{"type":"system_log/list"}`), "", "", "ws"},
		{ws(`{"type":"config/entity_registry/list"}`), "", "", "ws"},
		{ws(`{"message":{"type":"subscribe_events","event_type":"zha_event"},"collect_events":{"max_events":1}}`), "", "", "ws"},
		{ws(`{"type":"config/area_registry/create","name":"Garden"}`), "REFUSED", "ha_read only reads; to change something, " + planIt, ""},
		{ws(`{"type":"call_service","domain":"lock","service":"unlock"}`), "REFUSED", "ha-nova call", ""},
		{ws(`{"message":{"type":"subscribe_events","event_type":"call_service"},"collect_events":{"max_events":1}}`), "REFUSED", "subscribe to the one event type", ""},
		{ws(`{"type":"blueprint/import","url":"https://example.com/a.yaml"}`), "REFUSED", "blueprint/import fetches a URL or hands out a live link: " + mcpPlanIt, ""},
		{ws(`{"type":"camera/stream","entity_id":"camera.door"}`), "REFUSED", mcpPlanIt, ""},
		{ws(`{"type":"media_source/resolve_media","media_content_id":"media-source://x"}`), "REFUSED", mcpPlanIt, ""},
		{ws(`{"message":{"type":"media_source/resolve_media"},"collect_events":{"max_events":1}}`), "REFUSED", mcpPlanIt, ""},
		{map[string]any{"endpoint": "ws"}, "INVALID_ARGUMENTS", "needs payload", ""},
		{payload("files", "list_dir"), "", "", "files"},
		{payload("files", "read_file"), "", "", "files"},
		{payload("files", "write_file"), "REFUSED", "list_dir and read_file only; to write or delete one, " + planIt, ""},
		{payload("files", "delete_file"), "REFUSED", "list_dir and read_file only", ""},
		{payload("backups", "list"), "", "", "backups"},
		{payload("backups", "load"), "", "", "backups"},
		{payload("backups", "save"), "REFUSED", "lists and loads config snapshots only; to save, delete, or prune one, " + planIt, ""},
		{payload("backups", "delete"), "REFUSED", "lists and loads config snapshots only", ""},
		{payload("backups", "prune"), "REFUSED", "lists and loads config snapshots only", ""},
		{map[string]any{"endpoint": "camera", "entity_id": "light.kitchen"}, "INVALID_ARGUMENTS", "not a camera's entity_id", ""},
		{map[string]any{"endpoint": "camera", "entity_id": "camera.door", "jq": "."}, "INVALID_ARGUMENTS", "does not apply", ""},
		{map[string]any{"endpoint": "snapshot"}, "INVALID_ARGUMENTS", "needs action", ""},
		{map[string]any{"endpoint": "snapshot", "action": "verify"}, "INVALID_ARGUMENTS", "needs against", ""},
		{map[string]any{"endpoint": "snapshot", "action": "show", "against": map[string]any{}}, "INVALID_ARGUMENTS", "against applies to action verify", ""},
		{map[string]any{"endpoint": "diff", "before": map[string]any{}}, "INVALID_ARGUMENTS", "needs after", ""},
	}
	for _, tc := range cases {
		before := len(calls())
		res := c.tool("ha_read", tc.args)
		env, code := readEnvelope(res)
		errInfo, _ := env["error"].(map[string]any)
		message, _ := errInfo["message"].(string)
		sent := calls()[before:]
		switch {
		case code != tc.code || !strings.Contains(message, tc.says) || strings.Contains(message, "client with a shell"):
			t.Errorf("%v: code %q, message %q; want %q with %q", tc.args, code, message, tc.code, tc.says)
		case tc.sent == "" && len(sent) != 0:
			t.Errorf("%v: refused but sent %v", tc.args, sent)
		case tc.sent != "" && (len(sent) != 1 || sent[0].endpoint != tc.sent):
			t.Errorf("%v: sent %v, want one %s request", tc.args, sent, tc.sent)
		}
	}
	// The payload reaches the Relay as given: a core POST with its body, a
	// ws envelope whole.
	c.tool("ha_read", map[string]any{"endpoint": "core", "method": "POST", "path": "/api/template", "payload": map[string]any{"template": "{{ states('sun.sun') }}"}})
	last := calls()[len(calls())-1]
	if last.body != `{"method":"POST","path":"/api/template","body":{"template":"{{ states('sun.sun') }}"}}` {
		t.Fatalf("template body %s", last.body)
	}
}

// ha_read refuses every raw request `ha-nova relay` refuses, with the same words;
// of the requests the CLI sends, it sends the reads and plans the rest.
func TestMCPReadRefusesWhatRelayRefuses(t *testing.T) {
	paths, forwarded, _ := rawGuardRelay(t, func() string { return `[]` }, nil, nil)
	read := func(endpoint, body string) (int, verbTestEnvelope) {
		exit, out := captureCommandOutput(t, func() int { return runMCPRead(paths, []string{endpoint, "--body=" + body}) })
		var env verbTestEnvelope
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatalf("%s %s: %v in %q", endpoint, body, err, out)
		}
		return exit, env
	}
	refused, allowed := rawGuardCases()
	for endpoint, bodies := range refused {
		for _, body := range bodies {
			want := checkRawRelayRequest(endpoint, []byte(body))
			before := forwarded.Load()
			exit, env := read(endpoint, body)
			if exit != verbExitRefused || env.Error == nil || env.Error.Code != "REFUSED" || want == nil || env.Error.Message != want.Error() || forwarded.Load() != before {
				t.Errorf("%s %s: exit %d, error %+v; want the relay's refusal %v", endpoint, body, exit, env.Error, want)
			}
		}
	}
	core := func(method, path string) string {
		body, _ := json.Marshal(map[string]any{"method": method, "path": path})
		return string(body)
	}
	reads := map[string]bool{core("GET", "/api/events"): true, core("GET", "/api/states/lock.front_door"): true, core("GET", "/api/config/automation/config/1"): true,
		`{"method":"POST","path":"/api/template","body":{"template":"{{ 1 }}"}}`: true, core("POST", "/api/config/core/check_config"): true,
		`{"message":{"type":"subscribe_events","event_type":"zha_event"},"collect_events":{"max_events":1}}`:                true,
		`{"message":{"type":"subscribe_trigger","trigger":{"platform":"state","entity_id":"event.x"}},"collect_events":{}}`: true,
		`{"type":"ping"}`: true, `{"type":"lovelace/config","url_path":"x"}`: true}
	for endpoint, bodies := range allowed {
		for _, body := range bodies {
			exit, env := read(endpoint, body)
			switch {
			case reads[body] && (env.Error != nil && env.Error.Code == "REFUSED" || exit == verbExitRefused):
				t.Errorf("%s %s: a read the relay sends was refused: %+v", endpoint, body, env.Error)
			case reads[body]:
				delete(reads, body)
			case exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "ha_plan kind raw"):
				t.Errorf("%s %s: exit %d, error %+v; want it planned with ha_plan kind raw", endpoint, body, exit, env.Error)
			}
		}
	}
	if len(reads) != 0 {
		t.Fatalf("reads not among the relay's cases: %v", reads)
	}
}
