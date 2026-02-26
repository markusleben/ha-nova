# Phase 1a.3 App Packaging Acceptance Matrix

Date: 2026-02-26  
Branch: `feat/phase-1a3-packaging-ha-app`

## Packaging Checks (App; technical Supervisor API path remains `/addons/...`)

| Check | Command / Evidence | Expected |
|---|---|---|
| App config contract | `npm test -- tests/app/config-contract.test.ts` | PASS |
| App run contract | `npm test -- tests/app/run-contract.test.ts` | PASS |
| Smoke scripts contract | `npm test -- tests/app/smoke-scripts-contract.test.ts` | PASS |

## Local Container Smoke

| Check | Command | Expected |
|---|---|---|
| Build image | `npm run smoke:app:build` | image builds successfully |
| Run container | `npm run smoke:app:run` | container starts on `8791` |
| HTTP smoke | `npm run smoke:app:http` | `/health` 200; `/ws` either 200 or expected structured 502 in degraded local mode |

## Contributor Deploy Loop (HA Host)

| Check | Command | Expected |
|---|---|---|
| Fast deploy | `npm run deploy:app:fast` | app rebuild + start succeeds |
| Clean deploy (cache break) | `npm run deploy:app:clean` | stale image cache removed; rebuild picks new code |

Guide:
- `docs/contributor-deploy-loop.md`
- `.env.example` -> copy to `.env.local` for contributor setup

## Home Assistant Runtime Smoke (Manual App Flow)

1. Add local app repository and refresh store.
2. Install `HA Nova Bridge`.
3. Configure options:
   - `bridge_auth_token`: required
   - `ha_llat`: optional but required for full WS scope
4. Start app.
5. Verify health call:
   - `GET /health` with `Authorization: Bearer <bridge_auth_token>`
6. Verify WS call:
   - `POST /ws` with payload `{"type":"ping"}`

Expected:
- with LLAT: WS proxy works for allowlisted calls
- without LLAT: explicit `UPSTREAM_WS_ERROR` explains missing LLAT

## Live E2E Script (Supervisor + Bridge)

Use the reproducible E2E runner:

```bash
SUPERVISOR_TOKEN='<token>' \
APP_SLUG='self' \
BRIDGE_BASE_URL='http://homeassistant.local:8791' \
BRIDGE_AUTH_TOKEN='<bridge-auth-token>' \
npm run smoke:app:e2e
```

Apply options + restart in one run:

```bash
SUPERVISOR_TOKEN='<token>' \
APP_SLUG='self' \
BRIDGE_BASE_URL='http://homeassistant.local:8791' \
BRIDGE_AUTH_TOKEN='<bridge-auth-token>' \
HA_LLAT='<user-generated-llat>' \
npm run smoke:app:e2e -- --apply
```

## Supervisor API Assisted Checks

- Validate options before write:
  - `POST http://supervisor/addons/self/options/validate`
- Persist options:
  - `POST http://supervisor/addons/self/options`
- Restart app:
  - `POST http://supervisor/addons/self/restart`

Use header:
- `Authorization: Bearer ${SUPERVISOR_TOKEN}`

## Persistence Check

1. Set `ha_llat` once (UI or `npm run seed:llat -- '<TOKEN>'`).
2. Restart app.
3. Repeat `/ws` smoke.

Expected:
- `ha_llat` persists via `/data/options.json` and no manual re-entry needed.
