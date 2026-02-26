# Phase 1a.3 Acceptance Matrix

Date: 2026-02-26  
Branch: `feat/phase-1a3-packaging-ha-app`

## Packaging Checks

| Check | Command / Evidence | Expected |
|---|---|---|
| Add-on config contract | `npm test -- tests/addon/config-contract.test.ts` | PASS |
| Add-on run contract | `npm test -- tests/addon/run-contract.test.ts` | PASS |
| Smoke scripts contract | `npm test -- tests/addon/smoke-scripts-contract.test.ts` | PASS |

## Local Container Smoke

| Check | Command | Expected |
|---|---|---|
| Build image | `npm run smoke:addon:build` | image builds successfully |
| Run container | `npm run smoke:addon:run` | container starts on `8791` |
| HTTP smoke | `npm run smoke:addon:http` | `/health` + `/ws` return 2xx |

## Home Assistant Runtime Smoke (Manual)

1. Add local app/add-on repository and refresh store.
2. Install `HA Nova Bridge`.
3. Configure options:
   - `bridge_auth_token`: required
   - `ha_llat`: optional but required for full WS scope
4. Start app/add-on.
5. Verify health call:
   - `GET /health` with `Authorization: Bearer <bridge_auth_token>`
6. Verify WS call:
   - `POST /ws` with payload `{"type":"ping"}`

Expected:
- with LLAT: WS proxy works for allowlisted calls
- without LLAT: explicit `UPSTREAM_WS_ERROR` explains missing LLAT

## Supervisor API Assisted Checks

- Validate options before write:
  - `POST http://supervisor/addons/self/options/validate`
- Persist options:
  - `POST http://supervisor/addons/self/options`
- Restart app/add-on:
  - `POST http://supervisor/addons/self/restart`

Use header:
- `Authorization: Bearer ${SUPERVISOR_TOKEN}`

## Persistence Check

1. Set `ha_llat` once (UI or `npm run seed:llat -- '<TOKEN>'`).
2. Restart app/add-on.
3. Repeat `/ws` smoke.

Expected:
- `ha_llat` persists via `/data/options.json` and no manual re-entry needed.
