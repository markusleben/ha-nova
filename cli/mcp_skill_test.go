package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skillText0(t *testing.T, result map[string]any) string {
	t.Helper()
	if result["isError"] != false {
		t.Fatalf("ha_skill failed: %v", result)
	}
	return result["content"].([]any)[0].(map[string]any)["text"].(string)
}

// callSkillJSON runs ha_skill in process and returns the result as a client sees it.
func callSkillJSON(t *testing.T, srv *mcpServer, name string) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(srv.callSkill(map[string]json.RawMessage{"name": json.RawMessage(quoteShort(name))}))
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func skillErrorCode(result map[string]any) string {
	envelope, _ := result["structuredContent"].(map[string]any)
	errInfo, _ := envelope["error"].(map[string]any)
	code, _ := errInfo["code"].(string)
	return code
}

// ha_skill: schema, instructions, id and path forms, the dispatch fallback,
// the size bound, errors with a next step — and never Home Assistant.
func TestMCPSkillTool(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	init := c.request("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}})
	if instructions := init["result"].(map[string]any)["instructions"].(string); !strings.Contains(instructions, "review, diagnose, or suggest anything, call ha_skill") ||
		!strings.Contains(instructions, "load it with ha_skill") {
		t.Fatalf("instructions: %s", instructions)
	}
	tool := c.request("tools/list", map[string]any{})["result"].(map[string]any)["tools"].([]any)[0].(map[string]any)
	schema := tool["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	if tool["name"] != "ha_skill" || !strings.Contains(tool["description"].(string), "Skill ids: ha-nova, admin,") || len(properties) != 2 ||
		properties["name"] == nil || properties["task"] == nil || schema["required"] != nil || schema["additionalProperties"] != false {
		t.Fatalf("ha_skill tool: %v", tool)
	}

	before := len(h.log.all())
	for _, name := range []string{"write", "ha-nova:write", "ha-nova-write", "skills/write/SKILL.md", " write "} {
		text := skillText0(t, c.tool("ha_skill", map[string]any{"name": name}))
		if !strings.HasPrefix(text, mcpSkillHeader+"# HA NOVA skill write\n\n---\nname: write") {
			t.Fatalf("%q: %.300q", name, text)
		}
	}
	for name, want := range map[string]string{"ha-nova": mcpSkillHeader + "# HA NOVA skill ha-nova\n\n---\nname: ha-nova",
		"ha-nova:ha-nova":                 mcpSkillHeader + "# HA NOVA skill ha-nova\n",
		"skills/review/checks.md":         "# HA NOVA skills/review/checks.md (CLI commands map",
		"../ha-nova/session-bootstrap.md": "# HA NOVA skills/ha-nova/session-bootstrap.md (CLI commands map",
		"raw-routes.md":                   "# HA NOVA skills/service-call/raw-routes.md (CLI commands map"} {
		if text := skillText0(t, c.tool("ha_skill", map[string]any{"name": name})); !strings.HasPrefix(text, want) {
			t.Fatalf("%q: %.300q", name, text)
		}
	}
	routed := skillText0(t, c.tool("ha_skill", map[string]any{"task": "why did my motion light not turn on"}))
	if !strings.Contains(routed, "## Dispatch") || !strings.Contains(routed, "call ha_skill with name set to its id") || strings.Contains(routed, "motion light") {
		t.Fatalf("task: %.400q", routed)
	}
	if len(h.log.all()) != before {
		t.Fatalf("ha_skill reached the Relay")
	}

	refused := c.tool("ha_read", map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "system_log/list"}})["structuredContent"].(map[string]any)
	message, _ := refused["error"].(map[string]any)["message"].(string)
	if refused["error"].(map[string]any)["code"] != "INVALID_ARGUMENTS" || !strings.Contains(message, "search/related") ||
		!strings.Contains(message, "needs a client with a shell") || refused["next_step"] != nil {
		t.Fatalf("ha_read refused ws type: %v", refused)
	}
	for _, mapping := range []string{"ha-nova find|get|history|trace|map → ha_find|ha_get|ha_history|ha_trace|ha_map", "revert --target → id", "7\n  OUTCOME_UNKNOWN", "→ ha_read"} {
		if !strings.Contains(mcpSkillHeader, mapping) {
			t.Fatalf("header lacks %q", mapping)
		}
	}

	unknown := c.tool("ha_skill", map[string]any{"name": "nope"})
	next, _ := unknown["structuredContent"].(map[string]any)["next_step"].(string)
	if unknown["isError"] != true || skillErrorCode(unknown) != "UNKNOWN_SKILL" || !strings.Contains(next, "service-call") || !strings.Contains(next, "pass task") {
		t.Fatalf("unknown: %v", unknown)
	}
	for _, args := range []map[string]any{{}, {"name": ""}, {"name": 5}, {"path": "write"}} {
		if res := c.tool("ha_skill", args); res["isError"] != true || skillErrorCode(res) != "INVALID_ARGUMENTS" {
			t.Fatalf("%v: %v", args, res)
		}
	}
}

// Every installed skill is served whole; an inlined cited file keeps the
// response within the budget.
func TestMCPSkillEverySkillBounded(t *testing.T) {
	t.Setenv("HA_NOVA_DEV_ROOT", mustAbs(t, ".."))
	srv := &mcpServer{paths: runtimePaths{}}
	ids, files := skillIDs(srv.paths)
	if len(ids) < 30 {
		t.Fatalf("skills: %v", ids)
	}
	rules, err := os.ReadFile(filepath.Join(skillsRoot(srv.paths), "ha-nova", "output-rules.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		body, _ := os.ReadFile(files[id])
		text := skillText0(t, callSkillJSON(t, srv, id))
		inlined := strings.HasSuffix(text, "# Cited file skills/ha-nova/output-rules.md (included)\n\n"+string(rules))
		fits := len(mcpSkillHeader+"# HA NOVA skill "+id+"\n\n"+"\n\n---\n\n# Cited file skills/ha-nova/output-rules.md (included)\n\n")+len(body)+len(rules) <= mcpSkillBudget
		if !strings.Contains(text, string(body)) || inlined && len(text) > mcpSkillBudget || inlined != (fits && strings.Contains(string(body), "output-rules.md")) {
			t.Fatalf("%s: %d bytes, inlined %v", id, len(text), inlined)
		}
	}
}

// Only allowlisted files under skills/ are served: no traversal, no other
// extension, no symlink out of the tree.
func TestMCPSkillAllowlist(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.md")
	mustWrite := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(outside, "SECRET")
	mustWrite(filepath.Join(root, "AGENTS.md"), "SECRET")
	mustWrite(filepath.Join(root, "skills", "write", "SKILL.md"), "write skill, see ha-nova/output-rules.md")
	mustWrite(filepath.Join(root, "skills", "ha-nova", "filter.jq"), "SECRET")
	if err := os.MkdirAll(filepath.Join(root, "skills", "evil"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{filepath.Join(root, "skills", "ha-nova", "leak.md"), filepath.Join(root, "skills", "evil", "SKILL.md"),
		filepath.Join(root, "skills", "ha-nova", "output-rules.md"), filepath.Join(root, "skills", "ha-nova", "index.md")} {
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "skills", "linked")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HA_NOVA_DEV_ROOT", root)
	srv := &mcpServer{paths: runtimePaths{}}
	if text := skillText0(t, callSkillJSON(t, srv, "write")); !strings.HasSuffix(text, "write skill, see ha-nova/output-rules.md") {
		t.Fatalf("write: %q", text)
	}
	for _, name := range []string{"../AGENTS.md", "../../AGENTS.md", "skills/../AGENTS.md", "ha-nova/../../AGENTS.md", root + "/AGENTS.md",
		"/etc/passwd", "ha-nova/filter.jq", "output-rules.md", "index.md", "secret.md", "ha-nova/leak.md", "evil", "linked/secret.md", "linked", "skills/linked/secret.md"} {
		res := callSkillJSON(t, srv, name)
		if res["isError"] != true || skillErrorCode(res) != "UNKNOWN_SKILL" || strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "SECRET") {
			t.Fatalf("%q served: %v", name, res)
		}
	}
	raw, _ := json.Marshal(srv.callSkill(map[string]json.RawMessage{"task": json.RawMessage(`"x"`)}))
	if strings.Contains(string(raw), "SECRET") || !strings.Contains(string(raw), "UNKNOWN_SKILL") {
		t.Fatalf("task through a symlinked index: %s", raw)
	}
}
