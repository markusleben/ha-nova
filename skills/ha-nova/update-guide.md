# HA NOVA Update Guide

## Quick Update

Update the active HA NOVA install and any supported client integrations on this machine:

```text
ha-nova update
```

The CLI auto-detects which client integrations are installed and refreshes each using the appropriate method.

Update routing is simple:
- bundle and dev installs use the HA NOVA updater directly

On native Windows, the supported HA NOVA install path remains `install.ps1`, and `ha-nova update` keeps using the installed HA NOVA runtime directly.
Hermes on Windows is the exception: use the Linux/WSL HA NOVA install and run Hermes update/setup commands from inside that WSL shell.

`ha-nova check-update` compares the installed version against the latest GitHub release and keeps the shared update cache fresh for follow-up commands.

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
| Claude Code | Native | Re-stage the local marketplace from the installed release payload, then verify/reinstall the plugin |
| Codex | Linked/Copy | Refresh installed skill tree from the active HA NOVA install |
| OpenCode | Linked/Copy | Refresh installed skill tree from the active HA NOVA install |
| Google Antigravity | Flat-copy | Rebuild namespaced flat markdown copies from the active HA NOVA install |
| Hermes Agent | Namespaced copy | Rebuild the Hermes-local namespaced skill bundle from the active HA NOVA install |

After client updates, shared tools are refreshed from the active HA NOVA install.
On native Windows, post-update client sync uses the installed HA NOVA runtime directly. Hermes-on-Windows should be updated from the WSL2 install instead.

## Check Versions

- **Skills:** `ha-nova relay jq --file ~/.config/ha-nova/version.json .skill_version`
- **Relay:** `ha-nova relay health` → `"version"` field (matches `config.yaml`)
- **Compatibility:** `version.json:min_relay_version` must be <= running Relay version

## Automatic Checks

Two checks run automatically:

1. **Skill update check** — `ha-nova check-update` compares the installed version against the latest GitHub release. The result is cached briefly, then revalidated against GitHub with a conditional request, so a newly published release is detected promptly instead of being hidden until a long cache expires. Claude Code SessionStart reads the same shared release cache and can trigger a background CLI refresh when the cache is stale. Other clients should run the quiet check once on the first HA NOVA skill use in a session.
2. **Relay compat check** — `ha-nova relay health` compares Relay version against `min_relay_version`. Claude Code SessionStart context can surface the same warning independently.

The `doctor` command runs both checks synchronously and also refreshes the update cache.
Other clients use the same shared CLI updater path (`ha-nova check-update`, `ha-nova doctor`, `ha-nova update`), but do not currently inject the same automatic SessionStart banner.
Installed Claude uses the tested HA NOVA release payload on disk; update discovery stays automatic where HA NOVA already provides it.

## Agent-Driven Updates

When the agent detects `UPDATE AVAILABLE` in its session context, it can run the update command directly:

```text
ha-nova update
```

After a successful update, the user must start a new client session for the updated payload to take effect.
