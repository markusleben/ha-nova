---
name: ha-entities
description: Discover, list, and filter Home Assistant entities via REST states and service metadata.
---

# HA Entities

## Purpose

Resolve exact entity IDs and service capabilities before control or write operations.

## Required Inputs

- `HA_URL`
- `HA_TOKEN` (Long-Lived Token)

## Primary Endpoints

- `GET {HA_URL}/api/states`
- `GET {HA_URL}/api/states/{entity_id}`
- `GET {HA_URL}/api/services`

## Workflow

1. For broad discovery, call `GET /api/states` once.
2. Filter client-side by:
   - domain (`light.`, `switch.`, `sensor.`, `automation.`)
   - `friendly_name`
   - area/device hints embedded in `entity_id` or attributes
3. For exact detail, call `GET /api/states/{entity_id}`.
4. For available actions, call `GET /api/services`.

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
