---
name: ha-nova-entity-discovery
description: Use when searching or resolving Home Assistant entities by name, room, or domain through HA NOVA Relay.
---

# HA NOVA Entity Discovery

<!-- ha-nova-managed-install repo_root: __HA_NOVA_REPO_ROOT__ -->

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

1. Fetch states: `~/.config/ha-nova/relay ws -d '{"type":"get_states"}'`
2. Parse only object rows where `entity_id` is string.
3. Filter by user intent (domain, room, device keywords).
4. Return shortlist with:
   - `entity_id`
   - `state`
   - `friendly_name`
   - short relevance reason

## Matching Rules

- exact `entity_id` hit wins.
- exact `friendly_name` hit second.
- keyword intersection ranking third.

If ambiguity remains:
- present top candidates
- ask one selection question

If no match:
- state explicitly and request exact entity id or clearer phrase.

## Performance

- Cache `get_states` results within a session; do not re-fetch for every query.
- For a known entity_id, prefer direct state read: `GET /api/states/{entity_id}` via `/core`.

## Area and Device Discovery

For area-based targeting (e.g., "all lights in the bedroom"):
- `~/.config/ha-nova/relay ws -d '{"type":"config/area_registry/list"}'`
- `~/.config/ha-nova/relay ws -d '{"type":"config/device_registry/list"}'`
- `~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/list"}'`
- Match area name to area_id, then filter entities:
  1. Direct match: entity registry entry has `area_id` matching target area.
  2. Device fallback: if entity `area_id` is null, look up its `device_id` in device registry — if that device's `area_id` matches, include the entity.

## Guardrails

- never guess entity IDs
- no writes
- no proactive doctor before real failure
