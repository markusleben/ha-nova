---
name: onboarding
description: Use when HA NOVA Relay requests fail due to onboarding, connectivity, or auth issues.
---

# HA NOVA Onboarding


## Scope

Use when HA NOVA requests fail due to onboarding, connectivity, or auth issues.

## Bootstrap

Relay CLI command: `ha-nova relay`
If missing: `ha-nova setup`

## Flow

1. Run diagnostics only after a real failure.
2. Classify failure quickly:
   - `401/403`: relay auth token mismatch
   - `404`: endpoint/path mismatch
   - connect error / status `000`: relay unreachable
   - `ha_ws_connected=false`: upstream HA websocket not healthy
3. Return one concrete remediation step.

## Standard Remediation Commands

- onboarding setup:
  - `ha-nova setup`
- health quick check:
  - `ha-nova relay health`
- doctor detail:
  - `ha-nova doctor`

## Guardrails

- no proactive doctor/ready in normal success path
- keep response short and action-oriented
- never ask user to paste secrets in chat
