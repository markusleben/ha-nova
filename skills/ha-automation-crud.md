---
name: ha-automation-crud
description: Manage automation configs with explicit scope boundaries and best-practice-gated writes.
---

# HA Automation CRUD

## Purpose

Create, read, update, and delete automations, then reload and verify.
Use Relay as the only execution path for end-user automation CRUD.
For `create`/`update`, run with `ha-automation-best-practices` and enforce session refresh gate.

## Required Companion Skill for Writes

For `create` and `update`, always load:
- `ha-automation-best-practices`

Write planning/execution is blocked until its session refresh gate is satisfied.

## Relay Execution Path (Mandatory)

Use `POST {RELAY_BASE_URL}/core` for all automation CRUD requests.
Relay injects App-side LLAT; client-side `HA_LLAT` is not required.

Envelope shape:
- `{"method":"GET|POST|DELETE","path":"/api/...","body":{...}}`

Common automation paths in this flow:
- `GET /api/states` (filter `automation.*` in skill)
- `GET /api/config/automation/config/{id}`
- `POST /api/config/automation/config/{id}`
- `DELETE /api/config/automation/config/{id}`
- `POST /api/services/automation/reload` (recovery only)

MVP simplification:
- Relay `/core` is method+path passthrough for valid `/api/...` requests (no automation-specific allowlist).

## CRUD Flow (Path-Independent)

### List
1. Call `POST /core` with `{"method":"GET","path":"/api/states"}`.
2. Filter to `entity_id` starting with `automation.`.

### Get
1. Resolve config id (not full entity id).
2. Call `POST /core` with `{"method":"GET","path":"/api/config/automation/config/{id}"}`.

### Create / Update
1. Enforce best-practice session refresh gate (`ha-automation-best-practices`).
2. Build full automation config (`alias`, `trigger`, `condition`, `action`, `mode`).
3. Run best-practice checklist validation and include findings in preview.
4. Preview full config JSON/YAML-equivalent to user.
5. Ask explicit confirmation.
6. Call `POST /core` with:
   - `{"method":"POST","path":"/api/config/automation/config/{id}","body":<config>}`
7. Return success without default read-back verification.

### Delete
1. Preview target automation id.
2. Ask explicit confirmation.
3. Call `POST /core` with:
   - `{"method":"DELETE","path":"/api/config/automation/config/{id}"}`
4. Return success without default read-back verification.

### Recovery Reload (Only When Needed)
1. If state listing appears stale after writes, call:
   - `POST /core` with `{"method":"POST","path":"/api/services/automation/reload","body":{}}`

## Safety Rules

- Never overwrite unknown IDs.
- Never execute create/update/delete without preview + confirmation.
- On ambiguity in ID mapping (`id` vs `entity_id`), resolve explicitly before write.
- For `create`/`update`: no best-practice session refresh -> no write.
