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

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

File-based WS requests: `ha-nova relay ws --data-file <payload-file> --out <result-file>`.
WS response body: `.data` (`../ha-nova/relay-api.md` → Standard Envelope).

## Flow

### Status
`{"type":"backup/info"}` → report from `.data`:
- `backups` (count, newest `date`, sizes via `agents`), `state` (`idle` or in progress). Distinguish full backups (`homeassistant_included`) from partial App backups and automatic (`with_automatic_settings`) from manual; when the newest backup is partial, also report the newest FULL backup
- `last_completed_automatic_backup`, `next_automatic_backup` — say plainly when automatic backups are NOT configured (`next` is null)
- non-empty `agent_errors`: name each failing location as `backup/agents/info` names it (a failing backup location is silent data-loss risk)

### Create
1. Read Status first; if `state` is not `idle`, a backup is already running — poll instead of starting another.
2. Preview what will happen — estimate size and duration from the newest full backup so the user decides informed — and ask natural confirmation (see context skill → Active Preview Confirmation).
3. Primary path — the user's own settings (encryption, locations, scope): `{"type":"backup/generate_with_automatic_settings"}`.
4. Fallback when that fails because automatic settings are not configured: `{"type":"backup/generate","name":"<name>","agent_ids":["<local-agent>"],"include_homeassistant":true,"include_database":true}`:
   - discover the local agent via `{"type":"backup/agents/info"}`: Supervised installs register `hassio.local`, Core installs `backup.local` — never hardcode
   - add `"include_all_addons":true` ONLY for `hassio.local` (Core rejects App options)
   - if `backup/config/info` shows an encryption password configured, pass it as `password`; never invent one
5. Both generate commands return when the job is INITIATED, not when it finishes. Poll `backup/info` every ~10 s until `state` returns to `idle` and a new backup appears in `backups`; then check the new backup's `failed_agent_ids`, `failed_addons`, and `failed_folders`: if any is non-empty, report partial success — the backup exists but is missing those locations, Apps, or folders — never plain success. Otherwise report name, date, size, and locations by name (`backup/agents/info`). If still running after ~5 minutes, say the backup continues in the background and how to check later.
6. If Status afterwards shows the attempt failed (`last_action_event`/no new backup), report the failure — never claim success from initiation alone.

### Safety backup before risky changes
Other skills name HA Backups as the recovery path (see `../ha-nova/recovery-coverage.md` → Safety-Mechanism Availability). Proportionality rules:
- Safety backups are for far-reaching changes only (irreversible deletes, full-document overwrites, bulk operations) — never suggest one for routine small edits. A full backup can be many GB and take a long time.
- Check Status FIRST: when a recent completed full backup already covers the change (for example last night's automatic backup with no config changes since), say so instead of creating another.
- Create a fresh one only when the user asks or accepts, or the last full backup is stale relative to what the change touches; then proceed with the risky change only after the backup completed.

### Inspect
`{"type":"backup/details","backup_id":"<id>"}` → what is included (Home Assistant, database, Apps, folders), which locations hold it, protected (encrypted) or not.

### Delete
1. Require `state: idle` first (one backup operation at a time). Resolve the exact `backup_id` from Status; show name, date, and locations in the preview — deletion removes it from ALL listed locations, irreversibly.
2. Never delete the only backup or the newest completed backup — manual or automatic — refuse outright and explain: it is the recovery net every other skill points to (a manual safety backup can be the freshest recovery point). If the user insists (for example the newest backup is corrupted or partial), point to the HA UI (Settings > System > Backups) for that deliberate exception; this skill does not carry a bypass.
3. Require the one-time code — mint four random digits, display them in the Options slot, and proceed only on a reply that is just the code (spaces ignored).
4. `{"type":"backup/delete","backup_id":"<id>"}`; verify via Status that the backup is gone.

## Error Handling

- `generate_with_automatic_settings` failing on an instance without automatic settings: expected — use the explicit fallback (Create step 4), and suggest configuring automatic backups in the HA UI.
- `state` stuck in progress across the poll window: the backup may be large — do not fire a second generate; report and offer to check later.
- Unknown `backup_id` on details/delete: re-run Status — backups can be pruned by retention settings between reads.
- Full relay error taxonomy: `../ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `../ha-nova/output-rules.md`: plain words, HA names verbatim; unless asked, no ids, "entity", modes, Relay/HA URLs, snapshot files, raw JSON. Previews, deletes, and results use the Cards defined there (`../ha-nova/output-cards.md`).

- `Backups` / `Status`
- `Planned change`
- `Save status` / `Delete status` before confirmation
- `Options` / one-time-code prompt
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots. Status renders the Report shape; backup lists render the List Frame (output-cards.md). ISO dates in the user's local time; sizes human-readable.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's window click or phone tap, else their one-time code from HA NOVA; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any target, payload, endpoint, or scope change voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- Drafts follow `../ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing speculative.

- Create uses natural confirmation; delete uses the one-time code only.
- Restore is never executed here — it reboots the system; always route to the HA UI.
- A backup is only real once it appears completed in Status — initiation is not success.
- One backup operation at a time; verify by re-reading Status, not by command success alone.
