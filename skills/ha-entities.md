---
name: ha-entities
description: Discover, list, and filter Home Assistant entities via App + Relay.
---

# HA Entities

## Purpose

Resolve exact entity IDs before control or write operations.

## Capability Inputs

- App + Relay
  - `RELAY_BASE_URL`
  - `RELAY_AUTH_TOKEN`

## Capability Selection (Mandatory)

1. Use Relay `/ws` as the first and only end-user request path.
2. Do not run separate `/health` checks before `/ws` in normal read flows.
3. If `/ws` fails:
   - run diagnostics (`doctor`) once,
   - route user to `ha-onboarding` with exact failure reason.

## Endpoints

- Relay
  - `GET {RELAY_BASE_URL}/health`
  - `POST {RELAY_BASE_URL}/ws` with `{"type":"get_states"}`

## Fast Workflow

1. Load all states once:
   - `POST /ws` with `{"type":"get_states"}`
2. Filter client-side from `.data` only (avoid recursive `..` selectors).
3. Return requested subset directly.

Canonical one-shot command:

```bash
eval "$(bash scripts/onboarding/macos-onboarding.sh env)" && \
curl -sS -X POST \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  "$RELAY_BASE_URL/ws" \
  -d '{"type":"get_states"}' | \
jq -c '.data // []'
```

First 5 lights example:

```bash
eval "$(bash scripts/onboarding/macos-onboarding.sh env)" && \
curl -sS -X POST \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  "$RELAY_BASE_URL/ws" \
  -d '{"type":"get_states"}' | \
jq -c '(.data // []) | map(select((.entity_id|type)=="string" and (.entity_id|startswith("light.")))
  | {entity_id, state, friendly_name: (.attributes.friendly_name // null)})[:5]'
```

## Output Format

For each returned entity provide:
- `entity_id`
- `state`
- `friendly_name` (if available)
- key attributes relevant to user intent

## Safety Rules

- Never guess entity IDs.
- If multiple candidates match, return shortlist and ask user to pick.
- Prefer exact `entity_id` confirmation before downstream writes.
