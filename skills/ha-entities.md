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
  - `POST {RELAY_BASE_URL}/ws` with `{"type":"get_states"}`

- Failure diagnostics only
  - `GET {RELAY_BASE_URL}/health`

## Fast Workflow

1. Load all states once:
   - `POST /ws` with `{"type":"get_states"}`
2. Filter client-side from `.data` only and include object states only:
   - `(.data // [])[] | select(type=="object" and (.entity_id|type)=="string")`
3. Do not run schema-probing jq one-offs in normal flow; use canonical filters.
4. Return requested subset directly.

Load env once per shell session:

```bash
NOVA_REPO_ROOT="${NOVA_REPO_ROOT:-${HA_NOVA_REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}}"
if [[ ! -f "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" ]]; then
  echo "ERROR: missing onboarding script at $NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" >&2
  exit 1
fi
eval "$(bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env)"
```

Canonical one-shot command:

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  "$RELAY_BASE_URL/ws" \
  -d '{"type":"get_states"}' | \
jq -c '.data // []'
```

Canonical safe object-only filter:

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  "$RELAY_BASE_URL/ws" \
  -d '{"type":"get_states"}' | \
jq -c '[(.data // [])[] | select(type=="object" and (.entity_id|type)=="string") | {entity_id, state, friendly_name: (.attributes.friendly_name // null)}]'
```

First 5 lights example:

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  "$RELAY_BASE_URL/ws" \
  -d '{"type":"get_states"}' | \
jq -c '[limit(5; (.data // [])[] | select((.entity_id|type)=="string" and (.entity_id|startswith("light.")))
  | {entity_id, state, friendly_name: (.attributes.friendly_name // null)} )]'
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
