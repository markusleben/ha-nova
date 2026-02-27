---
name: ha-onboarding
description: Ultra-lean first-run onboarding for HA Nova with App-first flow and quick success checks.
---

# HA Nova Onboarding

## Goal

Get users to the first successful result in minutes with minimal setup burden.
Block onboarding completion until Relay upstream WS is healthy.
On macOS, prefer Keychain-backed setup over `.env.local` editing.

## Step 1: App-First Prerequisite

- Ensure NOVA Relay App is installed and running.
- Ensure relay auth is configured (`relay_auth_token` in App options).
- Use relay auth for first checks and first user wins.

## Step 2: Connectivity Checks

Run in order:

1. `GET {RELAY_BASE_URL}/health` with `Authorization: Bearer {RELAY_AUTH_TOKEN}`
2. If health reports degraded WS (`ha_ws_connected=false`), run diagnostic ping:
   - `POST {RELAY_BASE_URL}/ws` with payload `{"type":"ping"}` and relay auth header

Success criteria:
- Relay health responds with `status: ok`.
- Relay health reports `ha_ws_connected=true`.

## Step 3: Three Quick Wins

1. "List all lights."
2. "Show all temperature sensors."
3. "List all automations."

## Minimal Troubleshooting

- 401 Unauthorized on Relay:
  - Verify `relay_auth_token` in App options and client config.
- Relay unreachable:
  - Ensure NOVA Relay App is running.
- Full-scope WS features unavailable:
  - Stop and configure App option `ha_llat` now.
  - Continue only after App restart and Relay reports `ha_ws_connected=true`.
