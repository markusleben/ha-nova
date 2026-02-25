---
name: ha-nova
description: Bootstrap skill for Home Assistant control with direct REST calls and bridge-backed WS proxy operations.
---

# HA Nova Bootstrap

## Mission

Control Home Assistant via:
- Direct HA REST API for states, services, and automation CRUD.
- Nova Bridge for WS-only operations.

## Required Inputs

- `HA_URL` (for example `http://homeassistant.local:8123`)
- `HA_TOKEN` (user-generated Long-Lived Token, mandatory)
- `BRIDGE_URL` (for example `http://homeassistant.local:8791`)

## Active Skill Catalog (Phase 1a/1b only)

- `ha-onboarding`: first-run setup and connectivity checks.
- `ha-safety`: write safety baseline (preview and confirmation).
- `ha-entities`: discover entities and service capabilities.
- `ha-control`: call services for device control.
- `ha-automation-crud`: create, read, update, delete automations.
- `ha-automation-control`: enable/disable/turn_on/turn_off/toggle automations.

## Routing Rules

1. If user asks setup/connectivity -> load `ha-onboarding`.
2. If user asks read/search/list entities -> load `ha-entities`.
3. If user asks to control devices -> load `ha-control`.
4. If user asks to create/update/delete automation -> load `ha-automation-crud` + `ha-safety`.
5. If user asks to enable/disable/toggle automation -> load `ha-automation-control` + `ha-safety`.
6. For every write operation, always load `ha-safety`.

## API Routing

- Entity states:
  - `GET {HA_URL}/api/states`
  - `GET {HA_URL}/api/states/{entity_id}`
- Service calls:
  - `POST {HA_URL}/api/services/{domain}/{service}`
- Automation CRUD:
  - `GET {HA_URL}/api/config/automation/config/{id}`
  - `POST {HA_URL}/api/config/automation/config/{id}`
  - `DELETE {HA_URL}/api/config/automation/config/{id}`
- Bridge WS proxy:
  - `POST {BRIDGE_URL}/ws` with `{ "type": "..." }`

## Core Safety Baseline

- Never guess entity IDs or service names.
- Resolve ambiguity by listing/filtering first.
- Show preview before write operations.
- Wait for explicit confirmation before execute.
