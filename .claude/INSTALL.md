# Installing HA NOVA for Claude Code (macOS)

## Quick Install

Tell Claude Code:

```text
Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.claude/INSTALL.md
```

## Prerequisites

- macOS
- Git
- Node.js 20+
- `bash`, `curl`, `security`

## Manual Install

### 1. Prepare local repository

```bash
if [ -d "$HOME/ha-nova/.git" ]; then
  cd "$HOME/ha-nova" && git pull
else
  git clone https://github.com/markusleben/ha-nova.git "$HOME/ha-nova"
  cd "$HOME/ha-nova"
fi
npm install
```

### 2. Install Claude Code local skill

```bash
npm run install:claude-skill
```

Optional multi-client install (Codex + Claude Code + OpenCode):

```bash
npm run install:skills
```

### 3. Run onboarding setup

```bash
npm run onboarding:macos
```

## Verify

Run:

```bash
bash scripts/onboarding/macos-onboarding.sh doctor
```

Expected minimum:
- `[ok] Home Assistant reachable`
- `[ok] Relay health reachable`

## Troubleshooting

- Relay health `401/403`:
  - token mismatch; verify `relay_auth_token` in NOVA Relay App options.
- Relay health `404`:
  - wrong relay URL or App not started.
- Relay health `000`:
  - host/port/network unreachable.
- `/ws` degraded (`ha_ws_connected=false` or `UPSTREAM_WS_ERROR`):
  - App + Relay is running, but full upstream WS scope is limited without LLAT.

## Security

- Do not paste tokens in chat.
- Onboarding stores secrets in macOS Keychain (`ha-nova.relay-auth-token`, optional `ha-nova.ha-llat`).
- End-user onboarding does not require SSH.
- Contributor bootstrap remains separate: `scripts/dev/ha-app-bootstrap.sh`.
- Daily usage stays normal (`claude`); no custom launcher.

## Related Install Docs

- Codex: `https://raw.githubusercontent.com/markusleben/ha-nova/main/.codex/INSTALL.md`
