---
name: read
description: List/read Home Assistant automation and script configs through HA NOVA Relay. For analysis, use ha-nova:review.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay App in Home Assistant.
---

# HA NOVA Read


## Scope

Read only:
- `automation.list`
- `automation.read`
- `script.list`
- `script.read`
- `automation.trace`
- `script.trace`

Not for helpers — use `ha-nova:helper`.

Multi-target is inventory-only:
- use `skills/ha-nova/bulk-patterns.md` for `prefix` / `domain` / `area` / `label`
- keep full YAML reads single-target only

No writes.
- MUST NOT issue `POST`, `PUT`, `PATCH`, or `DELETE` relay requests.
- MUST NOT call service endpoints or any other mutation path learned during the read flow.
- If the task becomes a config change, hand off to `ha-nova:write` with resolved IDs and current config.

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based requests:

1. Write final JSON with the client's native file-writing tool. Do not create placeholder templates and patch them later with `perl -0pi`, `sed -i`, or similar rewrites.
2. Run `ha-nova relay ws --data-file <payload-file>`.
3. Use `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>`.
4. Use `--jq-file <filter-file>` for complex filters and `--out <result-file>` for large responses. `ha-nova relay jq` is single-input; compare files natively.

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

For bulk inventory by `prefix`, `domain`, `area`, or `label`, reuse `skills/ha-nova/bulk-patterns.md` and return the compact table only. For area scope, use the `search/related` area projection rules, not compact-registry `ai`.

### Keyword search

Use short stems; limit results.

```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
```

Write `<filter-file>` with:

```jq
[.data.entities[] | select(.ei | startswith("automation.")) | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:20]
```

If 0 results: try synonyms/shorter stems: `test("kw1|kw2";"i")`.

For "automations in room X": stay inside `read` and follow the area-first `search/related` flow from `skills/ha-nova/bulk-patterns.md`.

### Reading a single config

Resolve the config key via entity registry first; UI-created items often use numeric `unique_id` values.

1. Resolve `unique_id`:
   - create `<payload-file>` with `{"type":"config/entity_registry/get","entity_id":"automation.{slug}"}`
   - run `ha-nova relay ws --data-file <payload-file> --out <registry-file>`
   - then run `ha-nova relay jq -r --file <registry-file> '.data.unique_id'` (POSIX example; on Windows/PowerShell pass the same filter with native argument quoting)
   - write the final `entity_id` value directly into `<payload-file>`; do not use placeholder tokens such as `REPLACE_ENTITY_ID`
   - for scripts: use `script.{slug}`
2. Fetch config into `<result-file>`:
   - `ha-nova relay core --method GET --path /api/config/automation/config/{unique_id} --jq-file <filter-file> --out <result-file>`
   - for scripts: `/api/config/script/config/{unique_id}`
   - prefer copying `skills/ha-nova/config-body-filter.jq` to `<filter-file>`
   - if unavailable (flat-copy installs), recreate exactly:
     ```jq
     if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
     ```
   - if the contents differ, overwrite the same `<filter-file>` with the canonical line before the first config read; do not create alternate config-filter filenames
   - POSIX heredocs shown elsewhere are examples only; on Windows/PowerShell preserve the same jq body with native file writing
3. Validate:
   - `ha-nova relay jq --file <result-file> -e --jq-file <filter-file>`
   - use `type == "object"`
4. For counts or follow-up transforms, use `ha-nova relay jq --file <result-file>` with `length` or `--jq-file <filter-file>`.

Read the saved file with the native file-reading tool. Do not analyze configs from shell output.

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

If id is ambiguous, ask one clarifying question. Never use raw `get_states`.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

After reading a config, present:

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
alias: ...
triggers: ...
actions: ...
```

For list operations, use a compact table:

```
| Entity ID | Name | Area |
|-----------|------|------|
```

Never show raw JSON to the user.

## Trace Debugging

For trace queries:

Prefer CLI helpers: `ha-nova trace latest <entity> --json`, `ha-nova trace list <entity> --json`, `ha-nova trace get <entity> <run_id> --json`. They resolve `unique_id` and normalize trace shapes. If none exist, explain that Home Assistant keeps only recent traces and YAML automations/scripts need an `id`.

Manual relay path:

1. Resolve the `unique_id`. **`item_id` requires the `unique_id`, NOT the entity_id slug**.
   Create `<payload-file>` with the `config/entity_registry/get` request, then run:
   ```text
   ha-nova relay ws --data-file <payload-file> --out <registry-file>
   ha-nova relay jq -r --file <registry-file> '.data.unique_id'
   ```
2. List recent traces using the resolved `unique_id`:
   Create `<payload-file>` with:
   ```json
   {"type":"trace/list","domain":"automation","item_id":"{unique_id}"}
   ```
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```
   For scripts: `"domain":"script"`. Trace lists may be `.data` array or `.data.traces`; inspect first.
   Pick the newest real `run_id`. Do not use a list index such as `"0"` or `"4"`.
3. For a detailed trace, prefer `ha-nova trace get`; otherwise save `trace/get` to `<result-file>`:
   Create `<payload-file>` with:
   ```json
   {"type":"trace/get","domain":"automation","item_id":"{unique_id}","run_id":"{run_id}"}
   ```
   ```text
   ha-nova relay ws --data-file <payload-file> --out <result-file>
   ha-nova relay jq --file <result-file> empty
   ```
   Read the file with your native file-reading tool.
4. Summarize timestamp, trigger, conditions, actions, and result.
5. Treat trace config as historical; compare current config separately.
6. If traces do not cover the relevant period, optionally check `last_changed` via `/api/states/{entity_id}`.
7. Before presenting conclusions, verify `item_id` in trace data matches the target's `unique_id`. see `skills/ha-nova/SKILL.md` → Claim-Evidence Binding.

## Latency Policy

- no agent dispatch for simple reads
- no proactive `/health` preflight
- no exploratory retry loops without concrete failure

## Safety

- Read-only skill: never issue mutating relay or service calls.
- For write intent, hand off to the owning skill; unfamiliar writes go through `ha-nova:fallback` first.

- never guess ids
- if multiple close matches, ask one selection question
