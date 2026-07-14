---
name: backup
description: Use when checking Home Assistant backup status, creating a backup (including a safety backup before risky changes), inspecting backups, or deleting old backups through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Backup

## Scope

Backup lifecycle:
- status: existing backups, last/next automatic backup, backup state
- create a backup (user request, or safety backup before risky changes)
- inspect one backup's contents
- delete backups

Not in scope:
- **restoring** — a restore stops and reboots Home Assistant; never call it from here. Point to HA UI: Settings > System > Backups.
- changing automatic-backup settings or encryption keys — HA UI.

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

File-based WS requests: `ha-nova relay ws --data-file <payload-file> --out <result-file>`.
WS response body: `.data` (`skills/ha-nova/relay-api.md` → Standard Envelope).

## Flow

### Status
`{"type":"backup/info"}` → report from `.data`:
- `backups` (count, newest `date`, sizes via `agents`), `state` (`idle` or in progress). Distinguish full backups (`homeassistant_included`) from partial App backups and automatic (`with_automatic_settings`) from manual; when the newest backup is partial, also report the newest FULL backup
- `last_completed_automatic_backup`, `next_automatic_backup` — say plainly when automatic backups are NOT configured (`next` is null)
- surface `agent_errors` when non-empty (a failing backup location is silent data-loss risk)

### Create
1. Read Status first; if `state` is not `idle`, a backup is already running — poll instead of starting another.
2. Preview what will happen — estimate size and duration from the newest full backup so the user decides informed — and ask natural confirmation (see context skill → Active Preview Confirmation).
3. Primary path — the user's own settings (encryption, locations, scope): `{"type":"backup/generate_with_automatic_settings"}`.
4. Fallback when that fails because automatic settings are not configured: `{"type":"backup/generate","name":"<name>","agent_ids":["<local-agent>"],"include_homeassistant":true,"include_database":true}`:
   - discover the local agent via `{"type":"backup/agents/info"}`: Supervised installs register `hassio.local`, Core installs `backup.local` — never hardcode
   - add `"include_all_addons":true` ONLY for `hassio.local` (Core rejects App options)
   - if `backup/config/info` shows an encryption password configured, pass it as `password`; never invent one
5. Both generate commands return when the job is INITIATED, not when it finishes. Poll `backup/info` every ~10 s until `state` returns to `idle` and a new backup appears in `backups`; then check the new backup's `failed_agent_ids`, `failed_addons`, and `failed_folders`: if any is non-empty, report partial success — the backup exists but is missing those locations, Apps, or folders — never plain success. Otherwise report name, date, size, and locations. If still running after ~5 minutes, say the backup continues in the background and how to check later.
6. If Status afterwards shows the attempt failed (`last_action_event`/no new backup), report the failure — never claim success from initiation alone.

### Safety backup before risky changes
Other skills name HA Backups as the recovery path (see `skills/ha-nova/write-safety.md` → Safety-Mechanism Availability). Proportionality rules:
- Safety backups are for far-reaching changes only (irreversible deletes, full-document overwrites, bulk operations) — never suggest one for routine small edits. A full backup can be many GB and take a long time.
- Check Status FIRST: when a recent completed full backup already covers the change (for example last night's automatic backup with no config changes since), say so instead of creating another.
- Create a fresh one only when the user asks or accepts, or the last full backup is stale relative to what the change touches; then proceed with the risky change only after the backup completed.

### Inspect
`{"type":"backup/details","backup_id":"<id>"}` → what is included (Home Assistant, database, Apps, folders), which locations hold it, protected (encrypted) or not.

### Delete
1. Require `state: idle` first (one backup operation at a time). Resolve the exact `backup_id` from Status; show name, date, and locations in the preview — deletion removes it from ALL listed locations, irreversibly.
2. Never delete the only backup — refuse outright and explain: it is the recovery net every other skill points to. Deleting the newest automatic backup requires the user to explicitly restate that intent after a warning (it is the freshest recovery point), before the token prompt.
3. Require exact token confirmation `confirm:<token>` — generate a short token, display it in the Options slot, and proceed only when the user types it back exactly.
4. `{"type":"backup/delete","backup_id":"<id>"}`; verify via Status that the backup is gone.

## Error Handling

- `generate_with_automatic_settings` failing on an instance without automatic settings: expected — use the explicit fallback (Create step 4), and suggest configuring automatic backups in the HA UI.
- `state` stuck in progress across the poll window: the backup may be large — do not fire a second generate; report and offer to check later.
- Unknown `backup_id` on details/delete: re-run Status — backups can be pruned by retention settings between reads.
- Full relay error taxonomy: `skills/ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all output.

- `Backups` / `Status`
- `Planned change`
- `Save status` / `Delete status` before confirmation
- `Options` / confirmation token
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots. Status renders the Report shape; backup lists render the List Frame (output-rules.md). Dates are ISO timestamps with offsets — convert to the user's local time; sizes human-readable — never raw JSON.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Create uses natural confirmation; delete uses exact token confirmation only.
- Restore is never executed here — it reboots the system; always route to the HA UI.
- A backup is only real once it appears completed in Status — initiation is not success.
- One backup operation at a time; verify by re-reading Status, not by command success alone.
