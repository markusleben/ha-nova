---
name: yaml-config
description: Use when creating or editing Home Assistant configuration that only exists as YAML files — template sensors beyond the helper UI, REST and command-line sensors, packages, and themes — through HA NOVA Relay's opt-in file access.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay App in Home Assistant.
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

This skill needs **Relay 0.4.0 or newer** AND file access enabled. Both are the user's decision, not yours:
- Probe once: `ha-nova relay files --data-file <payload-file>` with `{"action":"list_dir","path":"/config"}`.
- `FILE_ACCESS_DISABLED` -> file access is OFF (the default). Tell the user how to turn it on (Settings > Apps > NOVA Relay > Configuration > `file_access`: `readwrite`, then restart the App) and what it means, then STOP. Do not nag, and do not work around it.
- `FILE_ACCESS_READONLY` -> reads work, writes do not. Offer the manual path below instead of asking for more permission.
- If the relay is older than 0.4.0, say so plainly — the endpoint does not exist there.

## Relay Contract

- `ha-nova relay files --data-file <payload-file>` — file transport. Actions: `list_dir`, `read_file`, `write_file` (`content`, `backup` defaults to true), `delete_file`.
- `ha-nova relay core --method POST --path /api/config/core/check_config` — validate the configuration BEFORE reloading.
- `ha-nova relay core --method POST --path /api/services/<domain>/reload --body-file <payload-file>` — apply.
- `ha-nova relay core --method GET --path /api/states/<entity_id>` — verify the entity actually exists afterwards.

## Flow

Never skip a step.

1. **Read before write.** `read_file` the target (or `list_dir` to find it). Never write a file you have not read: `write_file` replaces the whole file, so an unread file means an unknown loss.
2. **Build the change in memory** and show a real diff of before/after in the `Changes` slot. Say plainly which file, and that the whole file is being replaced.
3. **Confirm**, then `write_file` with `backup: true` (the default). The relay writes `<file>.bak` first — that is your rollback, and you should name it in the preview.
4. **Validate**: `POST /api/config/core/check_config`. `{"result":"invalid"}` means Home Assistant would refuse this config: restore the `.bak` immediately (`read_file` it, `write_file` the original back), tell the user what was wrong, and do NOT reload.
5. **Reload the right domain** — a full restart is almost never necessary:
   - template sensors -> `template.reload`
   - REST sensors -> `rest.reload`
   - command-line sensors -> `command_line.reload`
   - themes -> `frontend.reload_themes`
   - packages / anything under `homeassistant:` -> `homeassistant.reload_core_config` (say when a real restart IS required — some keys only apply at boot).
6. **Verify by read-back**: the new entity must appear in `/api/states/<entity_id>` with a real state. A successful reload is not proof: an entity that never appears means the config was accepted but the platform rejected it. Report that honestly and offer the `.bak` restore.

## Conventions

- Keep HA NOVA's own additions in `/config/ha_nova/` and include them from `configuration.yaml` — a small, reviewable footprint the user can delete in one go. Add the `!include` line only once, and show it before writing it.
- Never touch `secrets.yaml`, `.storage/`, or the recorder database: the relay refuses them anyway, and needing them means the approach is wrong.
- Code is not configuration: the relay refuses `custom_components/`, `python_scripts/`, and `www/` entirely and writes only `.yaml`/`.yml`/`.conf`/`.json`/`.txt`/`.md` — never offer to place scripts or web assets.
- Prefer a helper over a YAML file whenever the helper can express it (`ha-nova:helper`).

## When file access is off (the default)

Do not treat this as a blocker to argue around. Produce the exact YAML block, name the exact file it belongs in, and give the two commands to apply it (`check_config`, then the reload service) — which work through `/core` without file access. The user pastes the block with the File editor App or their own editor. This path is fully supported and is often the right one.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. yaml-config specifics:
- `FILE_ACCESS_DISABLED` / `FILE_ACCESS_READONLY`: a configuration choice, not an error — see Bootstrap.
- `FILE_PATH_DENIED`: the path is permanently off-limits (secrets, `.storage`, database, logs, executable dirs). Do not look for a way around it.
- `FILE_TYPE_DENIED`: not a writable configuration format (see Conventions).
- `FILE_NOT_TEXT` / `FILE_TOO_LARGE`: wrong target, or a file this skill has no business editing.
- `check_config` invalid: restore the `.bak` BEFORE reporting, then explain.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Name the file, show the diff (not the whole file unless asked), state that a `.bak` was written, and report the verification result: which entity appeared, with which state. If the entity did not appear, say so plainly and offer the restore.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- **Whole-file replacement**: `write_file` replaces the entire file. Always read it first, always preview the diff, and never write a file whose current content you have not seen.
- **The `.bak` is the rollback and it is not automatic beyond one step**: a second write overwrites the first backup. For a bigger change, offer a Home Assistant backup first (`ha-nova:backup`).
- **check_config before reload, always.** Reloading an invalid configuration can drop entities that other automations depend on.
- Never enable `file_access` on the user's behalf, and never ask twice.

## Guardrails

- One file per operation.
- Never write a file without a preceding read and an explicit diff.
- Never claim success from a reload alone — the entity must exist in `/api/states`.
- Do not add `!include` lines that already exist, and never rewrite `configuration.yaml` wholesale to add one.
