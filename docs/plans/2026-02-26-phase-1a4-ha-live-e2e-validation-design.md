# Phase 1a.4 HA Live E2E Validation Design

Date: 2026-02-26  
Status: Approved for implementation

## Objective

Create one deterministic live validation flow for `ha-nova` App on Home Assistant:

1. verify app exists in Supervisor (`/addons/<slug>/info`)
2. validate onboarding options (`relay_auth_token`, optional `ha_llat`)
3. optionally apply options and restart app
4. verify relay runtime via `GET /health` and `POST /ws`

## Constraints

- App-first wording in docs and scripts.
- No backward compatibility paths (`HA_TOKEN`, `ADDON_SLUG`, mixed auth fallbacks).
- Keep relay runtime unchanged; only add validation tooling.
- Keep technical Supervisor endpoint path `/addons/...`.

## Inputs

Required:
- `SUPERVISOR_TOKEN`
- `RELAY_BASE_URL`
- `RELAY_AUTH_TOKEN`

Optional:
- `APP_SLUG` (default `self`)
- `SUPERVISOR_URL` (default `http://supervisor`)
- `HA_LLAT` (for full WS scope)
- `--apply` (write options + restart app)

## Expected Outcomes

- Without `--apply`: pure preflight check (`info`, `validate`, `health/ws`).
- With `--apply`: options persisted + restart executed before HTTP checks.
- If `HA_LLAT` set: `/ws` must return `200`.
- If `HA_LLAT` missing: `/ws` may return `200` or structured degraded `502` (`UPSTREAM_WS_ERROR`).

## Deliverables

- `scripts/smoke/ha-app-e2e.mjs`
- `package.json` script entry
- docs update in `docs/phase-1a3-acceptance.md`
- contract test update for smoke scripts list
