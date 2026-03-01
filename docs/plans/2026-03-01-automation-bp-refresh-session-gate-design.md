# Automation Best-Practice Refresh Session Gate (Design)

Date: 2026-03-01  
Status: Implemented (skills + contract tests)

## Goal

Improve automation creation/update flow quality by enforcing current best practices at least once per session.

## Problem

- Existing automation CRUD flow can become stale when Home Assistant guidance changes.
- User requirement: best-practice context must be refreshed per session, not assumed from static skill text only.

## Constraints

- Keep Relay lean (no business logic moved into server).
- Keep skills markdown-first.
- No additional allowlist-style complexity in runtime transport path.

## Solution

1. Add a dedicated cross-cutting skill: `ha-automation-best-practices`.
2. Route automation write intents (`create`/`update`/`delete`) through this skill plus `ha-automation-crud` and `ha-safety`.
3. Enforce a per-session refresh gate for `create`/`update`:
   - must record refresh timestamp and sources in session context.
   - no refresh -> no write.
4. Restrict authoritative sources for refresh to official HA docs/release notes.
5. Keep delete exempt from mandatory refresh but still preview+confirm.

## Why This Shape

- Matches mature agent patterns:
  - static baseline instructions + dynamic session-time refresh.
  - explicit write gates for higher-risk operations.
  - source provenance captured with timestamp.
- Avoids full-time expensive research on every single turn while still ensuring session-level freshness.

## Verification

- Added/updated skill contracts:
  - routing requires `ha-automation-best-practices` for automation writes.
  - CRUD skill contains mandatory refresh gate language for write paths.
