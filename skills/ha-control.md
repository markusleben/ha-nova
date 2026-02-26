---
name: ha-control
description: Control Home Assistant entities through REST service calls with verification.
---

# HA Control

## Purpose

Execute device control actions (lights, switches, climate, covers, media, fans, locks) via REST service calls.

## Required Inputs

- `HA_URL`
- `HA_LLAT` (Long-Lived Access Token for direct REST)

## Primary Endpoint

- `POST {HA_URL}/api/services/{domain}/{service}`

## Control Flow

1. Resolve entity IDs first (use `ha-entities`).
2. Build service payload:
   - required target (`entity_id` list or single)
   - optional `data` fields (for example `brightness_pct`, `temperature`)
3. Preview exact service call payload.
4. Ask explicit confirmation for write action.
5. Execute service call.
6. Verify resulting state via `GET /api/states/{entity_id}` when practical.

## Common Patterns

- Light on:
  - `POST /api/services/light/turn_on` with `{"entity_id":"light.x","brightness_pct":50}`
- Switch off:
  - `POST /api/services/switch/turn_off` with `{"entity_id":"switch.x"}`
- Climate set temperature:
  - `POST /api/services/climate/set_temperature` with `{"entity_id":"climate.x","temperature":21}`

## Error Handling

- 401: token invalid/expired -> request a new LLAT.
- 400: invalid payload/service params -> show offending field.
- 404: bad route/domain/service -> re-check service list (`GET /api/services`).

If `HA_LLAT` is unavailable:
- stop direct REST control flow,
- route user to `ha-onboarding` for token setup guidance.
