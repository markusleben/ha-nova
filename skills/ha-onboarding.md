---
name: ha-onboarding
description: Ultra-lean first-run onboarding for HA Nova with App-first flow and quick success checks.
---

# HA Nova Onboarding

## Goal

Get users to the first successful result in minutes with minimal setup burden.
Block onboarding completion until LLAT is configured and validated.
On macOS, prefer Keychain-backed setup over `.env.local` editing.

## Step 1: App-First Prerequisite

- Ensure NOVA Relay App is installed and running.
- Ensure relay auth is configured (`relay_auth_token` in App options).
- Use relay auth for first checks and first user wins.

## Step 2: Connectivity Checks

Run in order:

1. `GET {RELAY_BASE_URL}/health` with `Authorization: Bearer {RELAY_AUTH_TOKEN}`
2. `POST {RELAY_BASE_URL}/ws` with payload `{"type":"ping"}` and relay auth header

Success criteria:
- Relay health responds with `status: ok`.
- Relay WS endpoint responds (`200`) with no degraded upstream state.

## Step 3: Three Quick Wins

1. "List all lights."
2. "Show all temperature sensors."
3. "Turn on one selected light."

## Minimal Troubleshooting

- 401 Unauthorized on Relay:
  - Verify `relay_auth_token` in App options and client config.
- Relay unreachable:
  - Ensure NOVA Relay app is running.
- Full-scope WS features unavailable:
  - Stop and configure `HA_LLAT` now.
  - Continue only after token validation succeeds and Relay reports `ha_ws_connected=true`.
