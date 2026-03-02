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

## Guardrails

- never guess entity IDs
- no writes
- no proactive doctor before real failure
