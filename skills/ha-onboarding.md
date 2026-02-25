---
name: ha-onboarding
description: Ultra-lean first-run onboarding for HA Nova with mandatory LLT and quick success checks.
---

# HA Nova Onboarding

## Step 1: Token Prerequisite (Mandatory)

Ask user to create a Home Assistant Long-Lived Token in:
- Profile -> Security -> Long-Lived Access Tokens

Store token as `HA_TOKEN`.

Do not continue without LLT.

## Step 2: Connectivity Checks

Run in order:

1. `GET {HA_URL}/api/` with `Authorization: Bearer {HA_TOKEN}`
2. `GET {BRIDGE_URL}/health` with `Authorization: Bearer {HA_TOKEN}`

Success criteria:
- HA API responds with running status.
- Bridge health responds with `status: ok`.

## Step 3: Three Quick Wins

1. "List all lights."
2. "Show all temperature sensors."
3. "Turn on one selected light."

## Minimal Troubleshooting

- HA URL unreachable:
  - Verify `HA_URL` and network path.
- 401 Unauthorized:
  - Regenerate LLT and retry.
- Bridge unreachable:
  - Ensure Nova Bridge app is running.
