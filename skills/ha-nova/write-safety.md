# HA NOVA Write Safety

Shared mechanics for two write-time safety features, owned here and referenced by
`ha-nova:write` and `ha-nova:helper` (do not duplicate them):

- **Pre-Write Diff** — show exactly what a change alters before it is applied.
- **Update-Revert** — durably undo the most recent update.

Skill source stays English. Localize headings and labels at runtime per
`skills/ha-nova/output-rules.md`.

Confirmation validity is owned by `skills/ha-nova/SKILL.md` → Safety Baseline →
Active Preview Confirmation. This file only defines preview/diff/revert
mechanics.

## Output hygiene (write previews & results)

A write preview (create/update/delete) or post-write result shows only
user-meaningful fields — this does not constrain an explicit read the user asked
for. Never surface internal bookkeeping in that user-facing write text:

- no internal check codes (S/R/P/M/F/H, R-18, …);
- no raw numeric config-id / `target_id` — it is an internal handle; identify the
  item by its name/alias and `entity_id`, not by its number;
- no `bp_status`, "best-practice snapshot", "baseline", or cache wording.

Best-practice freshness (`bp_status`) is an **internal gate input only**:

- `fresh`, or `stale` on a simple change → continue silently; never mention it.
- `stale`/`missing`/`invalid` on a complex change → you may decline, but say it in
  plain language and point to a Home Assistant Backup as the safety net — never
  name the snapshot or its age.

## Pre-Write Diff (`## Changes`)

The diff is rendered by the CLI, not by you — so it is byte-identical on every
run, every model, every client. Treat the `ha-nova diff` output like `git diff`:
a stable artifact you print **verbatim**. Do not hand-format, reorder, relabel,
translate, or summarize the change lines. The `-` change lines come ONLY from the
command's stdout this turn — if you did not just run `ha-nova diff`, do not print
a Changes block at all. There is no hand-computed fallback. `## Changes` is the
historical slot name, not a required literal Markdown heading; in terminal-like
clients prefer a plain localized label for the changes slot.

**update**:
1. Write the current config (resolve `CURRENT_CONFIG`) and the proposed draft to
   two files.
2. Prefer `ha-nova diff --before <current-file> --after <draft-file> --out <diff-file>`, then read `<diff-file>` with the native file-reading tool. If `--out` is unavailable, use stdout only when the client will not truncate the command output.
3. Print a localized label for the changes slot, then the diff file/stdout lines verbatim. If the
   diff is empty, there is nothing to change — say so plainly instead of showing
   an empty block.

**create**: no diff (there is no "before") — give a one-line plain-language
summary of what the new item does.

**delete**: no `## Changes` (the consumer-check result already covers it).

### User-authored notification copy

Notification text is user-authored copy, not disposable implementation detail.
For unrelated automation/script edits, preserve these fields exactly unless the
user explicitly asks to change notification wording or notification behavior:

- notification `title` and `message`;
- templates inside notification `title` or `message`;
- notification metadata and actionable payloads such as tags, groups, channels,
  sounds, URLs, actions, critical flags, and nested `data`.

Do not silently restyle, relocalize, shorten, expand, or restructure existing
notification text during a rename, refactor, timing change, trigger/condition
change, helper change, or service-call change. If the copy looks improvable,
offer it as a separate suggestion before preview; do not merge it into the draft
unless the user accepts it.

If notification copy does change, the Changes slot must show the old and new text or
payload. A count-only array line such as `Actions: 7 → 8 items` is not enough
when an existing notification title, message, or notification payload changed.

### Fixed update-preview shape

Render a terminal-friendly preview in this exact order, nothing extra — users
should learn one shape and always recognize it:

1. Preview-summary slot: target name and one plain-language sentence.
2. Changes slot: the `ha-nova diff` file/stdout lines, verbatim.
3. Pre-write-check/impact slot: one or two short lines.
4. Save-status slot: explicitly say that nothing has been saved yet.
5. Options slot: explicit choices with literal `apply`, `show yaml`, and `cancel`.

Do **not** re-describe the whole automation (entities, every trigger/action) —
the diff already states what changes. Keep it scannable.

### Full config: always offered, never forced

A non-technical user reads the Changes slot, not raw YAML. So:

- **update**: lead with the Changes slot; always close with the same localized
  Options block.
- **create**: show a compact human summary of the new item, the pre-write check,
  a not-saved-yet line, and the same Options block.

Present the confirm/apply choice via the menu-or-numbered convention (see
`skills/ha-nova/SKILL.md` → Interactive Choices).

Shape (the `-` lines are exactly what `ha-nova diff` produced this turn — never
invent, shorten, pre-fill, or copy these from an example). The labels below are
semantic placeholders; localize them before showing the user:

```
<localized changes label>:
<paste the ha-nova diff file/stdout here, unchanged>

<localized options label>:
1. apply — save exactly this preview.
2. show yaml — show the full proposed config.
3. cancel — do not save anything.
```

## Update-Revert (durable, identity-preserving)

Scope: **update only**. A create is undone by deleting the new item through the
normal HA NOVA delete flow; that delete still requires a delete preview, exact
`confirm:<token>`, and absence verification, even when the item was created earlier in the same session.
Do not call this `revert`, and do not imply that manual deletion or a full Home Assistant Backup restore is the only cleanup path.
A delete has no HA NOVA `revert`; rollback requires restoring a suitable existing
Home Assistant Backup, or recreating the item. Point the user to HA Backups
(Settings > System > Backups) for that case.

### 1. Capture the snapshot (after a verified update)

After Phase-4 verification of an update succeeds, store exactly one snapshot.
N=1: only the most recent update is revertible.

- `before_config` = the pre-write read-back captured during resolve
  (`CURRENT_CONFIG`). It is HA's own normalized form, so re-writing it
  round-trips cleanly.
- `expected_after` = the post-write verified read-back (`VERIFICATION.observed`
  from the apply phase, or the Phase-4 re-read body). **Not** the draft — HA
  reformats on write, so the draft would never match the live state and every
  revert would falsely look like drift.

Store the **complete** read-back body for both `before_config` and
`expected_after`, exactly as HA returned it. Do **not** reduce it to core fields
like `{alias, triggers, conditions, actions, mode}`: the Phase-4 comparison
filters fields for display only, but the snapshot must keep every field a real
automation carries (`description`, `max`, `mode`, …). Dropping a field HA stores
makes the next `verify` see it as drift and blocks a safe revert. Leaving the
bookkeeping `id`/`unique_id` in is fine — `ha-nova snapshot verify` ignores
them (and treats a missing field the same as an empty one).

Write the record with the client's file tool, then save it — file-based like
`ha-nova diff`, so there is no stdin redirect to get wrong on any shell:

```text
ha-nova snapshot save --data-file <record-file>
```

Record shape:

```json
{"op":"update","domain":"automation","target_id":"<config id>",
 "before_config":{ ... },"expected_after":{ ... }}
```

### 2. Offer the revert

After reporting the update, offer a localized affordance that names the point-in-time
rollback option in the same breath — never a bare "undo":

> reply `revert` to undo this update; for point-in-time rollback, restore a suitable existing Home Assistant Backup.

### 3. Execute `revert`

1. Run `ha-nova snapshot show` — the **only** source of `before_config`. Never
   reconstruct the previous config from memory or context.
2. Re-read the current live config of `target_id` (same read-back path as
   Phase 4) into a file.
3. Drift check:

   ```text
   ha-nova snapshot verify --against <live-file>
   ```

   - `match` (exit 0) → the config is still exactly as written; safe to restore.
   - `drift` (exit 2) → it changed since the write (e.g. an external UI edit).
     Do NOT overwrite blindly. Show what differs and ask the user how to proceed.
4. On `match`, restore the `before_config` from step 1 through the skill's own
   write path, then verify it like any other write:
   - `ha-nova:write` (automation/script): run the apply phase with
     `OPERATION=update`, `TARGET_ID=<target_id>`, `PAYLOAD=before_config`.
   - `ha-nova:helper` storage family: rebuild a schema-valid `{type}/update`
     payload from `before_config` — set `{type}_id` from its stored `id` and
     include only writable update fields (drop read-only `id`/`entity_id`; see
     `skills/ha-nova/helper-schemas.md` → Common Rules). Sending the raw
     `before_config` list item fails: it lacks the required `{type}_id`. Re-issue
     that `{type}/update` WS call, then re-read the list item to confirm.
   - `ha-nova:helper` config-entry family: no auto-revert (options-flow writes
     are multi-step) — point to HA Backups.

Use `ha-nova snapshot show` to read the stored record (including
`before_config`) back when you need it.

### Honesty

- The store holds only the last update; the next write overwrites it.
- `revert` restores the captured config through the normal write path; it does
  not replay HA's exact byte-for-byte formatting. For a guaranteed point-in-time
  restore, Home Assistant Backups is the source of truth.

## Safety-Mechanism Availability by Skill

Diff and revert coverage is deliberately uneven. Never imply a mechanism a
skill does not have; when only Backups remain, say so before the write — and
offer to create one first via `ha-nova:backup` (safety-backup flow).

| Skill / family | Pre-write diff | Update-revert | Fallback recovery path |
|---|---|---|---|
| `write` (automation/script) | yes (`ha-nova diff`) | yes (verified updates, N=1) | HA Backups |
| `helper` storage family | yes | yes (verified updates, N=1) | HA Backups |
| `helper` config-entry family | diff only | no (multi-step options flow) | HA Backups |
| `dashboard` | preview + read-back verify | no | HA Backups |
| `scene` | preview + read-back verify | no | HA Backups |
| `todo` | preview + read-back verify | no (list delete irreversible) | re-add items; HA Backups for lists |
| `organize` | field preview | no (registry deletes irreversible) | HA Backups |
| `service-call` | state-delta preview | no (runtime action, not config) | re-run corrective service call |
| `fallback` (experimental writes) | payload preview + read-back verify | no | HA Backups |
