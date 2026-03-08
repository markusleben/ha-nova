# HA NOVA Update Guide

## Quick Update

All clients (Claude Code, Codex, OpenCode, Gemini) — one command:

```bash
~/.config/ha-nova/update
```

The script auto-detects which clients are installed and updates each using the appropriate method. No arguments needed.

**Older installations** (before v0.1.7) may not have this script yet. In that case:
1. `cd <ha-nova-repo> && git pull` to get the latest code
2. Re-run `ha-nova setup` (or `bash scripts/onboarding/install-local-skills.sh <client>`) to deploy the update script

## Two Independent Version Lines

| Track | File | Scope | Bumped when |
|-------|------|-------|-------------|
| **Skill** | `version.json:skill_version` | Skills, plugin, package | Skill logic changes |
| **Relay** | `config.yaml:version` | HA App (Supervisor) | Relay runtime changes |

`version.json:min_relay_version` bridges the two: skills declare the minimum Relay version they need. The SessionStart hook warns if the running Relay is too old.

**Why separate?** Skill-only improvements (new checks, better prompts) should not force a Relay reinstall/rebuild on the user's HA instance.

## Relay (Home Assistant App)

HA Settings > Apps > NOVA Relay > Update (or reinstall from App Store).

## How the Update Script Works

The script uses three update archetypes depending on the client:

| Client | Archetype | What happens |
|--------|-----------|--------------|
| Claude Code | Native | `claude plugin update ha-nova@ha-nova` |
| Codex | Symlink | `git pull --ff-only` in source clone (symlink auto-resolves) |
| OpenCode | Symlink | `git pull --ff-only` in source clone (symlink auto-resolves) |
| Gemini | Flat-copy | `git pull --ff-only` + re-copy skill files |

After client updates, shared tools (relay CLI, version-check, update script itself) are refreshed from the latest source.

## Check Versions

- **Skills:** `cat ~/.config/ha-nova/version.json` → `skill_version` field
- **Relay:** `~/.config/ha-nova/relay health` → `"version"` field (matches `config.yaml`)
- **Compatibility:** `version.json:min_relay_version` must be <= running Relay version

## Automatic Checks

Two checks run automatically:

1. **Skill update check** — All clients: `relay health` runs `~/.config/ha-nova/version-check` (cached 24h). Claude Code also checks via SessionStart hook. Shows: `UPDATE AVAILABLE: v0.1.2 -> v0.2.0`
2. **Relay compat check** — `relay health` compares Relay version against `min_relay_version`. Claude Code SessionStart hook also checks independently. Shows: `WARNING: Relay version X is below minimum Y`

The `doctor` command (`npx ha-nova doctor` from the repo) runs both checks synchronously and also refreshes the update cache.

## Agent-Driven Updates

When the agent detects `UPDATE AVAILABLE` in its session context, it can run the update script directly:

```bash
~/.config/ha-nova/update
```

After a successful update, the user must start a new session for the updated skills to take effect.
