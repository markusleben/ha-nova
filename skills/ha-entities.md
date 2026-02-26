---
name: ha-entities
description: Discover, list, and filter Home Assistant entities via App + Relay first, then direct REST fallback when needed.
---

# HA Entities

## Purpose

Resolve exact entity IDs before control or write operations.

## Capability Inputs

- Path A: App + Relay (preferred)
  - `RELAY_BASE_URL`
  - `RELAY_AUTH_TOKEN`
- Path B: Direct Home Assistant REST (fallback)
  - `HA_URL`
  - `HA_LLAT`

## Capability Selection (Mandatory)

1. Require `HA_LLAT` before entity discovery starts.
2. Prefer the fastest viable path: Path A when Relay is reachable and reports upstream WS connected (`ha_ws_connected=true`).
3. Otherwise use Path B.
4. If neither path is available:
   - stop entity discovery,
   - route user to `ha-onboarding` with exact missing capability.

## Endpoints by Path

- Path A (Relay)
  - `GET {RELAY_BASE_URL}/health`
  - `POST {RELAY_BASE_URL}/ws` with `{"type":"get_states"}`
- Path B (Direct REST)
  - `GET {HA_URL}/api/states`
  - `GET {HA_URL}/api/states/{entity_id}`

## Workflow

1. Load all states once:
   - Path A: `POST /ws` with `{"type":"get_states"}`
   - Path B: `GET /api/states`
2. Filter client-side by:
   - domain (`light.`, `switch.`, `sensor.`, `automation.`)
   - `friendly_name`
   - area/device hints embedded in `entity_id` or attributes
3. For exact detail (Path B only), call `GET /api/states/{entity_id}` when needed.

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
