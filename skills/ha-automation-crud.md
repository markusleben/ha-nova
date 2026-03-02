---
name: ha-automation-crud
description: Manage automation configs with explicit scope boundaries and best-practice-gated writes.
---

# HA Automation CRUD

## Purpose

Create, read, update, and delete automations, then reload and verify.
Use Relay as the only execution path for end-user automation CRUD.
For `create`/`update`, run with `ha-automation-best-practices` and enforce refresh snapshot gate.

## Required Companion Skill for Writes

For `create` and `update`, always load:
- `ha-automation-best-practices`

Write planning/execution is blocked until its refresh snapshot gate is satisfied.

## Relay Execution Path (Mandatory)

Use `POST {RELAY_BASE_URL}/core` for all automation CRUD requests.
Relay injects App-side LLAT; client-side `HA_LLAT` is not required.

Envelope shape:
- `{"method":"GET|POST|DELETE","path":"/api/...","body":{...}}`

Response contract:
- success flag: `.ok`
- upstream status code: `.data.status`
- upstream payload: `.data.body`
- do not parse top-level `.result` from `/core` responses

Common automation paths in this flow:
- `GET /api/states` (filter `automation.*` in skill)
- `GET /api/config/automation/config/{id}`
- `POST /api/config/automation/config/{id}`
- `DELETE /api/config/automation/config/{id}`
- `POST /api/services/automation/reload` (recovery only)

Known non-list endpoint:
- `GET /api/config/automation/config` (without `{id}`) is not list-capable in this flow and can return `404`.

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
1. Enforce best-practice refresh snapshot gate (`ha-automation-best-practices`).
2. Build full automation config (`alias`, `trigger`, `condition`, `action`, `mode`).
3. Run best-practice checklist validation and include findings in preview.
4. Preview full config JSON/YAML-equivalent to user.
5. Ask tokenized confirmation (`confirm:<token>`).
6. Call `POST /core` with:
   - `{"method":"POST","path":"/api/config/automation/config/{id}","body":<config>}`
7. Verify persistence via `GET /api/config/automation/config/{id}`.
8. Return `Changes applied` only when verification `passed=true`.

### Delete
1. Preview target automation id.
2. Ask tokenized confirmation (`confirm:<token>`).
3. Call `POST /core` with:
   - `{"method":"DELETE","path":"/api/config/automation/config/{id}"}`
4. Return success without default read-back verification.

### Recovery Reload (Only When Needed)
1. If state listing appears stale after writes, call:
   - `POST /core` with `{"method":"POST","path":"/api/services/automation/reload","body":{}}`

## Fast Create/Update DAG (Target <= 6 Calls)

1. In parallel (when client/IDI supports it):
   - discovery read: `/api/states` (resolve entities)
   - existence read: `/api/config/automation/config/{id}` (expected 200/404)
2. Write call: `POST /api/config/automation/config/{id}`
3. Read-back call: `GET /api/config/automation/config/{id}`
4. Optional recovery only on stale symptoms:
   - `POST /api/services/automation/reload`
   - one final `GET /api/config/automation/config/{id}`

## Safety Rules

- Never overwrite unknown IDs.
- Never execute create/update/delete without preview + confirmation.
- Never accept free-text write confirmations; require `confirm:<token>`.
- On ambiguity in ID mapping (`id` vs `entity_id`), resolve explicitly before write.
- For `create`/`update`: no valid best-practice refresh snapshot -> no write.
- Keep create/update path under 6 Relay calls.
- Run independent reads in parallel when possible.
- In zsh snippets, never use loop variable name `path`; use `endpoint`.
