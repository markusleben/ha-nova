---
name: updates
description: Use when checking pending Home Assistant updates (core, OS, add-ons, integrations, device firmware), reading release notes, installing updates with safety gates, or skipping/unskipping update versions through HA NOVA Relay.
---

# HA NOVA Updates

## Scope

Update lifecycle:
- grouped overview of pending updates
- release notes/links for one update
- install an update (feature-gated, safety gates)
- skip/unskip a version

Not in scope:
- auto-update settings: HA UI
- HACS store operations beyond its `update.*` entities (`ha-nova:fallback`)
- rollbacks — updates are generally not downgradable; recovery is a Home Assistant Backup

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

File-based requests: `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>` and `ha-nova relay ws --data-file <payload-file>`; `--out <result-file>` for large responses.
REST body: `.data.body`; WS body: `.data` (`skills/ha-nova/relay-api.md` → Standard Envelope).

## Feature Gate (critical)

Read `supported_features` before acting:

| Bit | Feature |
|---|---|
| 1 | install |
| 2 | install a specific version |
| 4 | progress reporting |
| 8 | backup before update (add-on/core mechanism) |
| 16 | release notes |

Never call a service the mask does not allow — say which capability is missing instead.

## Flow

### Overview
`GET /api/states --out <result-file>` for the update entities AND WS `{"type":"config/entity_registry/list"}` for their `platform`/`unique_id` (states alone cannot classify). Filter `update.*`, report grouped, pending (`state: "on"`) first:
- Home Assistant stack: core, operating system, supervisor
- Add-ons; integrations (HACS components); device firmware (everything else)
Group deterministically: `platform: hacs` = integrations; `platform: hassio` splits by `unique_id` — `home_assistant_{core,os,supervisor}_version_latest` = HA stack, `<addon_slug>_version_latest` = add-on; everything else = device firmware/other. Never guess from names.
Per pending item: name, `installed_version` → `latest_version`, `auto_update`, `skipped_version`. Fleets: counts per group + pending list only — never dump 200 firmware rows.

### Release notes
When bit 16 is set: WS `{"type":"update/release_notes","entity_id":"update.<id>"}` → markdown in `.data`; summarize, do not dump. Without bit 16, do not call it (it fails with `not_supported`) — use the entity's `release_summary`/`release_url` instead; when both are empty, say no notes are available.

### Install
1. Feature Gate: bit 1 required. Preview exactly one update: name, `installed_version` → `latest_version`, release-notes summary or link.
2. Safety gates by kind:
   - **core / operating system**: far-reaching — offer a full safety backup first via `ha-nova:backup` (see `skills/ha-nova/write-safety.md` → Safety-Mechanism Availability) and say that HA restarts during the update. Surface breaking-changes sections first in the notes summary; on skipped-version jumps the notes cover only the target — link the intermediate releases via `release_url`.
   - **add-ons**: when bit 8 is set, include `"backup": true` (add-on partial backup). Updating the NOVA Relay add-on restarts the relay itself — the install call may drop mid-flight; verify afterwards via `ha-nova relay health`.
   - **supervisor**: usually updates itself (`auto_update`); an explicit install restarts the Supervisor — expect a brief entity dropout during the poll, no HA restart.
   - **integrations (HACS components)**: the new version loads only after a Home Assistant restart — after a verified install, report it as installed but not yet active, and offer the restart as a separate confirmed step via `ha-nova:service-call`.
   - **device firmware**: warn that firmware updates can take long, must not be interrupted, and rarely have release notes — confirm the exact device.
3. Natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation). One update per confirmation — for "update everything", list the plan and confirm the batch explicitly, then install sequentially, verifying each before the next. Order add-ons and firmware before core/OS — a core/OS restart pauses everything after it until the relay is reachable again.
4. POST `/api/services/update/install` with `{"entity_id":"update.<id>"}` (+ `"backup": true` for add-on updates; for core/OS pass it ONLY when the confirmed preview explicitly included that built-in backup — the offered `ha-nova:backup` flow is the safety net, never an unconditional flag; + `"version": "<v>"` only with bit 2).
5. The call returns before the update finishes. Poll the entity: `in_progress`/`update_percentage` while running; done when `state` is `off` and `installed_version` equals the target. For core/OS installs, connection and `UPSTREAM_*` errors during the poll ARE the expected restart window, not failure: keep polling every ~30 s (core up to ~15 min; OS longer — the host reboots and the relay itself is unreachable) and never route to `ha-nova setup` mid-update. If still `in_progress` after ~10 minutes (device firmware: 30+ minutes and a temporarily `unavailable` device are normal), say the update continues in the background and how to check later. Report failure when `state` stays `on` without progress — never claim success from the service call alone.

### Skip / unskip
- Skip a pending version: POST `/api/services/update/skip` with `{"entity_id":"update.<id>"}` — preview which version gets skipped; natural confirmation; verify by re-reading the entity (`skipped_version` set, `state: off`). Reversible. HA rejects skip/clear_skipped on `auto_update: true` entities — say so instead of calling.
- Undo: POST `/api/services/update/clear_skipped` — the update shows as pending again.

## Error Handling

- `not_supported` on release notes: expected without bit 16 — fall back to `release_url`.
- Install rejected: report HA's error; do not retry blind — a running update (`in_progress`) blocks a second install.
- Entity vanished mid-poll (relay/add-on restart): re-read once before reporting failure.
- Full relay error taxonomy: `skills/ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `skills/ha-nova/output-rules.md`.

- `Updates` / `Status`
- `Planned change`
- `Options`
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots. Group counts + pending list — never raw JSON.

## Safety

- Installs use natural confirmation per update after a preview; batch installs need an explicitly confirmed plan.
- Updates are effectively irreversible — for core/OS name the safety-backup offer before, not after.
- Never install anything the user did not ask about or confirm; `auto_update: true` items update themselves — say so instead of installing them unprompted; an explicit, confirmed user request may still install.
- Verify by re-reading the entity, not by service success alone.
