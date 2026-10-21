# HA NOVA Update Guide

## Quick Update

Update the active HA NOVA install and any supported client integrations on this machine:

```text
ha-nova update
```

The CLI auto-detects which client integrations are installed and refreshes each using the appropriate method. After `ha-nova update` succeeds, start a new AI client session to load the updated HA NOVA skills.

Update routing is simple:
- bundle and dev installs use the HA NOVA updater directly

On native Windows, the supported HA NOVA install path remains `install.ps1`, and `ha-nova update` keeps using the installed HA NOVA runtime directly.
Hermes on Windows is the exception: use the Linux/WSL HA NOVA install and run Hermes update/setup commands from inside that WSL shell.

`ha-nova check-update` compares the installed version against the latest GitHub release and keeps the shared update cache fresh for follow-up commands.

On Windows, the HA NOVA core path is verified; individual client coverage still depends on the client runtime you actually have available on that machine.

**Windows, coming from 0.24.x or 0.25.x:** update once with the Windows install command from the [latest release](https://github.com/markusleben/ha-nova/releases/latest). On those versions, `ha-nova update` asks for admin rights or fails. After that, no admin prompt.

**Older installations** may still use a migration shim. If `ha-nova update` is missing or fails before launch:
1. Re-run the installer for your platform
2. Re-run `ha-nova setup`

The first validated Relay operation from a newly updated binary repairs
v0.20+ copied Codex, OpenCode, current Antigravity, and Hermes skill layouts that predate
the shared first-use check. A health probe performs only the local repair; the
first successful proxy task carries the one-time notices afterwards so a
stderr-suppressing hook cannot consume them. A legacy Claude plugin cannot use that file-layout
repair before its old SessionStart hook runs; if Claude does not surface the
check after updating, run `ha-nova setup` once and start a fresh Claude session.
The one already-loaded transition task can carry only its selected server
profile; start the required fresh client session before switching profiles so
each profile receives its own Relay update check.
Retired pre-v0.20 Gemini/Antigravity paths require one `ha-nova setup` run.

## Two Independent Version Lines

| Track | File | Scope | Bumped when |
|-------|------|-------|-------------|
| **Skill** | `version.json:skill_version` | Skills, plugin, package | Skill logic changes |
| **Relay** | `config.yaml:version` | HA App (Supervisor) | Relay runtime changes |

`version.json:min_relay_version` bridges the two: skills declare the minimum Relay version they need. The first-use `ha-nova check-update --quiet`, relay commands, and the typed verbs' `notices[]` warn if the running Relay is too old.

**Why separate?** Skill-only improvements (new checks, better prompts) should not force a Relay update on the user's HA instance.

## Relay (Home Assistant App)

HA Settings > Apps > NOVA Relay > Update (or reinstall from App Store). On Home Assistant older than 2026.2, Apps are still called Add-ons (Settings > Add-ons).

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

- **Skills:** `ha-nova version` (reads `version.json` from the install root)
- **Relay:** `ha-nova relay health` → `"version"` field (matches `config.yaml`)
- **Compatibility:** `version.json:min_relay_version` must be <= running Relay version

## Automatic Checks

Three checks run automatically:

1. **Skill update check** — `ha-nova check-update` compares the installed version against the latest GitHub release. The result is cached briefly, then revalidated against GitHub with a conditional request, so a newly published release is detected promptly instead of being hidden until a long cache expires. Every independently loadable HA NOVA skill carries the contract, so the first skill used runs the quiet check once per selected server profile before that profile's first HA task in a session. The quiet check also reports a registry-proven pending NOVA Relay App update, including compatible versions above the skills' minimum Relay floor.
2. **Relay compat check** — `ha-nova relay health` compares Relay version against `min_relay_version`.
3. **Relay-traffic update nudge** — every `ha-nova relay ws|core` call surfaces a cached "update available" notice on stderr at most once per 24h; `ha-nova relay health` surfaces it unthrottled as the explicit diagnostic path. The compare is cache-only (never a network call in the hot path); a non-fresh cache is refreshed by a detached background `check-update --quiet --json`. Opt out with `HA_NOVA_NO_UPDATE_NUDGE=1`. An RC newer than the latest stable gets no notice (plain `ha-nova check-update` prints one return-to-stable line); its final release is a normal update notice.

`check-update` and the nudge say the same plain notice: "HA NOVA 0.27.0 is ready (you have 0.26.0)", one highlight, the update step, and the release link; `check-update` also says when the NOVA App needs an update too. The quiet first-use check waits at most 3 s for GitHub, then uses the cached release; offline with no cache it stays silent. The Claude Code session hook adds no update notice; it only keeps the release cache warm.

The `doctor` command runs the first two checks synchronously and also refreshes the update cache.
Other clients use the same shared first-use skill contract and CLI updater path (`ha-nova check-update`, `ha-nova doctor`, `ha-nova update`). The relay-traffic nudge remains a cache-only fallback if a client misses the skill instruction.
Installed Claude uses the tested HA NOVA release payload on disk; update discovery stays automatic where HA NOVA already provides it.

## Agent-Driven Updates

After a Home Assistant task, the agent offers an available update once per session ("Say 'update HA NOVA' and I'll do it"). Only after the user says yes does it run:

```text
ha-nova update
```

`ha-nova update` refreshes HA NOVA in every installed AI app, checks that Claude Code uses the new version, and ends with one line naming the apps it updated (and any it could not, with the next step). If the NOVA App is then older than this version needs and Home Assistant offers its update, a desktop terminal opens the setup page on "Update the NOVA App" (nothing installs before the owner clicks and signs in; Home Assistant backs up the App first). Another terminal gets the guided App update offer; without a terminal (an AI app ran the command) it prints where to update the App in Home Assistant.

After `ha-nova update` succeeds, start a new AI client session to load the updated HA NOVA skills.

A registry-proven pending NOVA Relay App update routes the agent to
`ha-nova:updates`. Agreement to prepare the update opens that skill's preview;
only confirmation of the preview permits installation with a partial App
backup. Interactive `ha-nova update` and `ha-nova doctor` apply the same
boundary directly: resolve registry-proven Home Assistant App evidence, require
install plus backup support, show versions/backup/restart impact, confirm, then
immediately re-read state and immutable registry provenance before writing.
Because Supervisor cannot bind an App install to a version, the request omits
`version` and the preview says Home Assistant installs the latest version
available at execution time.
Standalone Container/Core relays stay on the manual image-pull path.
