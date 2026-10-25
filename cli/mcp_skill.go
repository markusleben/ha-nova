package main

import (
	"encoding/json"
	"os"
	pathpkg "path"
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
- ha-nova find|get|history|trace|map → ha_find|ha_get|ha_history|ha_trace|ha_map
- ha-nova plan <kind>|call|apply → ha_plan kind|ha_call|ha_apply; flags become
  arguments (--confirm → confirm_code, --file → draft, --data-file and --set →
  data, revert --target → id, plan restore --snapshot/--id → kind restore
  with snapshot/id); this connection is pinned: no --server
- plan write|delete|revert --domain dashboard → domain dashboard with id,
  base, title, icon, require_admin, hide_sidebar; plan revert --domain
  device_area → domain device_area; plan rooms → kind rooms; plan template
  <name> → kind template with name; get --dashboard <url_path> [--config] |
  --dashboards → ha_get dashboard (config) | dashboards; map --insights →
  ha_map insights
- relay core|ws|files|backups reads (core POST /api/template and check_config
  too), a camera image (--out-binary), snapshot show|verify, and diff
  → ha_read endpoints; send payloads and files inline; --jq, --jq-file, and
  relay jq → its jq: --out <file> then relay jq --file <file> F → repeat the
  read with jq F; for -e, check yourself that the result is not null or
  false; -r isn't needed. Prefer ha_find/ha_get over whole-list reads such as
  /api/states
- relay ws|core writes, a GET on a flow step, relay files write_file|delete_file,
  and relay backups save|delete|prune → ha_plan kind raw with requests
  [{endpoint, payload}]: all snapshot saves first, then the writes, in the
  skill's order; never more items than the skill's own cap (grouped set 10,
  device and migrate 5); over 20 items, split into manifests of at most 10
  targets. Then ha_apply. Still show the skill's own preview (a file change,
  a scene's members, a flow's body) beside the plan's say: the plan's
  confirmation and tier replace the skill's confirmation step and any code
  it would mint, except where the skill gates a step with a code and the
  plan says preview: set typed on that item. Where the skill already counts
  a step as confirmed (a flow menu pick, the .bak rollback after an invalid
  check_config, cancelling a flow after a secret form) and the plan is a
  preview, call ha_apply right after ha_plan. snapshot save after a helper
  edit: ha_apply saved it (result undo)
- exit codes → status: 2 drift, 3 needs_confirmation, 6 unverified, 7
  OUTCOME_UNKNOWN (read with ha_get or ha_read, never retry); others status
  error
- skip the session bootstrap (check-update, relay health): results carry
  notices[]. Cache files under ~/.cache/ha-nova are out of reach: missing
- "context skill" → ha_skill name ha-nova; a cited file (skills/….md,
  ../ha-nova/….md, or a bare file name): ha_skill with it as name, when the
  step you run needs it; "why does Claude ask every time?" → ha_skill name
  capability-answer.md
- setup, pair, doctor, update, check-update, version, uninstall, cloud,
  chat-apps, status, server, try: this sets up HA NOVA on a computer; tell
  the user to run it there: "That sets up HA NOVA itself, so it runs on your computer:
  ha-nova <command>."
Pass a one-time code to ha_apply only when the user typed it in chat for that
plan. Where a result says their app asks for it in a form, that form
replaces every code step below: never ask for or wait for a code in chat;
where the skill would apply the plan (also after a drift, a wrong code, or the
choice to delete without a copy), call ha_apply without confirm_code.

---

`

// mcpSkillBudget bounds a skill response with an inlined cited file; the
// skill itself is never truncated.
const mcpSkillBudget = 28 << 10 // stays under a ~10k-token client output cap

const mcpOutputRules = "ha-nova/output-rules.md"

var mcpSkillTool = mcpTool{name: "ha_skill", title: "Load instructions", readOnly: true, idempotent: true,
	description: "HA NOVA's know-how: returns the skill (a Markdown workflow) to follow before you build, change, review, diagnose, or suggest anything — automations, scripts, helpers, scenes, dashboards, failures, audits. Device control and lookups need only the other tools. Read-only; reads no Home Assistant data. Pass name (a skill id, or a file a skill cites such as ../ha-nova/output-rules.md), or task (a few words) to get the dispatch table. " + mcpAnswerRules,
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

// resolveSkill matches a name to an allowlisted file: the skill id, the
// skills/-relative reference path, or a bare file name a skill cites
// ("raw-routes.md") when exactly one listed file carries it.
func resolveSkill(paths runtimePaths, name string) (label, file string, isSkill bool) {
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
	var matches []string
	for _, rel := range referenceFiles(paths) {
		if rel == n || !strings.Contains(n, "/") && pathpkg.Base(rel) == n {
			matches = append(matches, rel)
		}
	}
	if len(matches) != 1 {
		return "", "", false
	}
	return "skills/" + matches[0], filepath.Join(skillsRoot(paths), filepath.FromSlash(matches[0])), false
}

// readReference reads a skills/-relative reference only through the allowlist.
func readReference(paths runtimePaths, rel string) ([]byte, error) {
	if _, file, isSkill := resolveSkill(paths, rel); file != "" && !isSkill {
		return os.ReadFile(file)
	}
	return nil, os.ErrNotExist
}

func (srv *mcpServer) callSkill(args map[string]json.RawMessage) map[string]any {
	name, task := strings.TrimSpace(text(args, "name")), strings.TrimSpace(text(args, "task"))
	ids, _ := skillIDs(srv.paths)
	pick := "Call ha_skill with name set to one skill id: " + strings.Join(ids, ", ") + "; or pass task to get the dispatch table."
	switch {
	case name != "":
		label, file, isSkill := resolveSkill(srv.paths, name)
		body, err := os.ReadFile(file)
		if label == "" || err != nil {
			return skillError("UNKNOWN_SKILL", "no HA NOVA skill or cited file named "+quoteShort(name)+"; nothing was read", pick)
		}
		if !isSkill { // the skill's response carried the header already
			return skillText("# HA NOVA " + label + " (CLI commands map to the MCP tools as in the skill's header)\n\n" + string(body))
		}
		out := mcpSkillHeader + "# HA NOVA skill " + label + "\n\n" + string(body)
		if strings.Contains(string(body), mcpOutputRules) {
			extra, err := readReference(srv.paths, mcpOutputRules)
			cited := "\n\n---\n\n# Cited file skills/" + mcpOutputRules + " (included)\n\n" + string(extra)
			if err == nil && len(out)+len(cited) <= mcpSkillBudget {
				out += cited
			}
		}
		return skillText(out)
	case task != "":
		index, err := readReference(srv.paths, "ha-nova/index.md")
		if err != nil {
			return skillError("UNKNOWN_SKILL", "the dispatch table is missing from this HA NOVA install", pick)
		}
		return skillText(mcpSkillHeader + "Pick the task skill whose Dispatch line matches the task, then call ha_skill with name set to its id (the ha-nova: prefix is optional); the index says when the context skill (name ha-nova) loads beside it. Device control and lookups need no skill.\n\n" + string(index))
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
