# HA NOVA Update Guide

## Two Independent Version Lines

| Track | File | Scope | Bumped when |
|-------|------|-------|-------------|
| **Skill** | `version.json:skill_version` | Skills, plugin, package | Skill logic changes |
| **Relay** | `config.yaml:version` | HA App (Supervisor) | Relay runtime changes |

`version.json:min_relay_version` bridges the two: skills declare the minimum Relay version they need. The SessionStart hook warns if the running Relay is too old.

**Why separate?** Skill-only improvements (new checks, better prompts) should not force a Relay reinstall/rebuild on the user's HA instance.

## Relay (Home Assistant App)

HA Settings > Apps > NOVA Relay > Update (or reinstall from App Store).

## Skills (per client)

| Client | Command |
|--------|---------|
| Claude Code | `claude plugin update ha-nova` |
| Codex / OpenCode | `cd <ha-nova-repo> && git pull` |
| Gemini CLI | `cd <ha-nova-repo> && git pull && npm run install:gemini-skill` |

## Check Versions

- **Skills:** `cat version.json` → `skill_version` field
- **Relay:** `~/.config/ha-nova/relay health` → `"version"` field (matches `config.yaml`)
- **Compatibility:** `version.json:min_relay_version` must be <= running Relay version

## Automatic Checks

Two checks run automatically:

1. **Skill update check** — All clients: context skill runs `~/.config/ha-nova/version-check` (cached 24h). Claude Code also checks via SessionStart hook. Shows: `UPDATE AVAILABLE: v0.1.2 -> v0.2.0`
2. **Relay compat check** — SessionStart hook (Claude Code only) compares Relay version against `min_relay_version`. Shows: `WARNING: Relay version X is below minimum Y`

The `doctor` command (`npm run onboarding:macos:doctor` from the repo) runs both checks synchronously and also refreshes the update cache.
