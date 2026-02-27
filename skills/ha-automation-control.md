---
name: ha-automation-control
description: Automation runtime control scope definition for App + Relay end-user mode with internal contributor direct-REST fallback.
---

# HA Automation Control

## Purpose

Define deterministic handling for automation runtime control (`turn_on`, `turn_off`, `toggle`, `trigger`) in the current App + Relay end-user MVP.

## Modes

- End-user mode (default):
  - App + Relay only (user-facing).
  - No client-side LLAT.
  - Automation runtime control via relay is not exposed yet.
- Contributor mode (internal/dev-only, explicit):
  - `HA_URL`
  - `HA_LLAT`
  - direct Home Assistant REST service calls.

## End-user Behavior (Mandatory)

1. Do not attempt direct REST automation control.
2. Return a clear scope message:
   - automation runtime control write path is not exposed in relay-only MVP.
3. Keep user on App + Relay onboarding path and offer read-only alternatives.

## Contributor Behavior (Explicit Only)

Use direct REST service calls:
- `POST {HA_URL}/api/services/automation/{service}`

Safety:
- resolve automation entity IDs first.
- preview payload before execution.
- require explicit confirmation for writes.
