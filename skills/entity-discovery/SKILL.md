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

Verify relay CLI: `~/.config/ha-nova/relay health`
If this fails: `npm run onboarding:macos`

## Flow

### Step 1: Search entity registry

Entity registry uses compact abbreviated keys: `ei`=entity_id, `en`=name, `ai`=area_id.

Search both entity_id and name. Use short keyword stems to handle spelling variants. Always limit to 20 results.

```bash
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/list_for_display"}' \
  | jq '[.data.entities[] | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:20]'
```

**If 0 results:** try synonyms, alternative terms, or shorter keyword stems. Use OR for multiple variants: `test("kw1|kw2|kw3";"i")`.
**If too many:** narrow with AND: `test("kw1";"i") and test("kw2";"i")`.
**Never** dump entire domains without a user-intent keyword.

### Step 2: Get state or config

```bash
# State
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/states/{entity_id}"}'

# Automation/script config — always resolve unique_id first (see relay-api.md → ID Types)
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/get","entity_id":"automation.{slug}"}' | jq -r '.data.unique_id'
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/automation/config/{unique_id}"}' | jq 'if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end'
# For scripts: use "entity_id":"script.{slug}" and /api/config/script/config/{unique_id}
```

### Step 3: Find automations related to a device or area

Automations rarely have `area_id` set. When user asks "automations for X in room Y":

1. Resolve room name to area_id:
   `~/.config/ha-nova/relay ws -d '{"type":"config/area_registry/list"}'` | filter by name
2. Find entities in that area: filter entity registry with `select(.ai == "area_id")`
3. Use `search/related` to find automations that reference those entities:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"search/related","item_type":"entity","item_id":"{entity_id}"}'
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
