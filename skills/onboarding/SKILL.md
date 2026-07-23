---
name: onboarding
description: Use when HA NOVA Relay requests fail due to onboarding, connectivity, or auth issues.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Onboarding


## Scope

Use when HA NOVA requests fail due to onboarding, connectivity, or auth issues.
Diagnostics only. Do not use this skill for config writes or routine HA operations after readiness is restored.

## Bootstrap

If the `ha-nova` command itself is missing, the CLI is not installed — install it first from the latest GitHub release (https://github.com/markusleben/ha-nova/releases/latest carries the copy-paste install command per OS), then run `ha-nova setup`. Never bootstrap from the repo's `main` branch: released skills must install the reviewed, tagged release.

When the CLI exists, read and follow
`../ha-nova/session-bootstrap.md` before the first HA task.
Relay CLI command: `ha-nova relay`
If missing: `ha-nova setup`

## Flow

1. Run diagnostics only after a real failure.
2. Classify failure quickly:
   - `401/403`: relay auth token mismatch
   - `404`: endpoint/path mismatch
   - connect error / status `000`: relay unreachable
   - `ha_ws_connected=false`: the reason can be passive or stale because health/REST reads do not open WS. Send WS `{"type":"ping"}`, then re-read health before diagnosing any `auth`, `network`, or `never_connected` value. Ping `ok: true` plus post-ping `ha_ws_connected: true` proves readiness. Otherwise classify the ping error and post-health reason: `auth` means upstream authentication was rejected (App: update/restart Relay; standalone: replace `HA_LLAT`); `network` means HA remains unreachable/restarting; `never_connected` alone never proves a bad `HA_URL` or network path. Older relays omit the reason — use the ping result or `ha-nova doctor`
   - `/ws` on an older Relay proves `LLAT is required`: update the App; only standalone Container/Core should repair `HA_LLAT`
   - `desktop keyring locked` / `secure storage is present but locked` during setup or pairing: the machine has a Secret Service that never gets unlocked (headless VM, autologin box, systemd user service). Do not ask the user to unlock it on every boot — use the explicit file backend instead: `ha-nova setup --service <client>`, or `ha-nova pair --credential-store=file`
3. Return one concrete remediation step.

## Standard Remediation Commands

- onboarding setup:
  - `ha-nova setup`
- health quick check:
  - `ha-nova relay health`
- doctor detail:
  - `ha-nova doctor`
- pairing on a machine whose desktop keyring is never unlocked (headless VM, agent box):
  - `ha-nova setup --service <client>` — or standalone: `ha-nova pair --credential-store=file`

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Return a short diagnostic report:
1. **Status** — one-line result (e.g., "Relay reachable", "Auth token mismatch")
2. **Details** — error code or relevant diagnostic output (only on failure)
3. **Next step** — one concrete remediation command (only on failure)

On success, keep the response to the status line only. Do not dump raw relay output.

## Safety

- Read-only skill: never issue mutating relay or service calls.
- For write intent, hand off to the owning skill; unfamiliar writes go through `ha-nova:fallback` first.

- Diagnostics only — this skill never modifies Home Assistant state, config, or relay settings.
- Never ask the user to paste tokens or secrets in chat.
- All communication with Home Assistant goes through `ha-nova relay` exclusively.

## Guardrails

- no proactive doctor/ready in normal success path
- keep response short and action-oriented
- never ask user to paste secrets in chat
