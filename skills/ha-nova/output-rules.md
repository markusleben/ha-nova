# HA NOVA Output Rules

Canonical path: `skills/ha-nova/output-rules.md`

Apply these rules to every user-facing HA NOVA response, including direct sub-skill use.

## Localization

- Localize section headings and labels to the user's language with idiomatic wording in final answers and progress updates. Do not mix English slot labels such as `Changes`, `Options`, or `Pre-write check` into a German response unless the user used those labels first.
- Treat skill output format names as semantic slots, not literal headings.
- Keep Home Assistant state values, API names, commands, entity IDs, and service names literal when they are evidence.
- Keep typed safety keywords literal in every language: `revert`, `show yaml`, `confirm:<token>`.
- Localize the surrounding sentence around those keywords.
- The user-facing name for the typed destructive keyword is the "confirmation code" (localized to the user's language). Never call it a "token" in user-facing output — "token" is internal spec language, and it also collides with the unrelated access-token concept users know from onboarding.

## Technical Noise

- Do not dump raw JSON, full logs, full entity lists, full component lists, or full registry/config-entry lists unless the user explicitly asks.
- Summarize long inventories by counts, groups, and a few relevant examples.
- If raw automation IDs, helper IDs, config IDs, or entity IDs make the response more technical than helpful, summarize them in natural language or by count instead of echoing every raw identifier.
- Show precise identifiers only when they are needed for safety, disambiguation, or evidence.
- Scratch payload/filter/result files are internal execution artifacts. Do not create them under the repo working tree. Do not mention or echo scratch file paths, "edited files", payload files, or jq files in progress updates, command summaries, status, or final output unless the user asks for debugging details. If the client surfaces scratch files as workspace edits, or visible command text contains absolute scratch paths, the scratch location/tooling was wrong.
- Internal task, card, or payload labels are execution artifacts too. Do not surface labels such as `Automation Payload`, `Empty Payload`, `Apply And Verify`, `Collision Payload`, `Run Post Write`, scratch file names, filter names, or generated helper-step titles in the user-facing answer.

## Progressive Detail

For any truncated listing — capped groups, top-N rows, bounded chunks — the
output MUST carry the total count, the shown count, the omitted count, and a
precise way to request the full set (the exact follow-up phrase or mode, e.g.
"say 'full report' or 'show all entities in MQTT'"). A detail follow-up
widens only the DETAIL dimension — identity form stays in the active
privacy/identity mode until the user explicitly switches it. Never end with a bare
"N more", "other groups", or similar. When an active mode hides identities or
detail, name the mode and how to switch. Follow-ups are fresh live reads —
say that results may have changed since the previous report.

## Terminal-Friendly Shape

- Prefer plain short labels over decorative Markdown headings in terminal-like clients.
- Treat names such as `Changes`, `Preview`, `Post-write review`, and `Options` as semantic slots; do not rely on literal Markdown heading markers to make the output understandable.
- Keep create/update previews structured in this order: preview summary, changes or summary, pre-write check/impact, save status, options.
- Keep delete previews structured in this order: preview summary, consumer-check impact, save status, exact confirmation-code prompt.
- Use stable localized labels for those slots across a conversation. If a slot has content, show it under the same label and in the same order; if it has no content, omit that slot instead of printing an empty placeholder.
- Every write preview must explicitly say that nothing has been saved yet before showing the options.
- Always render write-preview options as an explicit choice block that includes literal `apply`, `show yaml`, and `cancel` for create/update. Sole exception: intermediate previews inside a grouped change set (`skills/ha-nova/grouped-change-set.md`) carry no options block — exactly one final action block closes the group with the same literal keywords.
- Delete previews must say nothing has been deleted yet and must ask for the exact `confirm:<token>`; never render `apply`, `show yaml`, `cancel`, or a selectable menu for destructive confirmation.
- Never hide the available next actions inside a paragraph.

## Cards (Write-Flow Visual System)

Write and action flows render one of four cards — the same shape every time, so users recognize them at a glance. Labels localize at runtime; emoji, literal keywords, entity IDs, state values, and CLI diff rows do not. Fixed emoji vocabulary: 📝 preview (write previews and the test-plan offer), 🗑️ delete preview, ✅ result, ⚠️ save status, 💡 suggestion, plus the 🔴🟠🟡 severity markers — nothing else decorative, never color. Cards frame writes and runtime actions only; review and read output keep their own sections. Emoji that end in a variation selector (🗑️, ⚠️) take two spaces before the following text — many terminals render them double-width over the next column and visually swallow a single space; the other card emoji keep one space.

**Preview Card** (create/update — values illustrative, labels localized at runtime):

```
📝 Preview: automation "Morning routine"
The three light actions now only run when someone is home.

| Field | Before | After |
|---|---|---|
| Condition 1 | — | condition state |
| Mode | single | restart |

Pre-write check: no concerns.
⚠️  Nothing saved yet.
Options: apply · show yaml · cancel
```

Title line: emoji + localized preview label + item type + name. Then one to three plain sentences on what the change DOES. They must cover every collection the diff touches — each added, removed, replaced, or modified entry described by its effect, never only by count or type name; grouped entries may share a sentence, and coverage beats the three-sentence guideline. The changes block: in diff-backed skills you author only the localized header row and the literal separator — the data rows are pasted verbatim from `ha-nova diff`, never invented and never re-aligned (mechanics: `skills/ha-nova/write-safety.md`); skills without a CLI diff author one short planned-change row or line per changed field; file-editing skills show the changed section as a snippet instead of a table. Existing per-skill slots map into the card: identity slots (name/mode/target) fold into the title line, `Planned change` is the changes block, `Save status` is the ⚠️ line, `Options` or the confirmation-code prompt closes the card. Runtime actions use `apply · cancel` and an explicit not-executed-yet line.

**Delete Card** (destructive — never a menu; the confirmation-code prompt is always the last line):

```
🗑️  Delete: automation "Old morning routine"
Turns all lights off at 23:00 — that stops when it is gone.
Used by: nothing else found.
⚠️  Nothing deleted yet.
To delete, reply exactly: confirm:<token>
```

Destructive batch previews and results render the Batch Cards defined in
`skills/ha-nova/batch-safety.md` — same emoji vocabulary and rules.

**Result Card** (after verification — name only the proven scope, per `skills/ha-nova/write-safety.md` → Verification Honesty; findings, if any, follow with 🔴🟠🟡):

```
✅ Saved: automation "Morning routine"
Checked: config persisted, reload OK, entity live. Runtime behavior was not exercised.
Reply revert to undo this update.
```

**Test Plan Card** (test offer after create/update — feasibility logic, option
selection, and post-run follow-up live in `skills/ha-nova/test-run.md`; menu
mechanics: context skill → Interactive Choices):

```
📝 Test: automation.morning_lights (saved — runtime not exercised yet)
1 (recommended) — Real test: what runs, what it proves, what it switches
2 — Actions only: …
skip — test later
```

Recommended option first and marked; at most 3 options plus `skip`; the option
choice is the single confirmation bound to that exact card.

Card coverage — every mutation and restore flow maps onto these cards; no
write flow renders outside this system:

| Flow | Cards |
|---|---|
| Create / update (any supported family) | Preview Card → Result Card |
| Delete / destructive operation (typed confirmation code) | Delete Card → Result Card |
| Natural-confirmation removals (e.g. snapshot prune, todo item removes) | Preview Card → Result Card |
| Batch mutation (manifest-gated) | Batch Cards (`skills/ha-nova/batch-safety.md`) |
| Grouped change set (non-destructive, one logical task) | Preview Cards without options → one final action block → Result Card (`skills/ha-nova/grouped-change-set.md`) |
| Snapshot restore (`skills/ha-nova/config-snapshots.md`) | Preview Card → Result Card |
| Post-write test offer | Test Plan Card |
| Runtime action (service call, experimental write) | Preview Card (`apply · cancel`; high-consequence actions escalate to the typed confirmation code — context skill → Confirmation Tiers) → Result Card |

## Report Shape (Read & Analysis Results)

Every read or analysis answer follows one shape: lead with the answer in one or
two sentences, then a grouped body (List Frame tables or grouped lists),
optional evidence per context skill → Claim-Evidence Binding, and a closing
`Next step` slot — omit it when nothing is actionable. Canonical label:
`Next step` (localized; never `Next Step`).

```
The dryer used 42 kWh last month — about 18 % of total consumption.

| Week | kWh |
|---|---|
| Jun 1–7 | 11.2 |

Next step: add the dryer plug to the Energy dashboard for per-cycle tracking.
```

Diagnosis specialization (diagnose, review trace analysis): the lead is the
root cause (or ranked hypotheses); the body is the evidence chain — trace step,
log lines, state sequence, each tied to its source and timestamp; `Next step`
is the recommended fix plus the owning-skill handoff.

Skills with their own ordered slot lists (health, history, calendar, energy,
backup, updates, maintenance, todo, scene) keep those slots — they are Report
Shape specializations: the status/summary slot is the answer-first lead, and
`Next step` closes.

## List Frame (Inventories & Discovery)

Any list of items — entities, configs, helpers, backups, updates, to-do items,
media browse results: a count line first ("12 found, showing first 10,
2 omitted — say 'show all' for the rest; fresh read, results may have
changed" — whenever rows are truncated, carry the Progressive Detail
fields: total, shown, omitted, the exact follow-up, and the fresh-read
notice), then compact pipe tables
(max 4 short columns — they degrade to plain pipe text in raw terminals) or
grouped lists with counts; never raw JSON. Canonical column order: ID, Name,
one or two domain-specific columns (Type, State, Reason), Area. Domains keep
their own columns — the frame (count line, column order, stated cap) is what
is shared.

```
23 automations found, showing first 10, 13 omitted — say "show all automations" for the rest (fresh read — results may have changed).

| Entity ID | Name | Area |
|---|---|---|
| automation.morning | Morning routine | Hallway |
```

## Suggestion Block

One shape for every improvement offer: write-flow enhancement suggestions,
helper suggested defaults, review Suggestions items, and separate
notification-copy offers. 💡 header line (single space after 💡), numbered
items. Caps: unsolicited improvement suggestions max 2, smallest intervention
first, none when the requested solution is already complete
(`skills/ha-nova/smallest-solution.md`); value-default suggestions (filling
fields of the requested item, e.g. helper defaults) max 4.
Item shape: short title + what it does + why it helps; value
suggestions (helper defaults) may use the value assignment as the title and
drop the benefit when it is obvious. Menu mechanics follow context skill →
Interactive Choices; skip/decline is always valid; omit the block entirely
when there is nothing to offer; never post-write. Numeric acceptance resolves
per context skill → Interactive Choices: name the accepted items before
applying — a suggestion choice edits the pending draft where one exists, and
in a standalone review it only hands the item to the owning write skill's
normal preview flow; it never runs anything. Review's Suggestions section
keeps its plain sectioned header — review output is sectioned, not card-framed
— but its items follow this item shape.

```
💡 Suggestions for "Morning routine":
1. Presence condition — runs only when someone is home · avoids empty-house runs
2. Restart mode — re-trigger restarts the timer · prevents overlapping runs
Accept numbers (e.g. "1 and 2"), or "skip".
```

## Findings

- Use only three visible severity markers: 🔴 high/critical, 🟠 medium, 🟡 low/info.
- Do not add redundant text severity labels; pairing the emoji with one short status word for accessibility (as the health report mandates) is fine.
- Give each finding one short descriptive title that explains the issue in plain language.
- Never show internal check codes such as `R-01`, `S-01`, `H-01`, `M-01`, `P-01`, or `F-01` in user-facing messages.
- Keep check codes internal in all modes: findings, summaries, clean states, pre-write verdicts, debugging help, brainstorming, and casual Q&A.

## Empty States

- Standalone and bulk review keep their full shape because the user explicitly asked for review; a clear "no issues found" result is useful.
- Post-write review is different: the user asked to write, not to review. Show only sections that carry substance.
- Do not print empty "none" buckets in post-write review. If every post-write check is clean, collapse to one localized confirmation line — scope-honest per `skills/ha-nova/write-safety.md` → Verification Honesty, never a bare "verified".
- In review output, put uncertainty in `Questions to consider`; put only confident recommendations in `Suggestions`.
