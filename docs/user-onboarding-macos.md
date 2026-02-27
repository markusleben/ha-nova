# User Onboarding (macOS, Keychain-First)

Date: 2026-02-26

## Goal

Onboard a user with minimal setup friction:
- no required `.env.local` editing
- no SSH key setup
- secrets stored in macOS Keychain
- clear setup and diagnostics commands

## Prerequisites

- macOS
- this repository checked out locally

## Codex Guided Setup (Single Prompt)

In Codex, use:

```text
Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.codex/INSTALL.md
```

This mirrors the popular skill-install UX pattern: one instruction link, then guided execution.

## Claude Code Guided Setup (Single Prompt)

In Claude Code, use:

```text
Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.claude/INSTALL.md
```

## 1. Install skill once (choose one)

```bash
npm run install:codex-skill
```

Or install skills for Codex + Claude Code + OpenCode:

```bash
npm run install:skills
```

## 2. Run onboarding setup

```bash
npm run onboarding:macos
```

## 3. Verify connectivity

```bash
bash scripts/onboarding/macos-onboarding.sh doctor
```

Fast daily readiness check:

```bash
npm run onboarding:macos:quick
```

The setup stores:
- Keychain secrets:
  - `ha-nova.relay-auth-token`
- Non-secret config file:
  - `~/.config/ha-nova/onboarding.env`
  - includes `HA_HOST`, `HA_URL`, `RELAY_BASE_URL`

Validation built into setup:
- verifies the Home Assistant host against common HA endpoints (`:8123`)
- supports URL + host + custom port input (for example `https://ha.example.com` or `192.168.1.5:18123`)
- validates Relay URL format
- probes Relay `GET /health` with your relay auth token
- when setup is re-run and token input is empty, it reuses existing Keychain token (no forced token rotation)
- if Relay is not reachable, setup prints a clear action:
  - install/start NOVA Relay App in Home Assistant
  - verify `relay_auth_token` in App options
- setup error hints classify common relay failures:
  - `401/403`: token mismatch
  - `404`: wrong relay URL or App not running
  - `000`: host/port/network unreachable
- if HA validation fails (for example temporary network issue), setup offers to continue with unverified host

## 4. Optional: Export env for current shell

```bash
eval "$(bash scripts/onboarding/macos-onboarding.sh env)"
```

## Notes

- Local App installation/bootstrap via SSH is contributor-only and handled by `scripts/dev/ha-app-bootstrap.sh`.
- `.env.local` stays a contributor convenience path.
- Keychain-first is the primary end-user path for macOS.
- `HA_LLAT` is required in App option `ha_llat`.
- `doctor` hard-fails if Relay reports `ha_ws_connected=false`; this indicates App LLAT/config issues.
- After one-time skill install, use normal daily startup (`codex`, Claude Code, OpenCode). No special launcher required.
