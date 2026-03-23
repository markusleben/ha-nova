# HA NOVA Update Guide

## Quick Update

Update the active HA NOVA install and any supported client integrations on this machine:

```text
ha-nova update
```

The CLI auto-detects which client integrations are installed and refreshes each using the appropriate method.

Update routing is source-aware:
- bundle and dev installs use the HA NOVA updater directly
- `winget`-managed installs delegate to `winget upgrade`

For `winget`-managed installs, `ha-nova update --version <x>` is intentionally unsupported. Use plain `ha-nova update` and let the package manager choose the published version.

`ha-nova check-update` follows the active install source as well. For `winget` installs it checks the package-manager channel, not just raw GitHub releases. On Windows, if both bundle and `winget` installs are detected, HA NOVA warns instead of guessing which channel you meant.

On Windows, `install.ps1` also refuses to install on top of an existing `winget` install. Keep one Windows install channel per machine.

On Windows, the HA NOVA core path is verified; individual client coverage still depends on the client runtime you actually have available on that machine.

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
| Claude Code | Native | Refresh local marketplace metadata + verify/update the installed plugin |
| Codex | Linked/Copy | Refresh installed skill tree from the active HA NOVA install |
| OpenCode | Linked/Copy | Refresh installed skill tree from the active HA NOVA install |
| Gemini | Flat-copy | Rebuild flat markdown copies from the active HA NOVA install |

After client updates, shared tools are refreshed from the active HA NOVA install.
For `winget`-managed Windows installs, the post-update client sync resolves the live winget runtime first instead of trusting whichever `ha-nova` happens to be first on `PATH`.

## Check Versions

- **Skills:** `ha-nova relay jq --file ~/.config/ha-nova/version.json .skill_version`
- **Relay:** `ha-nova relay health` → `"version"` field (matches `config.yaml`)
- **Compatibility:** `version.json:min_relay_version` must be <= running Relay version

## Automatic Checks

Two checks run automatically:

1. **Skill update check** — `ha-nova check-update` compares the installed version against the latest GitHub release (cached 24h). Claude Code SessionStart reads the same shared release cache and can trigger a background CLI refresh when the cache is stale. Other clients should run the quiet check once on the first HA NOVA skill use in a session.
2. **Relay compat check** — `ha-nova relay health` compares Relay version against `min_relay_version`. Claude Code SessionStart context can surface the same warning independently.

The `doctor` command runs both checks synchronously and also refreshes the update cache.
Other clients use the same shared CLI updater path (`ha-nova check-update`, `ha-nova doctor`, `ha-nova update`), but do not currently inject the same automatic SessionStart banner.

## Agent-Driven Updates

When the agent detects `UPDATE AVAILABLE` in its session context, it can run the update command directly:

```text
ha-nova update
```

After a successful update, the user must start a new session for the updated skills to take effect.
