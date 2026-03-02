---
name: ha-automation-crud
description: Manage automation configs with explicit scope boundaries and best-practice-gated writes.
---

# HA Automation CRUD

## Migration Note (Compatibility Shim)

This file remains valid during modular migration, but source-of-truth modules are:
- `skills/ha-nova/modules/automation/resolve.md`
- `skills/ha-nova/modules/automation/create-update.md`
- `skills/ha-nova/modules/automation/delete.md`
- `skills/ha-nova/modules/automation/read.md`

Router and lazy-discovery rules are defined in `skills/ha-nova.md` + `skills/ha-nova/core/discovery-map.md`.

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

### User-Facing Impact Schema (Mandatory)

For `create`/`update`/`delete` previews and results, render domain-first fields:
1. `Automation Name`
2. `Automation Goal`
3. `Entities Used`
4. `Behavior Summary`
5. `Next Step`

Normal success path:
- keep orchestration internals hidden (`A1/A2/A3`, fan-out details, gate counters)
- show only user-facing behavior + affected objects

### Delete
1. Preview target automation id.
2. Ask tokenized confirmation (`confirm:<token>`).
3. Call `POST /core` with:
   - `{"method":"DELETE","path":"/api/config/automation/config/{id}"}`
4. Return success when delete status is `200`/`204`; optional verify-absent read-back only when needed.

### Recovery Reload (Only When Needed)
1. If state listing appears stale after writes, call:
   - `POST /core` with `{"method":"POST","path":"/api/services/automation/reload","body":{}}`

## Automation Fast-Pass Mapping (MVP)

Use the reusable blocks defined in `ha-nova`:
- `B0_ENV` + `B1_STATE_SNAPSHOT` + `B2_ENTITY_RESOLVE` + `B3_ID_RESOLVE`
- `B4_BP_GATE` + `B5_BUILD_AUTOMATION` + `B7_RENDER_DOMAIN_PREVIEW`
- `B8_CONFIRM_TOKEN` + `B9_APPLY_WRITE` + `B10_VERIFY_WRITE`

Call budget target (`create`/`update`): <= 6 relay calls in normal path.
Avoid exploratory retries unless a real API/schema error occurs.

## Safety Rules

- Never overwrite unknown IDs.
- Never execute create/update/delete without preview + confirmation.
- Never accept free-text write confirmations; require `confirm:<token>`.
- On ambiguity in ID mapping (`id` vs `entity_id`), resolve explicitly before write.
- For `create`/`update`: no valid best-practice refresh snapshot -> no write.
- Keep create/update path under 6 Relay calls.
- Run independent reads in parallel when possible.
- For complex multiline commands, run in `bash -lc` for shell-stable behavior.
- Avoid shell-specific builtins in normal flow (for example `mapfile`).
- Do not generate temporary helper scripts in normal flow.
