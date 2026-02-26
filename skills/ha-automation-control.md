---
name: ha-automation-control
description: Control existing automations via automation service calls.
---

# HA Automation Control

## Purpose

Enable, disable, toggle, or trigger existing automations through REST service calls.

## Required Inputs

- `HA_URL`
- `HA_TOKEN` (Long-Lived Token)

## Primary Endpoint

- `POST {HA_URL}/api/services/automation/{service}`

Common services:
- `turn_on`
- `turn_off`
- `toggle`
- `trigger`

## Control Flow

1. Resolve exact automation entity IDs (use `ha-entities`).
2. Preview service name + target payload.
3. Ask explicit confirmation.
4. Execute service call.
5. Verify post-state when relevant:
   - `turn_on` expected state `on`
   - `turn_off` expected state `off`

## Payload Patterns

- Single target:
  - `{"entity_id":"automation.my_rule"}`
- Batch target:
  - `{"entity_id":["automation.a","automation.b"]}`
- Trigger with skipped conditions when intentionally requested:
  - `{"entity_id":"automation.my_rule","skip_condition":true}`

## Safety Rules

- Do not trigger unknown automations.
- For batch operations, execute sequentially and report per-item result.
- Stop batch on hard errors unless user explicitly asks to continue-on-error.
