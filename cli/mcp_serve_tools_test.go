package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The tools version and the ChatGPT refresh hint (mcp_serve_tools.go).

func TestMCPToolsVersionFollowsToolsAndInstructions(t *testing.T) {
	t.Setenv("HA_NOVA_DEV_ROOT", "..")
	v := mcpToolsVersion(runtimePaths{})
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(v) || mcpToolsVersion(runtimePaths{}) != v {
		t.Fatalf("version %q", v)
	}
	instructions := mcpInstructions
	mcpInstructions += " "
	changed := mcpToolsVersion(runtimePaths{})
	mcpInstructions = instructions
	description := mcpTools[1].description
	mcpTools[1].description += " "
	described := mcpToolsVersion(runtimePaths{})
	mcpTools[1].description = description
	if changed == v || described == v || described == changed {
		t.Fatalf("versions %s %s %s", v, changed, described)
	}
}

func TestNoteToolsVersionAtStart(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339)
	note := func(dir, version string) toolsVersionState {
		t.Helper()
		state, err := noteToolsVersion(dir, version, now)
		if err != nil {
			t.Fatal(err)
		}
		var saved toolsVersionState
		if raw, err := os.ReadFile(filepath.Join(dir, toolsVersionFile)); err != nil || json.Unmarshal(raw, &saved) != nil || saved != state {
			t.Fatalf("saved %s: %v", raw, err)
		}
		return state
	}
	fresh := t.TempDir()
	if got := note(fresh, "aaa"); got != (toolsVersionState{"aaa", ""}) {
		t.Fatalf("fresh: %+v", got)
	}
	if got := note(fresh, "bbb"); got != (toolsVersionState{"bbb", stamp}) {
		t.Fatalf("another version: %+v", got)
	}
	now = now.Add(time.Hour)
	if got := note(fresh, "bbb"); got != (toolsVersionState{"bbb", stamp}) {
		t.Fatalf("same version: %+v", got)
	}
	older := t.TempDir() // users from an App before the tools version
	if err := os.MkdirAll(filepath.Join(older, "users", "k"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := note(older, "aaa"); got != (toolsVersionState{"aaa", now.Format(time.RFC3339)}) {
		t.Fatalf("older App: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(older, toolsVersionFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := note(older, "aaa"); got.ChangedAt != now.Format(time.RFC3339) {
		t.Fatalf("unreadable: %+v", got)
	}
}

func TestServeToolsHintOncePerChange(t *testing.T) {
	t.Setenv("HA_NOVA_DEV_ROOT", "..") // ha_skill reads the repo's skills
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`","`+userB+`"]}`)
	get := map[string]any{"entity_id": "light.kitchen"}
	hinted := func(env map[string]any) bool {
		step, _ := env["next_step"].(string)
		return strings.Contains(step, toolsRefreshHint)
	}
	if hinted(s.tool(userA, "ha_get", get)) || hinted(s.tool(userA, "ha_get", get)) {
		t.Fatal("a new user got the hint")
	}
	call, _, _ := s.b.policyFor(userA)
	record := filepath.Join(s.data, "users", call.key, toolsUserFile)
	if raw, _ := os.ReadFile(record); strings.TrimSpace(string(raw)) != s.b.tools.Version {
		t.Fatalf("record %q", raw)
	}
	if err := os.WriteFile(record, []byte("000000000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := s.rpc(userA, "tools/call", map[string]any{"name": "ha_get", "arguments": get})
	result, _ := msg["result"].(map[string]any)
	env, _ := result["structuredContent"].(map[string]any)
	text, _ := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !hinted(env) || !strings.Contains(text, toolsRefreshHint) || !strings.HasPrefix(text, `{"answer_rule":`) {
		t.Fatalf("no hint after a change: %v", msg)
	}
	if hinted(s.tool(userA, "ha_get", get)) {
		t.Fatal("hint twice")
	}
	if raw, _ := os.ReadFile(record); strings.TrimSpace(string(raw)) != s.b.tools.Version {
		t.Fatalf("record after the hint %q", raw)
	}

	// No record (an older App's folder): due too. An invalid call carries it
	// in its own rendering; a skill's text gets a text of its own.
	other, _, _ := s.b.policyFor(userB)
	if err := os.MkdirAll(filepath.Join(s.data, "users", other.key), 0o700); err != nil {
		t.Fatal(err)
	}
	if env := s.tool(userB, "ha_get", map[string]any{}); envCode(env) != "INVALID_ARGUMENTS" || !hinted(env) {
		t.Fatalf("invalid call: %v", env)
	}
	if err := os.Remove(filepath.Join(s.data, "users", other.key, toolsUserFile)); err != nil {
		t.Fatal(err)
	}
	if hinted(s.tool(userB, "ha_get", get)) { // a record that failed: remembered in memory
		t.Fatal("hint again without a record")
	}
	s.b.mu.Lock()
	clear(s.b.toolsHints) // as after a restart
	s.b.mu.Unlock()
	msg = s.rpc(userB, "tools/call", map[string]any{"name": "ha_skill", "arguments": map[string]any{"name": "ha-nova"}})
	content, _ := msg["result"].(map[string]any)["content"].([]any)
	if last, _ := content[len(content)-1].(map[string]any)["text"].(string); len(content) < 2 || last != "next_step: "+toolsRefreshHint {
		t.Fatalf("skill: %v", content)
	}
	if _, body := s.b.controlTools(); body["version"] != s.b.tools.Version || body["changed_at"] != "" {
		t.Fatalf("control: %v", body)
	}
}

// A shared result (an apply job's) stays as it was; a chat code's ask
// leaves the hint for the next result.
func TestAddToolsHintCopiesAndSkipsCodeAsks(t *testing.T) {
	envelope := map[string]any{"status": "ok", "next_step": "Go on."}
	addOutcomeSay(envelope)
	shared := map[string]any{"content": []map[string]any{{"type": "text", "text": string(ruleFirst(envelope))}}, "structuredContent": envelope}
	before, _ := json.Marshal(shared)
	result := maps.Clone(shared)
	srv := &mcpServer{toolsHint: "Refresh."}
	srv.addToolsHint(result)
	if after, _ := json.Marshal(shared); string(after) != string(before) || !srv.toolsHinted ||
		result["structuredContent"].(map[string]any)["next_step"] != "Go on. Refresh." {
		t.Fatalf("shared %s, result %v", after, result)
	}
	ask := map[string]any{"status": "needs_confirmation", "plan": map[string]any{"code": "1234"}}
	srv = &mcpServer{toolsHint: "Refresh."}
	srv.addToolsHint(map[string]any{"content": []map[string]any{{"type": "text", "text": "x"}}, "structuredContent": ask})
	if srv.toolsHinted || ask["next_step"] != nil {
		t.Fatalf("code ask: %v", ask)
	}
}
