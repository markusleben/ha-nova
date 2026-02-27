# LLAT Core URL Default Fix (MVP)

Date: 2026-02-27

## Problem

`doctor` reports:
- LLAT is valid against Home Assistant API.
- Relay `/health` is reachable.
- Relay WebSocket upstream remains degraded (`ha_ws_connected=false`) with `UPSTREAM_WS_CONNECT_ERROR`.

Root cause in runtime defaults:
- LLAT-only auth is enforced.
- Default `HA_URL` still points to `http://supervisor/core`.
- Supervisor proxy path is not the correct default target for LLAT WebSocket auth.

## Decision

Use direct Home Assistant Core URL as runtime default:
- `http://homeassistant:8123`

Keep configuration surface minimal:
- no new user option for `ha_url`
- no fallback branches

## Scope

1. Change default in `src/config/env.ts`.
2. Change App runner default in `app/run`.
3. Update tests that assert default `HA_URL`.
4. Add/extend contract assertion to prevent regression.

## Verification

- `tests/security/auth.test.ts`
- `tests/app/run-contract.test.ts`

Expected outcome:
- New App builds connect upstream WS with LLAT-only model.
- `doctor` should pass when App option `ha_llat` matches Keychain LLAT.
