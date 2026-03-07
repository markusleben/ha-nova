---
name: read
description: Use when listing or reading Home Assistant automation and script configs through HA NOVA Relay. For analysis or review, use ha-nova:review instead.
---

# HA NOVA Read


## Scope

Read operations only:
- `automation.list`
- `automation.read`
- `script.list`
- `script.read`
- `automation.trace`
- `script.trace`

Not for helpers — use `ha-nova:helper` for helper list/read.

No writes. For analysis/review/audit, route through parent `ha-nova` skill — it dispatches the review agent after the read.

## Bootstrap (once per session)

Verify relay CLI: `~/.config/ha-nova/relay health`
If this fails: `npm run onboarding:macos`

## Flow

### Listing automations / scripts

Use the compact entity registry (abbreviated keys: `ei`=entity_id, `en`=name, `ai`=area_id):

```bash
# List automations (limit to first 30 for readability)
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/list_for_display"}' \
  | jq '[.data.entities[] | select(.ei | startswith("automation.")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:30]'

# List scripts
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/list_for_display"}' \
  | jq '[.data.entities[] | select(.ei | startswith("script.")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:30]'
```

### Keyword search

Use short keyword stems to handle spelling variants. Always limit results.

```bash
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/list_for_display"}' \
  | jq '[.data.entities[] | select(.ei | startswith("automation.")) | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:20]'
```

If 0 results: try synonyms, alternative terms, or shorter stems: `test("kw1|kw2";"i")`. Never dump entire domains.

For "automations in room X": use entity-discovery skill's area → device → `search/related` flow.

### Reading a single config

Always resolve the config key via entity registry first — the entity_id slug and the config key often differ (HA uses numeric `unique_id` internally for UI-created items).

**Always save config reads to a temp file** to avoid shell output truncation (complex automations can be 10–30 KB JSON):

```bash
# Step 1: Resolve unique_id (config key) — see relay-api.md → ID Types
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/get","entity_id":"automation.{slug}"}' \
  | jq -r '.data.unique_id'
# For scripts: use "entity_id":"script.{slug}"

# Step 2: Fetch config using the resolved unique_id (REST envelope: .data.body)
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/automation/config/{unique_id}"}' \
  | jq '.data.body' > /tmp/ha-config-{slug}.json
# For scripts: /api/config/script/config/{unique_id}

# Verify JSON is valid (exits 0 if valid, non-zero if invalid/truncated)
jq empty /tmp/ha-config-{slug}.json
```

**Read the file using your native file-reading tool** (Claude: `Read`, Gemini: file read, Cursor: open file). Do NOT use `cat`, `head`, or shell output — these may truncate.

**Never analyze a config from shell output.** Always read the temp file with your file-reading tool.

### Related entities

Find automations/scripts that use a specific entity:

```bash
~/.config/ha-nova/relay ws -d '{"type":"search/related","item_type":"entity","item_id":"{entity_id}"}'
```

If id is ambiguous, ask one clarifying question.

**IMPORTANT:** Never use raw `get_states` — it returns ALL entities (thousands) with full attributes. Use the targeted APIs above.

## Output Format

After reading a config, always present a structured summary:

```
**{Automation|Script}: {alias}**
- **ID:** {id}
- **Entities:** {list all entity_ids used in triggers, conditions, and actions}
- **Triggers:** {short description of each trigger}
- **Conditions:** {short description or "none"}
- **Actions:** {short description of each action, grouped by trigger if applicable}
- **Mode:** {single|restart|queued|parallel}
```

Then show the full YAML config:

```yaml
# Rendered YAML of the automation/script config
alias: ...
triggers: ...
actions: ...
```

For list operations, use a compact table:

```
| Entity ID | Name | Area |
|-----------|------|------|
```

Never show raw JSON to the user. Parse JSON config into structured summary + YAML.

## Trace Debugging

For trace queries ("why didn't automation X fire?", "show me the last runs"):

1. Resolve the `unique_id` (config key) — **`item_id` requires the `unique_id`, NOT the entity_id slug** (see `relay-api.md` → ID Types). For UI-created items the `unique_id` is numeric (e.g., `1766434159701`), not the slug:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/get","entity_id":"automation.{slug}"}' \
     | jq -r '.data.unique_id'
   ```
2. List recent traces using the resolved `unique_id`:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"trace/list","domain":"automation","item_id":"{unique_id}"}'
   ```
   For scripts: `"domain":"script"`.
3. For detailed trace (specific run), **save to file** (traces can be large):
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"trace/get","domain":"automation","item_id":"{unique_id}","run_id":"{run_id}"}' > /tmp/ha-trace-{run_id}.json
   jq empty /tmp/ha-trace-{run_id}.json
   ```
   Read the file with your native file-reading tool.
4. **Trace analysis checklist** — for each trace, determine:
   - When it ran (timestamp)
   - Trigger: what fired (or didn't)
   - Conditions: which passed/failed
   - Actions: what executed, any errors
   - Result: success/error/aborted
5. If traces don't cover the relevant period: check `last_changed` via `/api/states/{entity_id}` as indirect evidence. HA keeps only the last 5 traces.
6. Before presenting trace conclusions: verify `item_id` in trace data matches the target's `unique_id` — not just a name/regex match. see `skills/ha-nova/SKILL.md` → Claim-Evidence Binding.

## Latency Policy

- no agent dispatch for simple reads
- no proactive `/health` preflight
- no exploratory retry loops without concrete failure

## Safety

- never guess ids
- if multiple close matches, ask one selection question
