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

1. Require Relay health and upstream WS connectivity (`ha_ws_connected=true`).
2. Use Relay `/ws` as the only end-user path.
3. If Relay is unavailable or degraded:
   - stop entity discovery,
   - route user to `ha-onboarding` with exact failure reason.

## Endpoints

- Relay
  - `GET {RELAY_BASE_URL}/health`
  - `POST {RELAY_BASE_URL}/ws` with `{"type":"get_states"}`

## Workflow

1. Load all states once:
   - `POST /ws` with `{"type":"get_states"}`
2. Filter client-side by:
   - domain (`light.`, `switch.`, `sensor.`, `automation.`)
   - `friendly_name`
   - area/device hints embedded in `entity_id` or attributes

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
