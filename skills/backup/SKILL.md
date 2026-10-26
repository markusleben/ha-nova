---
name: backup
description: Use when checking Home Assistant backup status, creating a backup (including a safety backup before risky changes), inspecting or deleting backups, or changing the automatic-backup schedule, retention, or locations through HA NOVA Relay.
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
- automatic-backup settings: schedule, retention, locations, contents

Not in scope:
- **restoring** — a restore stops and reboots Home Assistant; never call it from here. Point to HA UI: Settings > System > Backups.
- the encryption key and per-location encryption (the CLI refuses both), and the first automatic setup — HA UI.

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
2. Preview it — estimate size and duration from the newest full backup — and ask natural confirmation (context skill → Active Preview Confirmation).
3. Primary path — the user's own settings (encryption, locations, scope): `{"type":"backup/generate_with_automatic_settings"}`.
4. Fallback when that fails because automatic settings are not configured: `{"type":"backup/generate","name":"<name>","agent_ids":["<local-agent>"],"include_homeassistant":true,"include_database":true}`:
   - discover the local agent via `{"type":"backup/agents/info"}`: Supervised installs register `hassio.local`, Core installs `backup.local` — never hardcode
   - add `"include_all_addons":true` ONLY for `hassio.local` (Core rejects App options)
   - the encryption password is hidden: never invent one; first say this backup gets no password
5. Both generate commands return when the job is INITIATED, not when it finishes. Poll `backup/info` every ~10 s until `state` returns to `idle` and a new backup appears in `backups`; then check the new backup's `failed_agent_ids`, `failed_addons`, and `failed_folders`: if any is non-empty, report partial success, never plain success. Otherwise report name, date, size, and locations by name (`backup/agents/info`).
6. If Status afterwards shows the attempt failed (`last_action_event`/no new backup), report the failure — never claim success from initiation alone.

### Safety backup before risky changes
Proportionality (`../ha-nova/recovery-coverage.md`):
- Safety backups are for far-reaching changes only (irreversible deletes, full-document overwrites, bulk operations) — never suggest one for routine small edits. A full backup can be many GB and take a long time.
- Check Status FIRST: when a recent completed full backup already covers the change, say so instead of creating another.
- Create one only when the user asks or accepts, or the last full backup is stale for what the change touches; then proceed with the risky change only after the backup completed.

### Automatic settings
`{"type":"backup/config/info"}` reads them; no saved key (`create_backup.password` empty) → first setup in the HA UI. One `backup/config/update` item, only changed keys, via `ha-nova plan raw` → `ha-nova apply`. `schedule` (`recurrence` `never`/`daily`/`custom_days`, `days`, `time`), `retention`, and `agents.<id>.retention` (`copies`, `days`) replace whole: send every field from the read. `create_backup` (`agent_ids`, `include_database`, `include_folders`, `include_all_addons`, `include_addons`, `name`) merges; name locations via `backup/agents/info`. A retention takes the one-time code: it deletes older automatic backups. Verify by re-reading.

### Inspect
`{"type":"backup/details","backup_id":"<id>"}` → what is included (Home Assistant, database, Apps, folders), which locations hold it, protected (encrypted) or not.

### Delete
1. Require `state: idle` first (one backup operation at a time). Resolve the exact `backup_id` from Status; show name, date, and locations in the preview — deletion removes it from ALL listed locations, irreversibly.
2. Never delete the only backup or the newest completed backup — manual or automatic — refuse outright and explain: it is the recovery net every other skill points to. If the user insists, point to the HA UI (Settings > System > Backups) for that deliberate exception; this skill does not carry a bypass.
3. Require the one-time code — mint four random digits, display them in the Options slot, and proceed only on a reply that is just the code (spaces ignored).
4. `{"type":"backup/delete","backup_id":"<id>"}`; verify via Status that the backup is gone.

## Error Handling

- `generate_with_automatic_settings` failing on an instance without automatic settings: expected — use the explicit fallback (Create step 4), and suggest configuring automatic backups in the HA UI.
- `state` still in progress after ~5 minutes: the backup may be large — do not fire a second generate; say it continues in the background and how to check later.
- Unknown `backup_id` on details/delete: re-run Status — backups can be pruned by retention settings between reads.
- Full relay error taxonomy: `../ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `../ha-nova/output-rules.md`: plain words, HA names verbatim; unless asked or names clash, no ids, "entity", modes, Relay/HA URLs, snapshots, raw JSON. Previews, deletes, and results use the Cards defined there (`../ha-nova/output-cards.md`).

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

- Create and settings use natural confirmation; delete uses the one-time code only.
- Restore is never executed here — it reboots the system; always route to the HA UI.
- A backup is only real once it appears completed in Status — initiation is not success.
