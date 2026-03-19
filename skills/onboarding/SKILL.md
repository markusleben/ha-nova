---
name: onboarding
description: Use when HA NOVA Relay requests fail due to onboarding, connectivity, or auth issues.
---

# HA NOVA Onboarding


## Scope

Use when HA NOVA requests fail due to onboarding, connectivity, or auth issues.
Diagnostics only. Do not use this skill for config writes or routine HA operations after readiness is restored.

## Bootstrap

Relay CLI command: `ha-nova relay`
If missing: `ha-nova setup`

## Flow

1. Run diagnostics only after a real failure.
2. Classify failure quickly:
   - `401/403`: relay auth token mismatch
   - `404`: endpoint/path mismatch
   - connect error / status `000`: relay unreachable
   - `ha_ws_connected=false`: relay is reachable but not proven ready yet; confirm with `/ws` or `ha-nova doctor` before blaming LLAT
   - `/ws` proves `LLAT is required`: Home Assistant access token (`ha_llat`) is missing or wrong
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
