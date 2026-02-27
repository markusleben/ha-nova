---
name: ha-automation-crud
description: Manage automation configs with explicit scope boundaries for App + Relay end-user MVP.
---

# HA Automation CRUD

## Purpose

Create, read, update, and delete automations, then reload and verify.
In App + Relay end-user sessions, fail fast with clear scope guidance.

## Capability Modes

- End-user mode (default):
  - App + Relay only (user-facing).
  - No client-side LLAT.
- Contributor mode (explicit):
- Contributor mode (internal/dev-only, explicit):
  - `HA_URL`
  - `HA_LLAT`

## Capability Selection (Mandatory)

1. If this is an end-user App + Relay session:
   - stop CRUD execution,
   - explain that automation config CRUD is not yet exposed in relay-only MVP path.
2. If contributor mode with explicit `HA_LLAT` is provided:
   - use direct Home Assistant REST (`{HA_URL}/api/...`) for CRUD.
3. If contributor mode lacks `HA_LLAT`:
   - stop and ask for contributor setup path (not end-user onboarding).

## Contributor Endpoints (Direct REST)

- Base: `{HA_URL}/api`
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
- Use deterministic mode selection; do not mix end-user and contributor paths mid-operation.
