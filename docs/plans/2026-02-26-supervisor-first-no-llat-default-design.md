# Supervisor-First Auth: No-LLAT Default (Mini Spec)

Date: 2026-02-26

## Goal

Make App runtime and contributor flow work by default without LLAT, while preserving optional LLAT for full-scope upstream access where needed.

## Context

Live validation showed:
- `SUPERVISOR_TOKEN -> http://supervisor/core/api/...` works for automation config CRUD.
- `HA_LLAT -> http://homeassistant:8123/api/...` also works.
- Cross-combinations fail (`401`), so token and upstream URL must match.

A practical blocker was `ha_llat: null` in app options. For optional password fields, `null` can break option validation/start paths.

## Decisions

1. Keep `ha_llat` optional.
2. Use Supervisor-first behavior in App runtime when LLAT is not provided.
3. Avoid `null` for optional `ha_llat` defaults and option writes.
4. Keep degraded WS behavior without LLAT (explicit error), while REST remains available.

## Scope

- Update App config default for `ha_llat` to an empty string (`""`) instead of `null`.
- Update smoke E2E script to sanitize optional fields and never submit `ha_llat: null`.
- Clarify warning text: fallback is limited WebSocket scope, not generic limited API scope.
- Update affected tests.

## Out of Scope

- No redesign of token resolver precedence.
- No new endpoints.
- No backward-compatibility fallback additions.
