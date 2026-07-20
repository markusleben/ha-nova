---
name: yaml-config
description: Use when creating or editing Home Assistant configuration that only exists as YAML files — template sensors beyond the helper UI, REST and command-line sensors, packages, and themes — through HA NOVA Relay's opt-in file access.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA YAML Config

## Scope

Configuration that has NO Home Assistant API and only exists as a YAML file:
- template sensors/binary sensors beyond what the helper UI can express (triggers, multiple entities per block, availability templates)
- `rest` and `command_line` sensors
- packages
- frontend themes

Not in scope: anything with an API — config-entry template helpers (`ha-nova:helper`), automations and scripts (`ha-nova:write`), dashboards (`ha-nova:dashboard`), scenes (`ha-nova:scene`). If a helper can express it, use the helper: it is safer, reloadable, and editable in the UI.

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

This skill needs **Relay 0.7.0 or newer** AND file access enabled. Both are the user's decision, not yours:
- Probe once: `ha-nova relay files --data-file <payload-file>` with `{"action":"list_dir","path":"/config"}`.
- `FILE_ACCESS_DISABLED` -> file access is OFF (the default). Tell the user how to turn it on (App: Settings > Apps > NOVA Relay > Configuration > `file_access`: `readwrite`, restart; container: `FILE_ACCESS=readwrite` plus a config mount) and what it means, then continue with the manual path below. Do not nag.
- `FILE_ACCESS_READONLY` -> reads work, writes do not. Offer the manual path below instead of asking for more permission.
- If the relay is older than 0.7.0, say so plainly — the endpoint does not exist there.

## Relay Contract

- `ha-nova relay files --data-file <payload-file>` — file transport. Actions: `list_dir`, `read_file`, `write_file` (`content`, `backup` defaults to true), `delete_file`.
- `ha-nova relay core --method POST --path /api/config/core/check_config` — validate the configuration BEFORE reloading.
- `ha-nova relay core --method POST --path /api/services/<domain>/<service> --body-file <payload-file>` — apply the reload from Flow step 5 (`template/reload`, `frontend/reload_themes`, ...).
- `ha-nova relay core --method GET --path /api/states/<entity_id>` — verify the entity actually exists afterwards.

## Flow

Never skip a step.

1. **Read before write.** `read_file` the target (or `list_dir` to find it). Never write a file you have not read: `write_file` replaces the whole file, so an unread file means an unknown loss.
2. **Build the change in memory**. For template/REST/command-line SENSOR definitions, apply the TS checks from `skills/review/checks.md` → Template/REST/Command-line Sensors to the draft first — fix HIGH findings inline, show the rest as plain-language advisories below the preview (never the internal codes). Then preview it as the File-Change Preview (below). Never a `-`/`+` unified diff, never the whole file unless asked — but say plainly that the whole file is replaced on save.
3. **Confirm**, then capture the auto config snapshot (category `yaml`; data = `{path: <exact logical path>, content: <current file content>}` — the slugified name is lossy, the stored `path` is what makes the promised path-stable restore possible; `skills/ha-nova/config-snapshots.md` — the `.bak` holds only ONE step, the snapshot store keeps history; on capture failure follow its capture-failure stop; skip for a brand-new file), then `write_file` with `backup: true` (the default). The relay writes `<file>.bak` first — that is your immediate rollback; name it in the preview. A brand-new file gets no `.bak` — say so.
4. **Validate**: `POST /api/config/core/check_config`. `{"result":"invalid"}` means Home Assistant would refuse this config: roll back immediately — restore the `.bak` (`read_file` it, `write_file` it back with `"backup": false`, or the `.bak` becomes the invalid file); a NEW file has no `.bak`, `delete_file` it instead. Tell the user what was wrong, and do NOT reload.
5. **Reload the right domain** — a full restart is almost never necessary:
   - template sensors -> `template.reload`
   - REST sensors -> `rest.reload`
   - command-line sensors -> `command_line.reload`
   - themes -> `frontend.reload_themes`
   - packages / anything under `homeassistant:` -> `homeassistant.reload_core_config` (say when a real restart IS required — some keys only apply at boot).
6. **Verify by read-back**: the new entity must appear in `/api/states/<entity_id>` with a real state. A successful reload is not proof: an entity that never appears means the config was accepted but the platform rejected it. Report that honestly and offer the `.bak` restore. For a multi-entity file, also confirm the file's OTHER entities still appear in `/api/states` — a silently dropped sibling passes `check_config` (still-valid config, fewer entities).

## Conventions

- Keep HA NOVA's own additions in `/config/ha_nova/` and include them from `configuration.yaml` — a small, reviewable footprint the user can delete in one go. Add the `!include` line only once, and show it before writing it.
- Never touch `secrets.yaml`, `.storage/`, or the recorder database: the relay refuses them anyway, and needing them means the approach is wrong.
- Code is not configuration: the relay refuses `custom_components/`, `python_scripts/`, `www/` and writes only `.yaml`/`.yml`/`.conf`/`.json`/`.txt`/`.md` — never offer to place scripts there.

## File-Change Preview

The Preview Card variant for file edits (`skills/ha-nova/output-rules.md` -> Cards) — effect sentences, then ONLY the changed section (after-state), what it replaces, the backup line:

```
📝 Preview: file change template_sensors.yaml
Adds a sensor that shows whether any window is open.

New section (the only part that changes):
- binary_sensor:
    - name: "Any window open"

Replaces: nothing — this section is new. A backup (.bak) is written first.
⚠️  Nothing saved yet.
Options: apply · show yaml · cancel   (show yaml = the whole resulting file)
```

For an edit to an existing section, show the after-state section and one sentence naming what the old one did.

## When file access is off (the default)

Do not argue around it. Produce the exact YAML block, name the file, and give the two `/core` commands to apply it (`check_config`, then the reload service). The user pastes the block with the File editor App or their own editor. This path is fully supported.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. yaml-config specifics:
- `FILE_ACCESS_DISABLED` / `FILE_ACCESS_READONLY`: a configuration choice, not an error — see Bootstrap.
- `FILE_PATH_DENIED`: the path is permanently off-limits (secrets, `.storage`, database, logs, executable dirs). Do not look for a way around it.
- `FILE_TYPE_DENIED`: not a writable configuration format (see Conventions).
- `FILE_NOT_TEXT` / `FILE_TOO_LARGE`: wrong target, or a file this skill has no business editing.
- `check_config` invalid: roll back BEFORE reporting (`.bak` restore, or delete a new file), then explain.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Previews use the File-Change Preview above; results use the Result Card. Report the verification result: which entity appeared, with which state. If the entity did not appear, say so plainly and offer the restore.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- **Whole-file replacement**: `write_file` replaces the entire file. Always read first and show the File-Change Preview.
- **The `.bak` is the rollback and it is not automatic beyond one step**: a second write overwrites the first backup. For a bigger change, offer a Home Assistant backup first (`ha-nova:backup`).
- **check_config before reload, always.** Reloading an invalid configuration can drop entities that other automations depend on.
- Never enable `file_access` on the user's behalf, and never ask twice.

## Guardrails

- One file per operation.
- Never write a file without a preceding read and a shown File-Change Preview.
- A user-requested `delete_file` is code-gated like any delete AND captures the auto config snapshot of the file first (it has no `.bak`; `skills/ha-nova/config-snapshots.md`).
- Never claim success from a reload alone — the entity must exist in `/api/states`.
- Do not add `!include` lines that already exist, and never rewrite `configuration.yaml` wholesale to add one.
