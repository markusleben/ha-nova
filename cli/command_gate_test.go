package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gateTestSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func gateTestServer(t *testing.T, paths runtimePaths) (post func(path, token string, in map[string]any) (int, map[string]any)) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(file, []byte(gateTestSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret, err := readServeSecret(file, "gate secret")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gateHandler(paths, secret))
	t.Cleanup(server.Close)
	return func(path, token string, in map[string]any) (int, map[string]any) {
		raw, _ := json.Marshal(in)
		req, _ := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(raw))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// The worker answers the Relay's checks with the CLI's own verdict, only
// with the gate secret.
func TestGateWorkerChecks(t *testing.T) {
	f := newFakeHA()
	paths, _ := setupVerbRelayTest(t, f.reply)
	post := gateTestServer(t, paths)
	check := func(endpoint, body string) map[string]any {
		t.Helper()
		status, out := post("/check", gateTestSecret, map[string]any{"principal": map[string]any{"kind": "device", "device_id": "d1"}, "endpoint": endpoint, "body_b64": b64(body)})
		if status != http.StatusOK {
			t.Fatalf("%s %s: HTTP %d", endpoint, body, status)
		}
		return out
	}
	for _, tc := range []struct{ endpoint, body, verdict, kind string }{
		{"core", `{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":["lock.front"]}}`, "typed", "write"},
		{"core", `{"method":"POST","path":"/api/services/light/turn_on","body":{"entity_id":["light.kitchen"]}}`, "pass", "write"},
		{"core", `{"method":"GET","path":"/api/states"}`, "pass", "read"},
		{"ws", `{"type":"fire_event","event_type":"mobile_app_notification_action"}`, "refuse", "write"},
		{"ws", `[1]`, "refuse", "write"},
	} {
		if out := check(tc.endpoint, tc.body); out["verdict"] != tc.verdict || out["kind"] != tc.kind {
			t.Errorf("%s %s: %v, want %s/%s", tc.endpoint, tc.body, out, tc.verdict, tc.kind)
		}
	}
	for _, token := range []string{"", strings.Repeat("0", 64)} {
		if status, _ := post("/check", token, map[string]any{"endpoint": "ws", "body_b64": b64(`{"type":"get_states"}`)}); status != http.StatusUnauthorized {
			t.Errorf("token %q: HTTP %d, want 401", token, status)
		}
	}
	if status, _ := post("/check", gateTestSecret, map[string]any{"endpoint": "ws", "body_b64": "not base64!"}); status != http.StatusBadRequest {
		t.Errorf("bad base64: HTTP %d, want 400", status)
	}
	if status, _ := post("/check", gateTestSecret, map[string]any{"endpoint": "ws", "body_b64": b64("{}"), "extra": 1}); status != http.StatusBadRequest {
		t.Errorf("unknown field: HTTP %d, want 400", status)
	}
	if status, _ := post("/other", gateTestSecret, map[string]any{"endpoint": "ws"}); status != http.StatusNotFound {
		t.Errorf("unknown path: HTTP %d, want 404", status)
	}
}

// Redaction hides stored codes in the whole envelope; a rendered template
// holding a live camera token is withheld; an answer that isn't JSON too.
func TestGateWorkerRedacts(t *testing.T) {
	f := newFakeHA()
	f.ent("camera.door", "demo", "idle", map[string]any{"access_token": "tok-0123456789abcdef"})
	paths, _ := setupVerbRelayTest(t, f.reply)
	post := gateTestServer(t, paths)
	redact := func(endpoint, request, response string) (int, string) {
		t.Helper()
		status, out := post("/redact", gateTestSecret, map[string]any{"endpoint": endpoint, "request_b64": b64(request), "response_b64": b64(response)})
		text, _ := out["response_b64"].(string)
		raw, _ := base64.StdEncoding.DecodeString(text)
		return status, string(raw)
	}
	status, got := redact("ws", `{"type":"config/entity_registry/get","entity_id":"lock.front"}`, `{"ok":true,"data":{"options":{"lock":{"default_code":"4711"}}}}`)
	if status != http.StatusOK || strings.Contains(got, "4711") || !strings.Contains(got, "default_code") {
		t.Errorf("lock code: HTTP %d %s", status, got)
	}
	if status, _ := redact("core", `{"method":"POST","path":"/api/template","body":{"template":"x"}}`, `{"ok":true,"data":{"status":200,"body":"tok-0123456789abcdef"}}`); status != http.StatusServiceUnavailable {
		t.Errorf("template with a camera token: HTTP %d, want withheld", status)
	}
	if status, got := redact("core", `{"method":"POST","path":"/api/template","body":{"template":"x"}}`, `{"ok":true,"data":{"status":200,"body":"21.5"}}`); status != http.StatusOK || !strings.Contains(got, "21.5") {
		t.Errorf("plain template: HTTP %d %s", status, got)
	}
	for _, request := range []string{"", "not json", `{"method":"POST","path":"/api/%zz"}`} {
		if status, _ := redact("core", request, `{"ok":true,"data":{"status":200,"body":"tok-0123456789abcdef"}}`); status != http.StatusServiceUnavailable {
			t.Errorf("request %q: HTTP %d, want withheld", request, status)
		}
	}
	if status, _ := redact("ws", `{"type":"get_states"}`, "not json"); status != http.StatusServiceUnavailable {
		t.Errorf("non-JSON answer: HTTP %d, want withheld", status)
	}
}

func TestGateRelayFile(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		file := filepath.Join(dir, "relay.json")
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	token := strings.Repeat("A", 43)
	if cfg, err := loadGateRelay(write(`{"relay_url":"http://127.0.0.1:8791","credential":"` + token + `"}`)); err != nil || cfg.RelaySpkiPin != "" || cfg.bridgeCredential != token {
		t.Errorf("standalone: %+v %v", cfg, err)
	}
	app := `{"relay_url":"https://127.0.0.1:8443","spki_pin":"` + strings.Repeat("p", 43) + `","credential":"hanova-dev-v1.` + strings.Repeat("a", 22) + "." + strings.Repeat("b", 43) + `"}`
	if cfg, err := loadGateRelay(write(app)); err != nil || cfg.RelaySpkiPin == "" {
		t.Errorf("App: %+v %v", cfg, err)
	}
	for _, bad := range []string{`{"relay_url":"http://10.0.0.2:8791","credential":"` + token + `"}`, `{"relay_url":"http://127.0.0.1:8791","credential":"short"}`, `{"relay_url":"http://127.0.0.1","credential":"` + token + `"}`} {
		if _, err := loadGateRelay(write(bad)); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
	loose := write(`{"relay_url":"http://127.0.0.1:8791","credential":"` + token + `"}`)
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadGateRelay(loose); err == nil {
		t.Error("a relay file others can read: accepted")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(write(`{"relay_url":"http://127.0.0.1:8791","credential":"`+token+`"}`), link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadGateRelay(link); err == nil {
		t.Error("a symlinked relay file: accepted")
	}
	if exit := runGateCommand(runtimePaths{}, []string{"--listen", "0.0.0.0:8796", "--secret-file", "x", "--relay-file", "y"}); exit != 1 {
		t.Errorf("a non-loopback listen: exit %d", exit)
	}
}

// A camera whose picture holds its token, but no access_token attribute:
// a template that prints it is withheld too; so is any template answer when
// the states read back no list.
func TestGateWorkerTemplatePictureToken(t *testing.T) {
	f := newFakeHA()
	f.ent("image.door", "demo", "idle", map[string]any{"entity_picture": "/api/image_proxy/image.door?token=pic-0123456789abcdef"})
	paths, _ := setupVerbRelayTest(t, f.reply)
	post := gateTestServer(t, paths)
	status, _ := post("/redact", gateTestSecret, map[string]any{"endpoint": "core", "request_b64": b64(`{"method":"POST","path":"/api/template","body":{"template":"x"}}`), "response_b64": b64(`{"ok":true,"data":{"status":200,"body":"pic-0123456789abcdef"}}`)})
	if status != http.StatusServiceUnavailable {
		t.Errorf("template with a picture token: HTTP %d, want withheld", status)
	}
}
