---
name: ha-automation-crud
description: Manage automation configs with capability-aware CRUD flow (App-context Supervisor path or direct HA REST with LLAT).
---

# HA Automation CRUD

## Purpose

Create, read, update, and delete automations, then reload and verify.
Use the best available capability path for the current session.

## Capability Inputs

- Path A: App-context Supervisor path
  - `SUPERVISOR_TOKEN` with access to `http://supervisor/core/api` (typically inside App runtime context)
- Path B: Direct Home Assistant REST
  - `HA_URL`
  - `HA_LLAT` (Long-Lived Access Token)

## Capability Selection (Mandatory)

1. Prefer Path A when available (`SUPERVISOR_TOKEN` + `/core/api` access).
2. Otherwise use Path B (`HA_LLAT`).
3. If neither path is available:
   - stop CRUD execution,
   - route user to `ha-onboarding` with exact missing capability.

## Endpoints by Path

- Path A (App-context, base: `http://supervisor/core/api`)
  - list runtime automations: `GET /states` (filter `automation.*`)
  - get config: `GET /config/automation/config/{id}`
  - create/update config: `POST /config/automation/config/{id}`
  - delete config: `DELETE /config/automation/config/{id}`
  - reload: `POST /services/automation/reload`

- Path B (Direct REST, base: `{HA_URL}/api`)
  - list runtime automations: `GET /states` (filter `automation.*`)
  - get config: `GET /config/automation/config/{id}`
  - create/update config: `POST /config/automation/config/{id}`
  - delete config: `DELETE /config/automation/config/{id}`
  - reload: `POST /services/automation/reload`

## CRUD Flow (Path-Independent)

### List
1. Call `GET /states`.
2. Filter to `entity_id` starting with `automation.`.

### Get
1. Resolve config id (not full entity id).
2. Call `GET /config/automation/config/{id}`.

### Create / Update
1. Build full automation config (`alias`, `trigger`, `condition`, `action`, `mode`).
2. Preview full config JSON/YAML-equivalent to user.
3. Ask explicit confirmation.
4. `POST /config/automation/config/{id}`.
5. `POST /services/automation/reload`.
6. Verify with:
   - `GET /config/automation/config/{id}` and
   - `GET /states/automation.{id}` when state entity naming matches slug.

### Delete
1. Preview target automation id.
2. Ask explicit confirmation.
3. `DELETE /config/automation/config/{id}`.
4. `POST /services/automation/reload`.
5. Verify target is absent or unavailable in states list.

## Safety Rules

- Never overwrite unknown IDs.
- Never execute create/update/delete without preview + confirmation.
- On ambiguity in ID mapping (`id` vs `entity_id`), resolve explicitly before write.
- Use deterministic capability selection; do not mix paths mid-operation.
