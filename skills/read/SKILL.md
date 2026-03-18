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

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based relay requests as the default path:

1. Write JSON payloads with the client's native file-writing tool.
2. Use `ha-nova relay ws --data-file <payload-file>`.
3. Use `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>` when a body is needed.
4. Use `--jq-file <filter-file>` for non-trivial filters and `--out <result-file>` for large responses.

## Flow

### Listing automations / scripts

Use the compact entity registry (abbreviated keys: `ei`=entity_id, `en`=name, `ai`=area_id):

Create `<payload-file>` with:

```json
{"type":"config/entity_registry/list_for_display"}
```

Then run:

```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
```

Write `<filter-file>` with one of:

```jq
[.data.entities[] | select(.ei | startswith("automation.")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:30]
```

```jq
[.data.entities[] | select(.ei | startswith("script.")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:30]
```

### Keyword search

Use short keyword stems to handle spelling variants. Always limit results.

```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
```

Write `<filter-file>` with:

```jq
[.data.entities[] | select(.ei | startswith("automation.")) | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:20]
```

If 0 results: try synonyms or shorter stems: `test("kw1|kw2";"i")`. Never dump entire domains.

For "automations in room X": use entity-discovery skill's `search/related` flow.

### Reading a single config

Always resolve the config key via entity registry first — the entity_id slug and the config key often differ (HA uses numeric `unique_id` internally for UI-created items).

**Always save config reads to a temp file** to avoid shell output truncation (complex automations can be 10–30 KB JSON):

1. Resolve `unique_id`:
   - create `<payload-file>` with `{"type":"config/entity_registry/get","entity_id":"automation.{slug}"}`
   - run `ha-nova relay ws --data-file <payload-file> --jq .data.unique_id`
   - for scripts: use `script.{slug}`
2. Fetch config into `<result-file>`:
   - `ha-nova relay core --method GET --path /api/config/automation/config/{unique_id} --jq-file <filter-file> --out <result-file>`
   - for scripts: `/api/config/script/config/{unique_id}`
   - write `<filter-file>` with:
     ```jq
     if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
     ```
3. Validate JSON:
   - `ha-nova relay jq --file <result-file> -e --jq-file <filter-file>`
   - write `<filter-file>` with `type == "object"`
4. For counts: use `ha-nova relay jq --file <result-file> length`
5. For non-trivial follow-up JSON transforms: use `ha-nova relay jq --file <result-file> --jq-file <filter-file>`

**Read the file using your native file-reading tool** (Claude: `Read`, Gemini: file read, Cursor: open file). Do NOT use `cat`, `head`, or shell output — these may truncate.

**Never analyze a config from shell output.** Always read the temp file with your file-reading tool.

### Related entities

Find automations/scripts that use a specific entity:

Create `<payload-file>` with:

```json
{"type":"search/related","item_type":"entity","item_id":"{entity_id}"}
```

Then run:

```text
ha-nova relay ws --data-file <payload-file>
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
   Create `<payload-file>` with the `config/entity_registry/get` request, then run:
   ```text
   ha-nova relay ws --data-file <payload-file> --jq .data.unique_id
   ```
2. List recent traces using the resolved `unique_id`:
   Create `<payload-file>` with:
   ```json
   {"type":"trace/list","domain":"automation","item_id":"{unique_id}"}
   ```
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```
   For scripts: `"domain":"script"`.
3. For detailed trace (specific run), **save to file** (traces can be large):
   Create `<payload-file>` with:
   ```json
   {"type":"trace/get","domain":"automation","item_id":"{unique_id}","run_id":"{run_id}"}
   ```
   ```text
   ha-nova relay ws --data-file <payload-file> --out <result-file>
   ha-nova relay jq --file <result-file> empty
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
