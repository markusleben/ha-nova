# HA NOVA Update Guide

## Quick Update

All clients (Claude Code, Codex, OpenCode, Gemini) — one command:

```bash
ha-nova update
```

The CLI auto-detects which clients are installed and updates each using the appropriate method. No arguments needed.

**Older installations** may still use a migration shim. If `ha-nova update` is missing or fails before launch:
1. Re-run the installer for your platform
2. Re-run `ha-nova setup`

## Two Independent Version Lines

| Track | File | Scope | Bumped when |
|-------|------|-------|-------------|
| **Skill** | `version.json:skill_version` | Skills, plugin, package | Skill logic changes |
| **Relay** | `config.yaml:version` | HA App (Supervisor) | Relay runtime changes |

`version.json:min_relay_version` bridges the two: skills declare the minimum Relay version they need. The SessionStart hook warns if the running Relay is too old.

**Why separate?** Skill-only improvements (new checks, better prompts) should not force a Relay reinstall/rebuild on the user's HA instance.

## Relay (Home Assistant App)

HA Settings > Apps > NOVA Relay > Update (or reinstall from App Store).

## How Update Works

HA NOVA uses three update archetypes depending on the client:

| Client | Archetype | What happens |
|--------|-----------|--------------|
| Claude Code | Native | Re-register local marketplace + refresh plugin |
| Codex | Linked/Copy | Refresh installed skill tree from the active HA NOVA install |
| OpenCode | Linked/Copy | Refresh installed skill tree from the active HA NOVA install |
| Gemini | Flat-copy | Rebuild flat markdown copies from the active HA NOVA install |

After client updates, shared tools are refreshed from the active HA NOVA install.

## Check Versions

- **Skills:** `cat ~/.config/ha-nova/version.json` → `skill_version` field
- **Relay:** `ha-nova relay health` → `"version"` field (matches `config.yaml`)
- **Compatibility:** `version.json:min_relay_version` must be <= running Relay version

## Automatic Checks

Two checks run automatically:

1. **Skill update check** — `ha-nova check-update` compares the installed version against the latest release (cached 24h). Claude Code can surface the same update notice via SessionStart context.
2. **Relay compat check** — `ha-nova relay health` compares Relay version against `min_relay_version`. Claude Code SessionStart context can surface the same warning independently.

The `doctor` command runs both checks synchronously and also refreshes the update cache.

## Agent-Driven Updates

When the agent detects `UPDATE AVAILABLE` in its session context, it can run the update script directly:

```bash
ha-nova update
```

After a successful update, the user must start a new session for the updated skills to take effect.
