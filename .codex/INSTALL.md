# Installing HA NOVA for Codex (macOS)

Use this when you want Codex to guide onboarding end-to-end.

## Prerequisites

- macOS
- Git
- Node.js 20+
- `bash`, `curl`, `security` available

## First-Time Setup (No Local Repo Yet)

Run:

```bash
if [ -d "$HOME/ha-nova/.git" ]; then
  cd "$HOME/ha-nova" && git pull
else
  git clone https://github.com/markusleben/ha-nova.git "$HOME/ha-nova"
  cd "$HOME/ha-nova"
fi
npm install
```

Install the HA NOVA skill globally for Codex:

```bash
npm run install:codex-skill
```

Optional: install local skills for Codex + Claude Code + OpenCode in one shot:

```bash
npm run install:skills
```

## Installation

1. Run onboarding setup:

```bash
npm run onboarding:macos
```

If this is your first run, setup will ask for:
- Home Assistant URL or host
- Relay base URL
- Relay auth token (leave empty to reuse existing Keychain token or auto-generate)
- optional Home Assistant Long-Lived Access Token (LLAT)

2. Run diagnostics:

```bash
bash scripts/onboarding/macos-onboarding.sh doctor
```

Expected:
- `[ok] Home Assistant reachable`
- `[ok] Relay health reachable`

## Troubleshooting

- `401/403` from Relay health:
  - Relay token mismatch.
  - Use the exact `relay_auth_token` configured in NOVA Relay App options.
- `404` from Relay health:
  - wrong Relay URL, or NOVA Relay App not started.
- `000` from Relay health:
  - host/port/network unreachable.

## Notes

- Primary model is App + Relay.
- End-user onboarding does not require SSH.
- Contributor bootstrap remains separate: `scripts/dev/ha-app-bootstrap.sh`.
- `ha_ws_connected=false` is a warning (degraded upstream WS scope), not a hard failure.
- Continue in your current Codex session after setup.
- For future sessions, use normal startup (`codex`). No special launcher is required.
