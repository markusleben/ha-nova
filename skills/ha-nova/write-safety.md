# HA NOVA Write Safety

Shared mechanics for the write-time safety features, owned here and referenced
by `ha-nova:write` and `ha-nova:helper` (do not duplicate them):

- **Pre-Write Diff** — show exactly what a change alters before it is applied.
- **Behavior narrative** — say what the change does, not only which fields move.
- **Verification Honesty** — post-write wording names the proven scope.
- **Multi-Target Changes** — plan-first flow for one logical change over
  several items.
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
run, every model, every client. It emits GFM table data rows
(`| Field | before | after |`; an empty side shows `—`). Treat the `ha-nova diff`
output like `git diff`: a stable artifact you print **verbatim**. Do not
hand-format, reorder, relabel, translate, merge, or summarize the rows —
do not re-align pipes or pad cells. The table rows come ONLY from the command's
output this turn — if you did not just run `ha-nova diff`, do not print a
Changes block at all. There is no hand-computed fallback. `## Changes` is the
historical slot name, not a required literal Markdown heading; in terminal-like
clients prefer a plain localized label for the changes slot.

**update**:
1. Write the current config (resolve `CURRENT_CONFIG`) and the proposed draft to
   two files.
2. Prefer `ha-nova diff --before <current-file> --after <draft-file> --out <diff-file>`, then read `<diff-file>` with the native file-reading tool. If `--out` is unavailable, use stdout only when the client will not truncate the command output.
3. Print a localized label for the changes slot, then exactly two hand-written
   lines — the localized table header row (field / before / after) and the
   literal separator `|---|---|---|` (never localized, never re-aligned) — then
   the diff file/stdout rows verbatim beneath. If the diff is empty, there is
   nothing to change — say so plainly instead of showing an empty table.

**create**: no diff (there is no "before") — give a one-line plain-language
summary of what the new item does.

**delete**: no `## Changes` (the consumer-check result already covers it).

### Drift check before apply (update)

Confirmation binds to the previewed diff BASIS, not only the payload. If the
conversation paused between preview and confirmation, or the target may have
been edited outside this session (HA UI open, another client), re-read the
target immediately before the write and structurally compare it against the
`CURRENT_CONFIG` the diff was computed from. On any foreign change: STOP —
the confirmation has expired; recompute the diff against the live config and
show the updated preview. Never silently overwrite an external edit — a
full-document write would revert it without a trace.

### Behavior narrative (required with every update preview)

The diff states WHAT fields change; the preview-summary sentence must state
what the change DOES — the behavioral effect in plain language ("the three
light actions now only run when someone is home", "the wait now has a 2-minute
timeout"). This narrative is your interpretation, clearly outside the Changes
slot — the diff lines stay verbatim CLI output, the two never mix. The diff is
what will be saved: if your narrative and the diff disagree, fix the draft or
the narrative before showing the preview — never show both and let the user
guess.

Confirmation quality depends on it: a user who cannot tell what the payload
changes cannot meaningfully confirm it. If the diff still contains a
count-only or `… and N more` line — or a truncated (`…`) value — for something
the user's request touched, the summary MUST name what was added, removed, or nested there,
and spell out what a cut-off value says in full. If you cannot explain the
behavioral effect of your own draft, stop and re-derive it —
never ask for confirmation of a change you cannot describe.

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
offer it as a separate Suggestion Block item (output-rules.md) before preview;
do not merge it into the draft unless the user accepts it.

If notification copy does change, the Changes slot must show the old and new text or
payload. A count-only array line such as `| Actions | 7 items | 8 items |` is not enough
when an existing notification title, message, or notification payload changed.

### Fixed update-preview shape

Render a terminal-friendly preview in this exact order, nothing extra — users
should learn one shape and always recognize it:

1. Preview-summary slot: 📝 title line (localized preview label + item type +
   name), then one to three plain-language sentences (the behavior narrative
   lives here).
2. Changes slot: your localized header row + `|---|---|---|`, then the
   `ha-nova diff` file/stdout rows, verbatim.
3. Pre-write-check/impact slot: one or two short lines.
4. Save-status slot: explicitly say that nothing has been saved yet (the ⚠️ line).
5. Options slot: explicit choices with literal `apply`, `show yaml`, and `cancel`.

This is the Preview Card of `skills/ha-nova/output-rules.md` → Cards; the
Result Card and Delete Card there frame the post-write and delete sides.

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

Shape (the table rows are exactly what `ha-nova diff` produced this turn — never
invent, shorten, pre-fill, or copy these from an example). The labels below are
semantic placeholders; localize them before showing the user:

```
📝 <localized preview label>: <type> "<name>"
<behavior narrative, one to three sentences>

| <localized field label> | <localized before label> | <localized after label> |
|---|---|---|
<paste the ha-nova diff file/stdout here, unchanged>

<pre-write check / impact line>
⚠️  <localized: nothing saved yet>
<localized options label>: apply · show yaml · cancel
```

## Verification Honesty (post-write wording)

Post-write checks prove persistence, not behavior. What they establish: the
write was accepted, the read-back matches, the domain reloaded, the runtime
entity exists. What they do NOT establish: that conditions carry the intended
meaning, that every branch is reachable, that timing assumptions hold, or that
the physical action succeeds.

Wording rules for the result and the collapsed no-findings line:

- Never a bare "verified"/"works now". Name the proven scope instead — the
  localized equivalent of: "Saved and checked: config persisted, reload OK,
  entity live. Runtime behavior was not exercised."
- Where a real run is meaningful, offer it as an explicit optional step: a
  manual trigger via `ha-nova:service-call` (its own preview + confirmation —
  it may actuate real devices; never run it unrequested). For automations and
  scripts, structure that offer as the Test Plan Card in
  `skills/ha-nova/test-run.md` (write flow → Phase 5); helper writes keep
  this plain single offer. If the user
  declines, the uncertainty stays in the final wording — do not let the
  closing sentence sound more conclusive than the checks were.

## Multi-Target Changes (one logical change, several items)

When one logical change spans multiple automations/scripts/helpers, per-target
previews alone hide the whole picture. Before the FIRST per-target preview:

1. Present the change plan: every target, what changes in each, the intended
   combined behavior, and the apply order (dependencies first).
2. State revert coverage honestly BEFORE starting: update-revert keeps the
   last 5 updated targets (one snapshot per target) — a logical change with
   more update targets than that loses auto-revert for the oldest ones; say
   so. For a broad or hard-to-reconstruct change, offer a safety backup via
   `ha-nova:backup` first (proportionality rules apply).
3. Get plan-level consent, then run the normal per-target flow (each target
   still gets its own preview + confirmation — the plan does not replace them).
4. Close with a combined summary: all targets applied, the still-revertible
   targets named (`ha-nova snapshot show --list`), and the
   verification-honesty wording for the whole change.

If a mid-sequence target fails or is cancelled, stop and show which targets
are already applied — never continue silently into a half-applied state.

This section covers multi-target updates. Deleting a reviewed multi-item
workset routes through `skills/ha-nova/batch-safety.md`: one immutable
manifest, one typed confirmation code, per-target impact checks and
verification retained, one resource family per manifest.

## Update-Revert (durable, identity-preserving)

Scope: **update only**. A create is undone by deleting the new item through the
normal HA NOVA delete flow; that delete still requires a delete preview, exact
`confirm:<token>`, and absence verification, even when the item was created earlier in the same session.
Do not call this `revert`, and do not imply that manual deletion or a full Home Assistant Backup restore is the only cleanup path.
A delete has no HA NOVA `revert`; rollback requires restoring a suitable existing
Home Assistant Backup, or recreating the item. Point the user to HA Backups
(Settings > System > Backups) for that case.

### 1. Capture the snapshot (after a verified update)

After Phase-4 verification of an update succeeds, store the snapshot. The
store keeps the last 5 updates, one per domain+target — a re-update of the
same target replaces its entry; beyond 5 targets the oldest entry is evicted.

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

Execution — `snapshot show` as the only `before_config` source, the
`--target`/`--domain`-bound drift check, and the restore through the skill's
own write path — lives in `skills/ha-nova/update-revert.md`. Load that file
whenever the user asks to revert, undo, or restore a verified update (any of
those intents — not only the literal word `revert`); it also carries the honesty limits
(5-target stack, one step back per target, no byte-exact formatting).

## Safety-Mechanism Availability by Skill

Diff and revert coverage is deliberately uneven. Never imply a mechanism a
skill does not have; when only Backups remain, say so before the write. For
far-reaching changes (irreversible deletes, full-document overwrites, bulk
operations) offer to create one first via `ha-nova:backup` — its safety-backup
flow checks for a recent full backup before creating, because full backups are
expensive. EXCEPTION: when the operation auto-captures a config snapshot
(`skills/ha-nova/config-snapshots.md`) and the store is available, the snapshot
IS the recovery net — skip the full-backup offer for that config-level op.
Never suggest a backup for routine small edits.

Config-level recovery: relays with the snapshot store (`POST /backups`) let the
covered families auto-capture a config snapshot before deletes/full-document
overwrites and restore selectively through their own write path —
`skills/ha-nova/config-snapshots.md` is the SSOT (capture rules, restore
fidelity, honest limits). The full-backup offer stays for system-level
operations and for relays whose `/backups` answers 404.

| Skill / family | Pre-write diff | Update-revert | Fallback recovery path |
|---|---|---|---|
| `write` (automation/script) | yes (`ha-nova diff`) | yes (verified updates, last 5 targets) | config snapshot (auto before delete, identity-preserving restore); HA Backups |
| `helper` storage family | yes | yes (verified updates, last 5 targets) | config snapshot (auto before delete; recreate mints a new id); HA Backups |
| `helper` config-entry family | diff only | no (multi-step options flow) | HA Backups |
| `integration-setup` | flow-step preview + config-entry read-back | no (multi-step add/reauth flow) | cancel only an unfinished add flow started in this session; after entry creation, manage or remove it in Home Assistant UI |
| `calendar` | event-field preview + bounded event read-back | no (provider event mutation) | provider recycle bin when available; otherwise recreate from the approved preview |
| `dashboard` | preview + read-back verify | no | config snapshot (auto before delete / content-dropping save); HA Backups |
| `scene` | preview + read-back verify | no | config snapshot (auto before delete, identity-preserving restore); HA Backups |
| `todo` | preview + read-back verify | no (list delete irreversible) | re-add items; HA Backups for lists |
| `updates` | preview + entity re-read verify | no (updates not downgradable) | HA Backups (offered pre-install for core/OS) |
| `energy` | change preview + read-back & validate verify | no (corrective save) | config snapshot (auto before entry-removing saves, whole-doc restore); HA Backups |
| `maintenance` | grouped preview + re-validate verify | spike adjust only (inverse call); clears/purges/removals irreversible | HA Backups (offered pre-bulk) |
| `organize` | field preview | no (registry deletes irreversible) | config snapshot (auto before rename/disable, in-place field write-back); registry deletes: HA Backups |
| `service-call` | state-delta preview | no (runtime action, not config) | re-run corrective service call |
| `admin` (persons/zones/tags/users) | field preview | no (deletes irreversible; user credentials/tokens unrecoverable) | recreate from previewed fields (tags keep their physical `tag_id`; other recreates mint new ids); HA Backups |
| `assist` (pipelines/exposure) | change preview | no | re-toggle exposure / resend prior pipeline fields; deleted pipelines recreate with a new id |
| `yaml-config` | File-Change Preview + `check_config` | one-step `.bak` restore (writes only; a second write overwrites it) | config snapshot with stored path (auto before overwrites/deletes, multi-step history); `.bak`; HA Backups |
| `mqtt` (publish) | payload preview | no (broker/device state) | clear a retained message by publishing an empty retained payload; corrective publish |
| `backup` (delete) | delete preview | no | none — a deleted archive is gone |
| `fallback` (experimental writes) | payload preview + read-back verify | no | HA Backups |
