---
name: entity-discovery
description: Use when searching or resolving Home Assistant entities by name, room, or domain through HA NOVA Relay.
---

# HA NOVA Entity Discovery


## Scope

Use for:
- listing entities by domain
- searching entities by user phrase
- resolving likely targets before writes

Read-only behavior.

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

**If 0 results:** try synonyms, alternative terms, or shorter keyword stems. Use OR for multiple variants: `test("kw1|kw2|kw3";"i")`.
**If too many:** narrow with AND: `test("kw1";"i") and test("kw2";"i")`.
**Never** dump entire domains without a user-intent keyword.

### Step 2: Get state or config

```text
# State
ha-nova relay core --method GET --path /api/states/{entity_id}

# Automation/script config — always resolve unique_id first (see relay-api.md → ID Types)
ha-nova relay ws --data-file <payload-file> --jq .data.unique_id
ha-nova relay core --method GET --path /api/config/automation/config/{unique_id} --jq-file <filter-file> --out <result-file>
# For scripts: use script.{slug} and /api/config/script/config/{unique_id}
```

Write `<filter-file>` with:

```jq
if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
```

### Step 3: Find automations related to a device or area

Automations rarely have `area_id` set. When user asks "automations for X in room Y":

1. Resolve room name to area_id:
   `ha-nova relay ws --data-file <payload-file>` then filter by name with `--jq`
2. Find entities in that area: filter entity registry with `select(.ai == "area_id")`
3. Use `search/related` to find automations that reference those entities:
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```

This is more reliable than keyword search for room-based queries.

### Step 4: Return shortlist

- `entity_id`, `friendly_name`, `state` (if fetched), short relevance reason

**IMPORTANT:** Never dump raw `get_states` — it returns thousands of entities with full attributes.

## Matching Rules

- exact `entity_id` match wins
- keyword match on entity_id + name second
- area → device → `search/related` third

If ambiguity remains: present top candidates (max 10), ask one selection question.

## Guardrails

- never guess entity IDs
- always limit results: `| .[0:20]`
- no writes
- no proactive doctor before real failure
