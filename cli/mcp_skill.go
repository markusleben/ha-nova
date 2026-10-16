package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ha_skill serves the skills as tool results: the same Markdown a
// skill-mode agent reads, behind one header that maps the CLI to the MCP
// tools. Read-only and offline: no Relay or Home Assistant call, and only
// files from the bundle's skills/ allowlist are ever opened — a name is
// matched against that list as a string, never joined into a path.

const mcpSkillHeader = `You are running HA NOVA through MCP tools, not a shell. Where the text below
names a CLI command, use the tool:
- ha-nova find|get|history|trace → ha_find|ha_get|ha_history|ha_trace
- ha-nova plan|call|apply → ha_plan|ha_call|ha_apply; flags become arguments
  (--confirm → confirm_code, plan write --file → draft, --data-file → data)
- read-only ha-nova relay core GET /api/… and relay ws reads → ha_read
- skip ha-nova check-update and relay health: tool results carry notices[]
Everything else (helper, dashboard, scene, registry or organize edits, other
relay writes, files, jq) needs a client with a shell and the HA NOVA CLI: tell
the user so instead of improvising. Pass a confirmation code to ha_apply only
when the user typed it. A cited file (skills/….md, ../ha-nova/….md): call
ha_skill with that path as name when the step you run needs it.

---

`

// mcpSkillBudget bounds a skill response with an inlined cited file; the
// skill itself is never truncated.
const mcpSkillBudget = 40 << 10

const mcpOutputRules = "ha-nova/output-rules.md"

var mcpSkillTool = mcpTool{name: "ha_skill", title: "Load a skill", readOnly: true, idempotent: true,
	description: "HA NOVA's know-how: returns the skill (a Markdown workflow) to follow before you build, change, review, or diagnose anything — automations, scripts, helpers, scenes, dashboards, failures, audits. Device control and lookups need only the other tools. Read-only; reads no Home Assistant data. Pass name (a skill id, or a file a skill cites such as skills/ha-nova/output-rules.md), or task (a few words) to get the dispatch table.",
	schema: object(nil, map[string]any{"name": prop("string", "a skill id (e.g. write, review, diagnose) or a cited file path"),
		"task": prop("string", "what the user wants, when unsure which skill fits")})}

// skillIDs maps skill id (the skills/<id> directory) → its SKILL.md path.
func skillIDs(paths runtimePaths) ([]string, map[string]string) {
	names, files := skillPrompts(paths)
	ids, byID := []string{}, map[string]string{}
	for _, name := range names {
		id := strings.TrimPrefix(name, "ha-nova-")
		ids, byID[id] = append(ids, id), files[name]
	}
	return ids, byID
}

// skillDescription appends the installed skill ids to ha_skill's description.
func skillDescription(paths runtimePaths) string {
	ids, _ := skillIDs(paths)
	if len(ids) == 0 {
		return mcpSkillTool.description
	}
	return mcpSkillTool.description + " Skill ids: " + strings.Join(ids, ", ") + "."
}

// resolveSkill matches a name to an allowlisted file: the skill id or the
// skills/-relative reference path it stands for.
func resolveSkill(paths runtimePaths, name string) (label, path string, isSkill bool) {
	n := strings.TrimPrefix(strings.TrimSpace(name), "ha-nova:")
	for _, prefix := range []string{"./", "../", "skills/"} {
		n = strings.TrimPrefix(n, prefix)
	}
	n = strings.TrimSuffix(n, "/SKILL.md")
	_, byID := skillIDs(paths)
	if file, ok := byID[n]; ok {
		return n, file, true
	}
	if file, ok := byID[strings.TrimPrefix(n, "ha-nova-")]; ok { // a prompt name
		return strings.TrimPrefix(n, "ha-nova-"), file, true
	}
	if containsText(referenceFiles(paths), n) {
		return "skills/" + n, filepath.Join(skillsRoot(paths), filepath.FromSlash(n)), false
	}
	return "", "", false
}

func (srv *mcpServer) callSkill(args map[string]json.RawMessage) map[string]any {
	name, task := strings.TrimSpace(text(args, "name")), strings.TrimSpace(text(args, "task"))
	ids, _ := skillIDs(srv.paths)
	pick := "Call ha_skill with name set to one skill id: " + strings.Join(ids, ", ") + "; or pass task to get the dispatch table."
	switch {
	case name != "":
		label, path, isSkill := resolveSkill(srv.paths, name)
		body, err := os.ReadFile(path)
		if label == "" || err != nil {
			return skillError("UNKNOWN_SKILL", "no HA NOVA skill or cited file named "+quoteShort(name)+"; nothing was read", pick)
		}
		title := label
		if isSkill {
			title = "skill " + label
		}
		out := mcpSkillHeader + "# HA NOVA " + title + "\n\n" + string(body)
		if isSkill && strings.Contains(string(body), mcpOutputRules) {
			extra, err := os.ReadFile(filepath.Join(skillsRoot(srv.paths), filepath.FromSlash(mcpOutputRules)))
			cited := "\n\n---\n\n# Cited file skills/" + mcpOutputRules + " (included)\n\n" + string(extra)
			if err == nil && len(out)+len(cited) <= mcpSkillBudget {
				out += cited
			}
		}
		return skillText(out)
	case task != "":
		index, err := os.ReadFile(filepath.Join(skillsRoot(srv.paths), "ha-nova", "index.md"))
		if err != nil {
			return skillError("UNKNOWN_SKILL", "the dispatch table is missing from this HA NOVA install", pick)
		}
		return skillText(mcpSkillHeader + "Pick the ONE skill whose Dispatch line matches the task, then call ha_skill with name set to its id (the ha-nova: prefix is optional). Device control and lookups need no skill.\n\n" + string(index))
	}
	return skillError("INVALID_ARGUMENTS", "ha_skill needs name or task; nothing was read", pick)
}

func skillText(markdown string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": markdown}}, "isError": false}
}

func skillError(code, message, next string) map[string]any {
	envelope := map[string]any{"schema_version": 1, "verb": "skill", "status": "error",
		"error": map[string]any{"code": code, "message": message}, "next_step": next}
	raw, _ := json.Marshal(envelope)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(raw)}}, "structuredContent": envelope, "isError": true}
}

// quoteShort quotes the model's own input, bounded.
func quoteShort(s string) string {
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	b, _ := json.Marshal(s)
	return string(b)
}
