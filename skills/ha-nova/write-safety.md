# HA NOVA Write Safety

Shared rules for two write-time safety features, owned here and referenced by
`ha-nova:write` and `ha-nova:helper` (do not duplicate them):

- **Pre-Write Diff** — show exactly what a change alters before it is applied.
- **Update-Revert** — durably undo the most recent update.

Skill source stays English. Localize headings and labels at runtime per
`skills/ha-nova/SKILL.md` → Output Localization.

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
a `## Changes` block at all. There is no hand-computed fallback.

**update**:
1. Write the current config (resolve `CURRENT_CONFIG`) and the proposed draft to
   two files.
2. Run `ha-nova diff --before <current-file> --after <draft-file>`.
3. Print a localized `## Changes` heading, then the command's lines verbatim. If
   the command prints nothing, there is nothing to change — say so plainly
   instead of showing an empty block.

**create**: no diff (there is no "before") — give a one-line plain-language
summary of what the new item does.

**delete**: no `## Changes` (the consumer-check result already covers it).

### Fixed update-preview shape

Render an update preview in this exact order, nothing extra — users should learn
one shape and always recognize it:

1. `## Changes` (the `ha-nova diff` lines, verbatim)
2. one line of pre-write impact (advisory; see `ha-nova:write` Phase 2 Step 3c)
3. the confirm step, offering: apply · show full config · cancel

Do **not** re-describe the whole automation (entities, every trigger/action) —
the diff already states what changes. Keep it scannable.

### Full config: always offered, never forced

A non-technical user reads `## Changes`, not raw YAML. So:

- **update**: lead with `## Changes`; always close with the same localized
  affordance — a `show yaml` option for the complete config (same wording every
  time, so the shape is predictable).
- **create**: show a compact human summary of the new item plus the same
  `show yaml` affordance.

Present the confirm/apply choice via the menu-or-numbered convention (see
`skills/ha-nova/SKILL.md` → Interactive Choices).

Shape (the `-` lines are exactly what `ha-nova diff` printed this turn — never
invent, pre-fill, or copy these from an example):

```
## Changes
<paste the ha-nova diff stdout here, unchanged>
```

## Update-Revert (durable, identity-preserving)

Scope: **update only**. A create is undone by deleting the new item; a delete is
undone by restoring a Home Assistant Backup. Both are out of scope here — point
the user to HA Backups (Settings > System > Backups) for those.

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

After reporting the update, offer a localized affordance that names the durable
fallback in the same breath — never a bare "undo":

> reply `revert` to undo this change — or use Home Assistant Backups anytime.

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
   - `ha-nova:helper` storage family: re-issue the storage `{type}/update` WS
     call with `before_config`, then re-read the list item to confirm.
   - `ha-nova:helper` config-entry family: no auto-revert (options-flow writes
     are multi-step) — point to HA Backups.

Use `ha-nova snapshot show` to read the stored record (including
`before_config`) back when you need it.

### Honesty

- The store holds only the last update; the next write overwrites it.
- `revert` restores the captured config through the normal write path; it does
  not replay HA's exact byte-for-byte formatting. For a guaranteed point-in-time
  restore, Home Assistant Backups is the source of truth.
