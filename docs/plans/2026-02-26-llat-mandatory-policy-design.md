# LLAT Mandatory Policy Design

Date: 2026-02-26

## Goal

Enforce a deterministic MVP contract: Home Assistant operations require LLAT.
No degraded runtime mode. No Supervisor-token fallback for upstream WebSocket/REST capability selection.

## Decision

1. `HA_LLAT` is mandatory for runtime startup and onboarding completion.
2. `SUPERVISOR_TOKEN` remains a contributor/deploy helper only (Supervisor API control path), not upstream auth fallback.
3. Relay readiness/doctor fail when upstream WS is degraded (`ha_ws_connected=false`).
4. Documentation and skill contracts must describe LLAT as required, not optional.

## Scope

- Runtime auth resolution and startup gates
- App entrypoint and app option schema
- macOS onboarding scripts (`setup`, `doctor`, `ready`, `env`)
- Active skill docs (`ha-nova`, `ha-onboarding`, `ha-entities`, `ha-automation-crud`)
- User/contributor docs and acceptance docs
- Contract/unit tests affected by capability semantics

## Non-Goals

- Backward-compatibility fallback behavior
- Broad historical doc rewrite outside active project docs (old plan docs remain historical artifacts)

## Validation

- Runtime/tests: token resolver, env parser, runtime bootstrap tests pass with mandatory LLAT model.
- Onboarding/tests: setup enforces LLAT; doctor/ready/env fail without LLAT; contract tests updated.
- Docs/skills: no active guidance says LLAT is optional.
