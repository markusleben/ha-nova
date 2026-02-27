---
name: ha-control
description: Device control scope definition for App + Relay end-user mode with internal contributor direct-REST fallback.
---

# HA Control

## Purpose

Define deterministic handling for device control requests in the current App + Relay end-user MVP.

## Modes

- End-user mode (default):
  - App + Relay only (user-facing).
  - No client-side LLAT.
  - Device control via relay is not exposed yet.
- Contributor mode (internal/dev-only, explicit):
  - `HA_URL`
  - `HA_LLAT`
  - direct Home Assistant REST service calls.

## End-user Behavior (Mandatory)

1. Do not attempt direct REST control.
2. Return a clear scope message:
   - control write path is not exposed in relay-only MVP.
   - keep user on App + Relay onboarding path.
3. Offer read-only alternatives (for example entity discovery/state listing).

## Contributor Behavior (Explicit Only)

Use direct REST service calls:
- `POST {HA_URL}/api/services/{domain}/{service}`
- verify with `GET {HA_URL}/api/states/{entity_id}` when practical.

Safety:
- resolve entity IDs first (`ha-entities`).
- preview payload before execution.
- require explicit confirmation for writes.
