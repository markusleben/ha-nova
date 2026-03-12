---
name: ha-nova-onboarding
description: Use when HA NOVA Relay requests fail due to onboarding, connectivity, or auth issues.
---

# HA NOVA Onboarding


## Scope

Use when HA NOVA requests fail due to onboarding, connectivity, or auth issues.

## Bootstrap

Relay CLI path: `~/.config/ha-nova/relay`
If missing: `npm run onboarding:macos`

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
  - `npm run onboarding:macos`
- health quick check:
  - `~/.config/ha-nova/relay health`
- doctor detail:
  - `npm run onboarding:macos:doctor`

## Guardrails

- no proactive doctor/ready in normal success path
- keep response short and action-oriented
- never ask user to paste secrets in chat
