---
name: entity-discovery
description: Use when searching or resolving Home Assistant entities by name, room, or domain through HA NOVA Relay.
---

# HA NOVA Entity Discovery


## Scope

Use for:
- listing entities by domain
- searching entities by user phrase
- bulk inventory by `prefix`, `domain`, `area`, or `label`
- resolving likely targets before writes

Read-only behavior.
- No `POST`, `PUT`, `PATCH`, or `DELETE` relay writes.
- If the user moves from discovery to mutation, stop after resolution and hand off to the write-capable skill.

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based relay requests by default:
- `ha-nova relay ws --data-file <payload-file>`
- `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>` when a body is needed
- `--jq-file <filter-file>` for non-trivial filters; keep inline `--jq` for short selectors only
- `--out <result-file>` for large reads
- On Windows PowerShell, never chain commands with `&&` or `||`; run separate shell commands instead.
- Never call external `jq`; use relay-native filters or `ha-nova relay jq --file <result-file> ...`.

## Flow

### Step 1: Search entity registry

Entity registry uses compact abbreviated keys: `ei`=entity_id, `en`=name, `ai`=area_id.

Search both entity_id and name. Use short keyword stems to handle spelling variants. Always limit to 20 results.

For bulk selectors, follow `skills/ha-nova/bulk-patterns.md`.
- `prefix`: case-insensitive prefix match on the entity_id suffix and display name
- `domain`: exact domain filter
- `area`: resolve the area, then use `search/related` as the primary shortlist source; use `ai` only as optional extra evidence when present
- `label`: escalate to the full registry only when label evidence is required; `config/entity_registry/list` returns the entity array directly in `.data`
- `helper` + `area`: not a first-class bulk selector contract; do not imply room-owned helper discovery unless live helper-area semantics are explicitly defined

For domain counts or domain shortlists:
- count only the requested domain unless the user explicitly asks for heuristics or related domains
- use `--jq-file <filter-file>` for the count filter
- if you need a follow-up count from a saved file, use `ha-nova relay jq --file <result-file> length`

Create `<payload-file>` with `{"type":"config/entity_registry/list_for_display"}`, then run:

```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
```

Write `<filter-file>` with:

```jq
[.data.entities[] | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:20]
```

This generic `test("KEYWORD";"i")` example is for free-text search, not explicit `prefix` matching.
For an explicit prefix selector, match the suffix and display name with `startswith(...)`, not loose substring search.

**If 0 results:** try synonyms, alternative terms, or shorter keyword stems. Use OR for multiple variants: `test("kw1|kw2|kw3";"i")`.
**If too many:** narrow with AND: `test("kw1";"i") and test("kw2";"i")`.
**Never** dump entire domains without a user-intent keyword.

When the task is multi-target inventory:
- save the shortlist with `--out <result-file>`
- do not trim to 20 inside the initial selector filter
- dedupe first, then sort deterministically, then compute the exact matched count, then apply the 20-row display cap
- keep ordering deterministic: domain, then entity_id
- return the exact matched count separately from the displayed rows

### Step 2: Get state or config

```text
# State
ha-nova relay core --method GET --path /api/states/{entity_id}

# Automation/script config — always resolve unique_id first (see relay-api.md → ID Types)
ha-nova relay ws --data-file <payload-file> --out <registry-file>
ha-nova relay jq -r --file <registry-file> '.data.unique_id'
ha-nova relay core --method GET --path /api/config/automation/config/{unique_id} --jq-file <filter-file> --out <result-file>
# For scripts: use script.{slug} and /api/config/script/config/{unique_id}
```

For `<filter-file>`, prefer copying `skills/ha-nova/config-body-filter.jq`; if the canonical file is unavailable (flat-copy installs), recreate it with exactly:

```jq
if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
```

### Step 3: Find automations related to a device or area

Automations rarely have reliable direct `area_id` data in the compact registry. When user asks "automations for X in room Y":

1. Resolve room name to area_id:
   `ha-nova relay ws --data-file <payload-file>` then filter by name with `--jq`
2. Query the area directly:
   `{"type":"search/related","item_type":"area","item_id":"<area_id>"}`
3. Treat the response as a keyed object, not an array:
   - automation discovery uses `.data.automation`
   - script discovery uses `.data.script`
   - entity discovery uses `.data.entity`
4. If automation/script keys are absent and only area entities are present, use `search/related` on those entities to derive the automation/script shortlist:
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```

This is more reliable than keyword search or assuming `.ai` is populated for room-based queries.

### Step 4: Return shortlist

- `entity_id`, `friendly_name`, `state` (if fetched), short relevance reason
- for bulk inventory: filter used, matched count, displayed rows
- do not fetch full YAML for every matched item in one response

**IMPORTANT:** Never dump raw `get_states` — it returns thousands of entities with full attributes.

## Matching Rules

- area-first bulk discovery by room/area uses `search/related` on the resolved area before keyword heuristics
- exact `entity_id` match wins
- keyword match on entity_id + name second

If ambiguity remains: present top candidates (max 10), ask one selection question.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Return the Step 4 shortlist shape; never dump raw `get_states` output.

## Safety

- Read-only — this skill never modifies Home Assistant state or config.
- No `POST`, `PUT`, `PATCH`, or `DELETE` relay writes.
- All communication with Home Assistant goes through `ha-nova relay` exclusively.

## Guardrails

- never guess entity IDs
- cap displayed shortlist rows at 20 only after exact matched-count computation for bulk inventory
- no writes
- no proactive doctor before real failure
