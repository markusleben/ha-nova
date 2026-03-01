# Relay-Only Automation CRUD MITM Fast Path (Design)

Date: 2026-03-01  
Status: Proposed

## Goal

Enable automation CRUD for end users through Relay only:
- Relay holds and uses LLAT.
- Client never needs `HA_LLAT`.
- Relay remains a thin man-in-the-middle transport.

## Architecture Principles (KISS)

1. Relay does transport only (auth + shape validation + forward + map errors).
2. No automation business logic in Relay.
3. Skill layer owns behavior (preview/confirm/best-practice gate).
4. No proactive preflight (`/health`, `doctor`) before normal CRUD calls.
5. Minimize calls per operation.

## Relay Contract (MITM)

Add one endpoint:
- `POST /core`

Request envelope:
```json
{
  "method": "GET|POST|DELETE",
  "path": "/api/...",
  "body": {}
}
```

Minimal validation (only what is necessary):
- `method` must be `GET|POST|DELETE`.
- `path` must be a relative HA API path starting with `/api/`.
- reject absolute URLs and path traversal patterns.

Forwarding behavior:
- Upstream target: `${HA_URL}${path}`.
- Auth header injected by Relay: `Authorization: Bearer <LLAT from App option ha_llat>`.
- Forward JSON body for `POST`.
- Return upstream status + JSON/body (no transformation beyond existing error envelope mapping).

## Skill Fast Path

### Session rule
- Load env once.
- No proactive readiness checks.
- Diagnose only after call failure.

### `create` / `update`
1. Run best-practice gate once per session (`automation_bp_refreshed` cache).
2. Build payload once.
3. Preview once.
4. Ask confirmation once.
5. Execute one Relay call:
   - `POST /core` -> `POST /api/config/automation/config/{id}`
6. Return success without default read-back.

Nominal path: **1 write call + 1 confirmation**  
First write in session adds one best-practice refresh step.

### `delete`
1. Resolve id only if ambiguous.
2. Preview once.
3. Ask confirmation once.
4. Execute one Relay call:
   - `POST /core` -> `DELETE /api/config/automation/config/{id}`
5. Return success without default read-back.

Nominal path: **1 write call + 1 confirmation**

### `list` / `get`
- `list`: `POST /core` -> `GET /api/states` (filter `automation.*` in skill).
- `get`: `POST /core` -> `GET /api/config/automation/config/{id}`.

## Reload Policy (Performance-first)

- Do not auto-call `automation.reload` after each CRUD write by default.
- If explicit user request or stale-state symptom appears, perform one targeted reload call:
  - `POST /core` -> `POST /api/services/automation/reload`

## Error Policy

- Best-practice refresh failure blocks `create`/`update`.
- Relay upstream failures return existing structured error envelope.
- No hidden fallback path that bypasses Relay or best-practice gate.

## Implementation Files

Runtime:
- `src/index.ts` (register `/core`)
- `src/runtime/start.ts` (wire REST client dependency)
- `src/ha/rest-client.ts` (new, thin upstream caller with keep-alive)
- `src/http/handlers/core-proxy.ts` (new, validate + forward)
- `src/types/api.ts` (request/response types)

Tests:
- `tests/http/core-proxy.test.ts` (new)
- `tests/bootstrap/app-wiring.test.ts`
- `tests/bootstrap/runtime-start.test.ts`
- `tests/http/error-envelope.test.ts`

Skills/contracts:
- `skills/ha-nova.md`
- `.agents/skills/ha-nova/SKILL.md`
- `skills/ha-automation-crud.md`
- `skills/ha-automation-best-practices.md`
- `tests/skills/ha-nova-skill-contract.test.ts`

## Definition of Done

1. End-user automation CRUD executes via Relay without client `HA_LLAT`.
2. Relay stays transport-only (no CRUD decision logic).
3. Write flow remains preview + confirm.
4. Performance path is minimal (single write call in nominal case).
