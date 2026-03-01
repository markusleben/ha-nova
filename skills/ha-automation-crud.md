---
name: ha-automation-crud
description: Manage automation configs with explicit scope boundaries and best-practice-gated writes.
---

# HA Automation CRUD

## Purpose

Create, read, update, and delete automations, then reload and verify.
In App + Relay end-user sessions, fail fast with clear scope guidance.
For `create`/`update`, run with `ha-automation-best-practices` and enforce session refresh gate.

## Capability Modes

- End-user mode (default):
  - App + Relay only (user-facing).
  - No client-side LLAT.
- Contributor mode (internal/dev-only, explicit):
  - `HA_URL`
  - `HA_LLAT`

## Required Companion Skill for Writes

For `create` and `update`, always load:
- `ha-automation-best-practices`

Write planning/execution is blocked until its session refresh gate is satisfied.

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
1. Enforce best-practice session refresh gate (`ha-automation-best-practices`).
2. Build full automation config (`alias`, `trigger`, `condition`, `action`, `mode`).
3. Run best-practice checklist validation and include findings in preview.
4. Preview full config JSON/YAML-equivalent to user.
5. Ask explicit confirmation.
6. `POST /config/automation/config/{id}`.
7. `POST /services/automation/reload`.
8. Verify with:
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
- For `create`/`update`: no best-practice session refresh -> no write.
