# 2026-03-17 Relay Runtime Hardening

## Problem

- `npm run dev` points Node at raw TypeScript ESM sources and fails before startup.
- WS client keeps a stale cached connection after request failure or timeout.
- `/core` requests have no timeout and can hang forever.
- Relay build currently packages `nova/src/skills/contracts/*`, which is test-only skill logic.

## Goals

- Restore a working local relay dev loop.
- Reset WS connection state after request-level failure so the next request can reconnect.
- Bound `/core` upstream latency with an explicit timeout.
- Keep test-only skill contracts out of the relay build artifact.

## Non-Goals

- No new relay business logic.
- No protocol changes to `/health`, `/ws`, or `/core`.
- No rewrite of the skill contract helpers in this pass.

## Decisions

- Use `tsx` as a dev-only runner for `npm run dev`.
- On WS request timeout/error, clear the cached connection immediately.
- Add an HTTP timeout error path with a dedicated code and regression tests.
- Exclude `nova/src/skills/**/*.ts` from `nova/tsconfig.json` so relay bundles stay transport-only.
