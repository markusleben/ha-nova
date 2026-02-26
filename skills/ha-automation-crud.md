---
name: ha-automation-crud
description: Manage automation configs via Home Assistant REST automation config endpoints.
---

# HA Automation CRUD

## Purpose

Create, read, update, and delete automations through REST config endpoints, then reload and verify.

## Required Inputs

- `HA_URL`
- `HA_TOKEN` (Long-Lived Token)

## Primary Endpoints

- List runtime automations:
  - `GET {HA_URL}/api/states` (filter `automation.*`)
- Read config:
  - `GET {HA_URL}/api/config/automation/config/{id}`
- Create/Update config:
  - `POST {HA_URL}/api/config/automation/config/{id}`
- Delete config:
  - `DELETE {HA_URL}/api/config/automation/config/{id}`
- Reload:
  - `POST {HA_URL}/api/services/automation/reload`

## CRUD Flow

### List
1. Call `GET /api/states`.
2. Filter to `entity_id` starting with `automation.`.

### Get
1. Resolve config id (not full entity id).
2. Call `GET /api/config/automation/config/{id}`.

### Create / Update
1. Build full automation config (`alias`, `triggers`, `conditions`, `actions`, `mode`).
2. Preview full config JSON/YAML-equivalent to user.
3. Ask explicit confirmation.
4. `POST /api/config/automation/config/{id}`.
5. `POST /api/services/automation/reload`.
6. Verify with `GET /api/states/automation.{id}` where naming matches slug.

### Delete
1. Preview target automation id.
2. Ask explicit confirmation.
3. `DELETE /api/config/automation/config/{id}`.
4. `POST /api/services/automation/reload`.
5. Verify target is absent or unavailable in states list.

## Safety Rules

- Never overwrite unknown IDs.
- Never execute create/update/delete without preview + confirmation.
- On ambiguity in ID mapping (`id` vs `entity_id`), resolve explicitly before write.
