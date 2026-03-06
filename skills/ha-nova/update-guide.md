# HA NOVA Update Guide

## Relay (Home Assistant App)

HA Settings > Apps > NOVA Relay > Update (or reinstall from App Store).

## Skills (per client)

| Client | Command |
|--------|---------|
| Claude Code | `claude plugin update ha-nova` |
| Codex / OpenCode | `cd <ha-nova-repo> && git pull` |
| Gemini CLI | `cd <ha-nova-repo> && git pull && npm run install:gemini-skill` |

## Check Versions

- **Skills:** `cat version.json` (in repo root)
- **Relay:** `~/.config/ha-nova/relay health` (look for `"version"`)
- **Compatibility:** `version.json:min_relay_version` must be <= Relay version

## Automatic Checks

Two checks run automatically:

1. **Skill update check** — All clients: context skill runs `~/.config/ha-nova/version-check` (cached 24h). Claude Code also checks via SessionStart hook. Shows: `UPDATE AVAILABLE: v0.1.2 -> v0.2.0`
2. **Relay compat check** — SessionStart hook (Claude Code only) compares Relay version against `min_relay_version`. Shows: `WARNING: Relay version X is below minimum Y`

The `doctor` command (`npm run onboarding:macos:doctor` from the repo) runs both checks synchronously and also refreshes the update cache.
