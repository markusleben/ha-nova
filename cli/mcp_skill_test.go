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
		core := strings.TrimPrefix(text, mcpSkillHeader)
		if !strings.HasPrefix(core, "# Safety Core\n") ||
			!strings.Contains(core, "---\n\n# HA NOVA skill write\n\n---\nname: write") || strings.Index(core, "The CLI rates every action") > strings.Index(core, "name: write") {
			t.Fatalf("%q: %.300q", name, text)
		}
	}
	routerCore, err := skillSafetyCore(h.paths, mcpSafetyCorePointer) // the router's snapshot deletes carry the pointer
	if err != nil || routerCore == "" {
		t.Fatalf("Safety Core: %v", err)
	}
	for name, want := range map[string]string{"ha-nova": mcpSkillHeader + routerCore + "# HA NOVA skill ha-nova\n\n---\nname: ha-nova",
		"ha-nova:ha-nova":                 mcpSkillHeader + routerCore + "# HA NOVA skill ha-nova\n",
		"skills/review/checks.md":         "# HA NOVA skills/review/checks.md (CLI commands map",
		"../ha-nova/session-bootstrap.md": "# HA NOVA skills/ha-nova/session-bootstrap.md (CLI commands map",
		"raw-routes.md":                   "# HA NOVA skills/service-call/raw-routes.md (CLI commands map",
		"capability-answer.md":            "# HA NOVA skills/ha-nova/capability-answer.md (CLI commands map"} {
		if text := skillText0(t, c.tool("ha_skill", map[string]any{"name": name})); !strings.HasPrefix(text, want) {
			t.Fatalf("%q: %.300q", name, text)
		}
	}
	routed := skillText0(t, c.tool("ha_skill", map[string]any{"task": "why did my motion light not turn on"}))
	if !strings.Contains(routed, "## Dispatch") || !strings.Contains(routed, "# Safety Core\n") || !strings.Contains(routed, "call ha_skill with name set to its id") || strings.Contains(routed, "motion light") {
		t.Fatalf("task: %.400q", routed)
	}
	if len(h.log.all()) != before {
		t.Fatalf("ha_skill reached the Relay")
	}

	refused := c.tool("ha_read", map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "camera/stream", "entity_id": "camera.door"}})["structuredContent"].(map[string]any)
	message, _ := refused["error"].(map[string]any)["message"].(string)
	if refused["error"].(map[string]any)["code"] != "REFUSED" || !strings.Contains(message, mcpPlanIt) ||
		strings.Contains(message, "client with a shell") || refused["next_step"] != nil {
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
		core, err := skillSafetyCore(srv.paths, string(body))
		if err != nil || (core != "") != strings.Contains(string(body), mcpSafetyCorePointer) || !strings.HasPrefix(text, mcpSkillHeader+core+"# HA NOVA skill "+id+"\n\n") {
			t.Fatalf("%s: Safety Core %q, %v", id, core, err)
		}
		inlined := strings.HasSuffix(text, "# Cited file skills/ha-nova/output-rules.md (included)\n\n"+string(rules))
		fits := len(mcpSkillHeader+core+"# HA NOVA skill "+id+"\n\n"+"\n\n---\n\n# Cited file skills/ha-nova/output-rules.md (included)\n\n")+len(body)+len(rules) <= mcpSkillBudget
		if id == "fallback" && !inlined { // the unfamiliar-write path keeps its output rules in one call
			t.Fatalf("fallback: output-rules.md not inlined (%d bytes)", len(text))
		}
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
	mustWrite(filepath.Join(root, "skills", "scene", "SKILL.md"), mcpSafetyCorePointer+".")
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
	// A mutation skill whose Safety Core cannot be read is never served.
	for _, name := range []string{"scene", "ha-nova-scene"} {
		res := callSkillJSON(t, srv, name)
		if res["isError"] != true || strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "SECRET") {
			t.Fatalf("%q without its Safety Core: %v", name, res)
		}
	}
	if _, err := srv.getPrompt(json.RawMessage(`{"name":"ha-nova-scene"}`)); err == nil {
		t.Fatal("prompts/get served scene without its Safety Core")
	}
	raw, _ := json.Marshal(srv.callSkill(map[string]json.RawMessage{"task": json.RawMessage(`"x"`)}))
	if strings.Contains(string(raw), "SECRET") || !strings.Contains(string(raw), "UNKNOWN_SKILL") {
		t.Fatalf("task through a symlinked index: %s", raw)
	}
}

// The Safety Core reaches every text that points to it, or the text is not
// served: a heading-only or partial file fails closed, a CRLF checkout works.
func TestMCPSafetyCoreFailsClosed(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(root, "skills", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pointer := mcpSafetyCorePointer + "."
	write("scene/SKILL.md", "scene skill\n"+pointer)
	write("read/SKILL.md", "read-only skill")
	write("ha-nova/index.md", "# HA NOVA Index\n\n## Dispatch\n")
	write("ha-nova/config-snapshots.md", "snapshot recipe\n"+pointer)
	t.Setenv("HA_NOVA_DEV_ROOT", root)
	srv := &mcpServer{paths: runtimePaths{}}
	served := func() []bool {
		var out []bool
		for _, name := range []string{"scene", "config-snapshots.md"} {
			out = append(out, callSkillJSON(t, srv, name)["isError"] != true)
		}
		task, _ := json.Marshal(srv.callSkill(map[string]json.RawMessage{"task": json.RawMessage(`"x"`)}))
		_, promptErr := srv.getPrompt(json.RawMessage(`{"name":"ha-nova-scene"}`))
		_, resourceErr := srv.readResource(json.RawMessage(`{"uri":"ha-nova://skills/ha-nova/config-snapshots.md"}`), true)
		return append(out, !strings.Contains(string(task), "UNKNOWN_SKILL"), promptErr == nil, resourceErr == nil)
	}
	full, err := os.ReadFile(filepath.Join("..", "skills", "ha-nova", "safety-core.md"))
	if err != nil {
		t.Fatal(err)
	}
	var missingRule []string
	for _, line := range strings.Split(string(full), "\n") {
		if !strings.HasPrefix(line, "- Anything before the preview") {
			missingRule = append(missingRule, line)
		}
	}
	truncated := string(full[:strings.LastIndex(string(full), "once.")])
	for _, core := range []string{"", "# Safety Core\n", "# Safety Core\n\n- The CLI rates every action\n- ha-nova:fallback\n", strings.Join(missingRule, "\n"), truncated} {
		write("ha-nova/safety-core.md", core)
		for i, ok := range served() {
			if ok {
				t.Fatalf("core %q: path %d served without a complete Safety Core", core, i)
			}
		}
		if text := skillText0(t, callSkillJSON(t, srv, "read")); text != mcpSkillHeader+"# HA NOVA skill read\n\nread-only skill" {
			t.Fatalf("read-only skill: %q", text)
		}
	}
	write("ha-nova/safety-core.md", strings.ReplaceAll(string(full), "\n", "\r\n"))
	for i, ok := range served() {
		if !ok {
			t.Fatalf("CRLF core: path %d refused", i)
		}
	}
	if text := skillText0(t, callSkillJSON(t, srv, "config-snapshots.md")); !strings.HasPrefix(text, "# Safety Core\n") || !strings.Contains(text, "snapshot recipe") {
		t.Fatalf("cited recipe: %q", text)
	}
}
