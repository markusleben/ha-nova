package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ha-nova serve (mcp_serve.go): the chat-app bridge over Streamable HTTP,
// driven with raw HTTP against the fake Relay.

const (
	userA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	userB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type serveHarness struct {
	*writeHarness
	b      *chatBridge
	url    string
	secret string
	data   string
	logs   *lockedBuffer
	id     atomic.Int64
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func randomHex(t *testing.T, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// newServe starts the bridge; settings is chat-apps.json ("" = none).
func newServe(t *testing.T, f *fakeHA, settings string) *serveHarness {
	t.Helper()
	h := newWriteHarness(t, f)
	dir := t.TempDir()
	secret := randomHex(t, 32)
	secretFile := filepath.Join(dir, "secret")
	if err := os.WriteFile(secretFile, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuffer{}
	data := filepath.Join(dir, "data")
	b, err := newChatBridge(h.paths, data, secretFile, 300*time.Millisecond, logs)
	if err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(data, "chat-apps.json"), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	return &serveHarness{writeHarness: h, b: b, url: srv.URL + "/mcp", secret: secret, data: data, logs: logs}
}

// raw posts body with the given headers and returns the status and body.
func (s *serveHarness) raw(method string, headers map[string][]string, body string) (int, string) {
	s.t.Helper()
	req, _ := http.NewRequest(method, s.url, strings.NewReader(body))
	for k, values := range headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func (s *serveHarness) as(user string) map[string][]string {
	return map[string][]string{"Authorization": {"Bearer " + s.secret}, serveUserHeader: {user}}
}

// rpc sends one request as user and returns the JSON-RPC message.
func (s *serveHarness) rpc(user, method string, params any) map[string]any {
	s.t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": s.id.Add(1), "method": method, "params": params})
	status, out := s.raw(http.MethodPost, s.as(user), string(body))
	var msg map[string]any
	if status != http.StatusOK || json.Unmarshal([]byte(out), &msg) != nil {
		s.t.Fatalf("%s: HTTP %d %q", method, status, out)
	}
	return msg
}

// tool calls a tool as user and returns its envelope (structuredContent).
func (s *serveHarness) tool(user, name string, args map[string]any) map[string]any {
	s.t.Helper()
	msg := s.rpc(user, "tools/call", map[string]any{"name": name, "arguments": args})
	result, _ := msg["result"].(map[string]any)
	envelope, _ := result["structuredContent"].(map[string]any)
	if envelope == nil {
		s.t.Fatalf("%s: %v", name, msg)
	}
	return envelope
}

func (s *serveHarness) userPlans(user string) []string {
	call, _, _ := s.b.policyFor(user)
	files, _ := filepath.Glob(filepath.Join(s.data, "users", call.key, "plans", "p_*"))
	return files
}

func envCode(env map[string]any) string {
	errInfo, _ := env["error"].(map[string]any)
	code, _ := errInfo["code"].(string)
	return code
}

func TestServeRefusesWithoutSecretAndAllowedUser(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	bearer := "Bearer " + s.secret
	cases := map[string]struct {
		headers map[string][]string
		want    int
	}{
		"no headers":         {nil, http.StatusUnauthorized},
		"no secret":          {map[string][]string{serveUserHeader: {userA}}, http.StatusUnauthorized},
		"wrong secret":       {map[string][]string{"Authorization": {"Bearer " + randomHex(t, 32)}, serveUserHeader: {userA}}, http.StatusUnauthorized},
		"secret, no bearer":  {map[string][]string{"Authorization": {s.secret}, serveUserHeader: {userA}}, http.StatusUnauthorized},
		"secret prefix":      {map[string][]string{"Authorization": {bearer[:len(bearer)-1]}, serveUserHeader: {userA}}, http.StatusUnauthorized},
		"two authorizations": {map[string][]string{"Authorization": {bearer, bearer}, serveUserHeader: {userA}}, http.StatusUnauthorized},
		"no user":            {map[string][]string{"Authorization": {bearer}}, http.StatusUnauthorized},
		"malformed user":     {map[string][]string{"Authorization": {bearer}, serveUserHeader: {"../" + userA}}, http.StatusUnauthorized},
		"two users":          {map[string][]string{"Authorization": {bearer}, serveUserHeader: {userA, userA}}, http.StatusUnauthorized},
		"user not allowed":   {s.as(userB), http.StatusForbidden},
	}
	for name, c := range cases {
		if status, body := s.raw(http.MethodPost, c.headers, list); status != c.want || strings.Contains(body, "ha_apply") {
			t.Errorf("%s: HTTP %d %q, want %d", name, status, body, c.want)
		}
	}
	if status, _ := s.raw(http.MethodGet, s.as(userA), ""); status != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", status)
	}
	if len(s.log.all()) != 0 {
		t.Fatalf("a refused request reached the Relay: %d calls", len(s.log.all()))
	}
	if status, _ := s.raw(http.MethodPost, s.as(userA), list); status != http.StatusOK {
		t.Fatalf("allowed user: %d", status)
	}
	if logs := s.logs.String(); strings.Contains(logs, s.secret) || !strings.Contains(logs, "http=401") || !strings.Contains(logs, "http=403") {
		t.Fatalf("log: %s", logs)
	}
}

// No allowed_users: the owner only, read through the Relay. Settings the
// bridge can't read refuse everyone.
func TestServeDefaultsToTheOwnerAndFailsClosed(t *testing.T) {
	f := newFakeHA()
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		if endpoint == "ws" && p["type"] == "config/auth/list" {
			return []map[string]any{{"id": userB, "is_owner": false, "is_active": true}, {"id": userA, "is_owner": true, "is_active": true}}, true
		}
		return nil, false
	}
	s := newServe(t, f, "")
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if status, _ := s.raw(http.MethodPost, s.as(userA), list); status != http.StatusOK {
		t.Fatalf("owner: %d", status)
	}
	if status, _ := s.raw(http.MethodPost, s.as(userB), list); status != http.StatusForbidden {
		t.Fatalf("not the owner: %d", status)
	}
	for _, bad := range []string{`{`, `{"allowed_user":["` + userA + `"]}`, `{"read_only":"yes"}`, `{}{"read_only":true}`} {
		if err := os.WriteFile(filepath.Join(s.data, "chat-apps.json"), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if status, _ := s.raw(http.MethodPost, s.as(userA), list); status != http.StatusServiceUnavailable {
			t.Fatalf("settings %s: %d", bad, status)
		}
	}
}

// The same tools, schemas, and instructions as ha-nova mcp (golden: the
// stdio server's own answers).
func TestServeToolsMatchStdio(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
	c := startMCP(t, s.paths)
	stdioInit := c.request("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})["result"].(map[string]any)
	stdioTools := c.request("tools/list", map[string]any{})["result"]
	serveInit := s.rpc(userA, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"elicitation": map[string]any{}}, "clientInfo": map[string]any{"name": "t", "version": "1"}})["result"].(map[string]any)
	if !reflect.DeepEqual(stdioInit, serveInit) || serveInit["instructions"] != mcpInstructions {
		t.Fatalf("initialize differs:\nstdio %v\nserve %v", stdioInit, serveInit)
	}
	serveTools := s.rpc(userA, "tools/list", map[string]any{})["result"]
	if !reflect.DeepEqual(stdioTools, serveTools) {
		t.Fatalf("tools/list differs")
	}
	var names []string
	for _, tool := range serveTools.(map[string]any)["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "ha_skill,ha_find,ha_get,ha_history,ha_map,ha_trace,ha_read,ha_plan,ha_call,ha_apply" {
		t.Fatalf("tools %v", names)
	}
	// Stateless eras: an unknown revision header is refused; a 2026-07-28
	// request without _meta is refused as in stdio.
	headers := s.as(userA)
	headers["Mcp-Protocol-Version"] = []string{"1999-01-01"}
	if status, _ := s.raw(http.MethodPost, headers, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); status != http.StatusBadRequest {
		t.Fatalf("unknown revision: %d", status)
	}
	headers["Mcp-Protocol-Version"] = []string{mcpModernVersion}
	if status, body := s.raw(http.MethodPost, headers, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); status != http.StatusOK || !strings.Contains(body, "missing _meta") {
		t.Fatalf("modern without _meta: %d %s", status, body)
	}
	// A notification gets 202 and no body.
	if status, body := s.raw(http.MethodPost, s.as(userA), `{"jsonrpc":"2.0","method":"notifications/initialized"}`); status != http.StatusAccepted || body != "" {
		t.Fatalf("notification: %d %q", status, body)
	}
}

func TestServeReadOnlyRefusesEveryChange(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"],"read_only":true}`)
	for name, args := range map[string]map[string]any{
		"ha_call":  {"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}},
		"ha_plan":  {"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}},
		"ha_apply": {"plan_id": "p_0123456789ab"},
	} {
		env := s.tool(userA, name, args)
		if errInfo, _ := env["error"].(map[string]any); envCode(env) != "READ_ONLY" || !strings.Contains(errInfo["next"].(string), "read only") {
			t.Fatalf("%s: %v", name, env)
		}
	}
	if env := s.tool(userA, "ha_get", map[string]any{"entity_id": "light.kitchen"}); env["status"] != "ok" {
		t.Fatalf("read: %v", env)
	}
	if len(s.posts()) != 0 {
		t.Fatalf("posts %d", len(s.posts()))
	}
}

// A plan belongs to the user who made it: another user's apply finds
// nothing, even with the right code.
func TestServePlansAreBoundToTheirUser(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`","`+userB+`"]}`)
	usePhones(t) // no phone linked: the chat code
	env := s.tool(userA, "ha_plan", map[string]any{"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	plan := env["plan"].(map[string]any)
	code, _ := plan["code"].(string)
	if plan["channel"] != channelChat || len(code) != 4 || !strings.HasSuffix(env["say"].(string), chatCodeNote) || strings.Contains(env["say"].(string), sayChatNote) {
		t.Fatalf("chat plan: %v", env)
	}
	if other := s.tool(userB, "ha_apply", map[string]any{"plan_id": plan["id"], "confirm_code": code}); envCode(other) != "PLAN_NOT_FOUND" {
		t.Fatalf("other user: %v", other)
	}
	if len(s.posts()) != 0 {
		t.Fatal("another user's apply sent")
	}
	if own := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"], "confirm_code": code}); own["status"] != "verified" || len(s.posts()) != 1 {
		t.Fatalf("own apply: %v posts %d", own, len(s.posts()))
	}
	if strings.Contains(s.logs.String(), code) {
		t.Fatal("the code reached the log")
	}
}

// The owner's switch: no phone and no chat codes refuses typed plans, and
// a chat-code plan made before the switch no longer applies.
func TestServeChatCodesOff(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
	usePhones(t)
	before := s.tool(userA, "ha_plan", map[string]any{"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}})["plan"].(map[string]any)
	if err := os.WriteFile(filepath.Join(s.data, "chat-apps.json"), []byte(`{"allowed_users":["`+userA+`"],"chat_codes":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := s.tool(userA, "ha_plan", map[string]any{"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	errInfo, _ := env["error"].(map[string]any)
	if envCode(env) != "CONFIRM_UNAVAILABLE" || env["plan"] != nil || errInfo["next"] != chatCodesOffNext || len(s.userPlans(userA)) != 1 {
		t.Fatalf("chat codes off: %v", env)
	}
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": before["id"], "confirm_code": before["code"]}); envCode(env) != "CONFIRM_UNAVAILABLE" || len(s.posts()) != 0 {
		t.Fatalf("earlier chat plan: %v", env)
	}
}

// The command refuses to start without an explicit address, a data
// directory, and a 256-bit secret.
func TestServeCommandNeedsExplicitConfig(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "secret")
	short := filepath.Join(dir, "short")
	_ = os.WriteFile(good, []byte(randomHex(t, 32)), 0o600)
	_ = os.WriteFile(short, []byte("abc123"), 0o600)
	data := filepath.Join(dir, "data")
	for name, args := range map[string][]string{
		"no listen":     {"--data-dir", data, "--secret-file", good},
		"port only":     {"--listen", ":8765", "--data-dir", data, "--secret-file", good},
		"no data dir":   {"--listen", "127.0.0.1:0", "--secret-file", good},
		"no secret":     {"--listen", "127.0.0.1:0", "--data-dir", data},
		"short secret":  {"--listen", "127.0.0.1:0", "--data-dir", data, "--secret-file", short},
		"missing file":  {"--listen", "127.0.0.1:0", "--data-dir", data, "--secret-file", filepath.Join(dir, "none")},
		"wait too long": {"--listen", "127.0.0.1:0", "--data-dir", data, "--secret-file", good, "--confirm-wait", "5m"},
	} {
		if exit := runServeCommand(runtimePaths{}, args); exit != 1 {
			t.Errorf("%s: exit %d", name, exit)
		}
	}
}
