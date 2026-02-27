# Installing HA NOVA for Codex (macOS)

## Quick Install

Tell Codex:

```text
Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.codex/INSTALL.md
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

### 2. Install Codex local skill

```bash
npm run install:codex-skill
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
- `[ok] Onboarding preflight passed.`

## Troubleshooting

- Relay health `401/403`:
  - token mismatch; verify `relay_auth_token` in NOVA Relay App options.
- Relay health `404`:
  - wrong relay URL or App not started.
- Relay health `000`:
  - host/port/network unreachable.
- `/ws` degraded (`ha_ws_connected=false` or `UPSTREAM_WS_ERROR`):
  - App + Relay is running, but upstream WS is not connected.
  - verify App option `ha_llat` and restart the App.

## Security

- Do not paste tokens in chat.
- Onboarding stores only relay auth in macOS Keychain (`ha-nova.relay-auth-token`).
- LLAT stays in App option `ha_llat` only.
- End-user onboarding does not require SSH.
- Contributor bootstrap remains separate: `scripts/dev/ha-app-bootstrap.sh`.
- Daily usage stays normal (`codex`); no custom launcher.

## Related Install Docs

- Claude Code: `https://raw.githubusercontent.com/markusleben/ha-nova/main/.claude/INSTALL.md`
