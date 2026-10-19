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
`../ha-nova/output-rules.md`.

Confirmation validity is owned by `../ha-nova/SKILL.md` → Safety Baseline →
Active Preview Confirmation. This file only defines preview/diff/revert
mechanics.

## Output hygiene (write previews & results)

A write preview (create/update/delete) or post-write result shows only
user-meaningful fields — this does not constrain an explicit read the user asked
for. Never surface internal bookkeeping in that user-facing write text:

- no internal check codes (S/R/P/M/F/H, R-18, …);
- no raw numeric config-id / `target_id` — it is an internal handle; identify the
  item by its name/alias, not by its number;
- no `bp_status`, "best-practice snapshot", "baseline", or cache wording.

Best-practice freshness (`bp_status`) is an **internal gate input only**:

- `fresh`, or `stale`/`missing`/`invalid` on a simple change → continue silently; never mention it.
- `stale`/`missing`/`invalid` on a complex change → you may decline, but say it in
  plain language and point to a Home Assistant Backup as the safety net — never
  name the snapshot or its age.

## Pre-Write Diff (`## Changes`)

The diff is rendered by the CLI, not by you — byte-identical on every run,
model, and client: GFM table rows (`| Field | before | after |`; an empty side
shows `—`). Print them **verbatim** like `git diff` output (sole exception:
webhook-id masking, output-rules.md → Technical Noise) — never hand-format,
reorder, relabel, translate, merge, summarize, or re-align them — do not re-align pipes or pad cells.
Rows come ONLY from a CLI run this turn; without one, print no Changes block.
There is no hand-computed fallback. `## Changes` is the historical slot name, not a required literal Markdown heading; in
terminal-like clients prefer a plain localized label.

- Automation/script: the `diff` of `ha-nova plan write` / `plan revert` (it
  also carries the order rows of `variables`/`trigger_variables`/`fields`).
- Other families with a diff (helper storage family): write the current config
  and the draft to two files, run
  `ha-nova diff --before <current-file> --after <draft-file> --out <diff-file>`
  (prefer `--out <diff-file>`; stdout only when the client will not truncate it), and
  read the file.

Print a localized label, exactly two hand-written lines — the localized table header row
(field / before / after) and the literal separator `|---|---|---|` — then
the rows verbatim. An empty diff means nothing changes: say so plainly.
**create**: no diff — a one-line plain-language summary. **delete**: no
Changes block (the consumer-check result covers it).

### Drift check before apply

Applies to every write that replaces a whole document or whole list: an
automation or script update, a scene, a dashboard config, energy preferences, and a YAML file.
Home Assistant has no optimistic locking anywhere — the last writer wins, silently.
Confirmation binds to the previewed BASIS, not only the payload. Automation,
script, and dashboard writes get this from the CLI: `ha-nova apply` re-plans against the
live config and sends nothing on any change (exit 2). Every other
whole-document or whole-list write (scene, energy
preferences, YAML file, helper list item): if the conversation paused or the
target may have been edited elsewhere, re-read it immediately before the write
and compare it with the preview's basis; on any foreign change STOP — the
confirmation has expired; show the updated preview. Verifying afterwards is not a substitute: a post-write check only
tells the user an external edit is already gone; the re-read before the write
is what keeps it.

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
changes cannot meaningfully confirm it. The narrative covers every collection
the diff touches — including ones the merge changed that the user never
mentioned. If the diff still contains a count-only or `… and N more` line — or
a truncated (`…`) value — for a touched collection, the summary MUST name what
was added, removed, replaced, modified, or nested there, and spell out what a
cut-off value says in full. Each added, removed, replaced, or modified entry
gets a plain-language line on what it does; entries with one shared effect may
share a line ("all three notify actions now go to Anna's phone"). Describe a
nested change at the depth needed to understand its effect ("the new choose
branch skips heating when a window is open") — container counts alone never
qualify, and echoing diff type names ("condition state", "platform: time") is
not a description. A preview that explains a touched collection only as a
count or type change is not a valid confirmation basis (context skill →
Active Preview Confirmation) — fix the draft or the narrative before asking.
If you cannot explain the behavioral effect of your own draft, stop and
re-derive it — never ask for confirmation of a change you cannot describe.

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
offer it as a separate Suggestion Block item (output-cards.md) before preview;
do not merge it into the draft unless the user accepts it.

If notification copy does change, the Changes slot must show the old and new text or
payload. A count-only array line such as `| Actions | 7 items | 8 items |` is not enough
when an existing notification title, message, or notification payload changed.

### Fixed update-preview shape

Render a terminal-friendly preview in this exact order, nothing extra — users
should learn one shape and always recognize it:

1. Preview-summary slot: 📝 title line (localized preview label + item type +
   name), then the plain-language behavior narrative (one to three sentences;
   it may run longer when per-entry coverage requires it).
2. Changes slot: your localized header row + `|---|---|---|`, then the
   CLI diff rows, verbatim.
3. Pre-write-check/impact slot: one or two short lines.
4. Save-status slot: explicitly say that nothing has been saved yet (the ⚠️ line).
5. Options slot: one natural question — save it, show the details, or leave it.

This is the Preview Card of `../ha-nova/output-cards.md` → Cards; the
Result Card and Delete Card there frame the post-write and delete sides.

Do **not** re-describe the whole automation (entities, every trigger/action) —
the diff already states what changes. Keep it scannable.

### Full config: always offered, never forced

A non-technical user reads the Changes slot, not raw YAML. So:

- **update**: lead with the Changes slot; close with the save question.
- **create**: a compact human summary, the pre-write check, a not-saved-yet
  line, and the save question (a selectable menu where the client offers one).

Shape (the table rows are exactly what the CLI produced this turn — never
invent, shorten, pre-fill, or copy these from an example). The labels below are
semantic placeholders; localize them before showing the user:

```
📝 <localized preview label>: <type> "<name>"
<behavior narrative, one to three sentences>

| <localized field label> | <localized before label> | <localized after label> |
|---|---|---|
<paste the CLI diff rows here, unchanged>

<pre-write check / impact line>
⚠️  <localized: nothing saved yet>
<localized question: save it? I can also show the details.>
```

## Verification Honesty (post-write wording)

Post-write checks prove persistence, not behavior. What they establish: the
write was accepted, the read-back matches, the domain reloaded, the runtime
entity exists. What they do NOT establish: that conditions carry the intended
meaning, that every branch is reachable, that timing assumptions hold, or that
the physical action succeeds.

Wording rules for the result and the collapsed no-findings line:

- Never a bare "verified"/"works now". Name the proven scope instead — the
  localized equivalent of: "Saved and checked: it is stored and Home
  Assistant loaded it. Runtime behavior was not exercised."
- Where a real run is meaningful, offer it as an explicit optional step: a
  manual trigger via `ha-nova:service-call` (its own preview + confirmation —
  it may actuate real devices; never run it unrequested). For automations and
  scripts, structure that offer as the Test Plan Card in
  `../ha-nova/test-run.md` (write flow → Phase 5); helper writes keep
  this plain single offer. If the user
  declines, the uncertainty stays in the final wording — do not let the
  closing sentence sound more conclusive than the checks were.

## Time-Window Evidence (window-derived claims)

A configured window is a MAXIMUM, not proof of coverage (#596). Any claim
derived from a bounded time window — statistics helpers (`max_age`),
`history_stats`, history/statistics queries, calibration evidence —
distinguishes:

- the configured maximum window vs the actual oldest-to-newest sample span,
- sample count and density when available,
- source validity and `unknown`/`unavailable` states in the source series,
- explicit coverage attributes when the platform exposes them (the statistics
  platform exposes `age_coverage_ratio` and `source_value_valid`),
- a retained last sample (`keep_last_sample`) vs evidence spanning the window
  — one persisted value proves persistence of that value, never stability
  across the full window.

Read coverage and source-validity attributes when present BEFORE describing a
result as covering "the last N hours"; when coverage is partial, qualify the
claim — "based on samples covering X% of the window" (semantic slot, localized
at runtime per output-rules.md). When the source history is sparse,
discontinuous, or Recorder data is unavailable, the observed evidence span —
not the configured window — bounds the claim. After a helper write, partial
coverage is an advisory in the result, never a verification failure: the
helper is created and valid.

## Multi-Target Changes (one logical change, several items)

When one logical change spans multiple automations/scripts/helpers, per-target
previews alone hide the whole picture. Before the FIRST per-target preview:

1. Present the change plan: every target, what changes in each, the intended
   combined behavior, and the apply order (dependencies first). The plan must
   satisfy the active user decisions (context skill → Decision Memory).
2. State revert coverage honestly BEFORE starting: update-revert keeps the
   last 5 updated targets (one snapshot per target) — a logical change with
   more update targets than that loses auto-revert for the oldest ones; say
   so. For a broad or hard-to-reconstruct change, offer a safety backup via
   `ha-nova:backup` first (proportionality rules apply).
3. Get plan-level consent, then run the normal per-target flow (each target
   still gets its own preview + confirmation — the plan does not replace them).
   Exception: where the owning skill declares grouped support and the workset
   is non-destructive with at most 10 operations, the per-target previews
   render without intermediate menus and ONE final action block confirms the
   whole set (`../ha-nova/grouped-change-set.md`).
4. Close with a combined summary: all targets applied, the still-revertible
   targets named (`ha-nova snapshot show --list`), and the
   verification-honesty wording for the whole change.

If a mid-sequence target fails or is cancelled, stop and show which targets
are already applied — never continue silently into a half-applied state.

This section covers multi-target updates. Deleting a reviewed multi-item
workset routes through `../ha-nova/batch-safety.md`: one immutable
manifest, one one-time code (automations/scripts: one CLI code per item), per-target impact checks and
verification retained, one resource family per manifest.

## Update-Revert (durable, identity-preserving)

Scope: **update only**. A create is undone by deleting the new item through the
normal HA NOVA delete flow; that delete still requires a delete preview, one-time
code, and absence verification, even when the item was created earlier in the same session.
Do not call this `revert`, and do not imply that manual deletion or a full Home Assistant Backup restore is the only cleanup path.
A delete has no HA NOVA `revert`; a snapshot-covered delete restores from its
auto config snapshot (`../ha-nova/config-snapshots.md`) — that is the
first rollback path. When no config snapshot was captured, rollback requires
restoring a suitable existing Home Assistant Backup (Settings > System >
Backups), or recreating the item.

Checkpoints: the store keeps the last 5 updated targets per server, one step
back each. Automation/script updates (reverts included) are checkpointed by
`ha-nova apply` itself — repeat the structured receipt it returns as `undo` — a `replaced` receipt means the target's PREVIOUS checkpoint is gone and only the newest update stays revertible; name evicted targets as no longer revertible. Helper storage updates capture their checkpoint by hand:
`../ha-nova/update-revert.md` → Manual capture.

After reporting the update, offer a localized affordance that names the point-in-time
rollback option in the same breath — never a bare "undo":

> to undo this change, just say so; for an older state, restore a suitable Home Assistant Backup.

Execution lives in `../ha-nova/update-revert.md`; load it whenever the user
asks to revert, undo, or restore a verified update (any of those intents — not
only the literal word `revert`).

## Safety-Mechanism Availability by Skill

Never imply a recovery mechanism a skill does not have; when only Backups
remain, say so before the write. The per-skill matrix (diff, update-revert,
fallback recovery path) and the full-backup offer rules live in
`../ha-nova/recovery-coverage.md` — read it before promising or offering a
recovery path outside this skill's own (`write`: `plan write` diff,
`plan revert`, config snapshot before delete, HA Backups).
