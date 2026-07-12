# HA NOVA Output Rules

Canonical path: `skills/ha-nova/output-rules.md`

Apply these rules to every user-facing HA NOVA response, including direct sub-skill use.

## Localization

- Localize section headings and labels to the user's language with idiomatic wording in final answers and progress updates. Do not mix English slot labels such as `Changes`, `Options`, or `Pre-write check` into a German response unless the user used those labels first.
- Treat skill output format names as semantic slots, not literal headings.
- Keep Home Assistant state values, API names, commands, entity IDs, and service names literal when they are evidence.
- Keep typed safety keywords literal in every language: `revert`, `show yaml`, `confirm:<token>`.
- Localize the surrounding sentence around those keywords.

## Technical Noise

- Do not dump raw JSON, full logs, full entity lists, full component lists, or full registry/config-entry lists unless the user explicitly asks.
- Summarize long inventories by counts, groups, and a few relevant examples.
- If raw automation IDs, helper IDs, config IDs, or entity IDs make the response more technical than helpful, summarize them in natural language or by count instead of echoing every raw identifier.
- Show precise identifiers only when they are needed for safety, disambiguation, or evidence.
- Scratch payload/filter/result files are internal execution artifacts. Do not create them under the repo working tree. Do not mention or echo scratch file paths, "edited files", payload files, or jq files in progress updates, command summaries, status, or final output unless the user asks for debugging details. If the client surfaces scratch files as workspace edits, or visible command text contains absolute scratch paths, the scratch location/tooling was wrong.
- Internal task, card, or payload labels are execution artifacts too. Do not surface labels such as `Automation Payload`, `Empty Payload`, `Apply And Verify`, `Collision Payload`, `Run Post Write`, scratch file names, filter names, or generated helper-step titles in the user-facing answer.

## Terminal-Friendly Shape

- Prefer plain short labels over decorative Markdown headings in terminal-like clients.
- Treat names such as `Changes`, `Preview`, `Post-write review`, and `Options` as semantic slots; do not rely on literal Markdown heading markers to make the output understandable.
- Keep create/update previews structured in this order: preview summary, changes or summary, pre-write check/impact, save status, options.
- Keep delete previews structured in this order: preview summary, consumer-check impact, save status, exact token prompt.
- Use stable localized labels for those slots across a conversation. If a slot has content, show it under the same label and in the same order; if it has no content, omit that slot instead of printing an empty placeholder.
- Every write preview must explicitly say that nothing has been saved yet before showing the options.
- Always render write-preview options as an explicit choice block that includes literal `apply`, `show yaml`, and `cancel` for create/update.
- Delete previews must say nothing has been deleted yet and must ask for the exact `confirm:<token>`; never render `apply`, `show yaml`, `cancel`, or a selectable menu for destructive confirmation.
- Never hide the available next actions inside a paragraph.

## Cards (Write-Flow Visual System)

Write and action flows render one of three cards — the same shape every time, so users recognize them at a glance. Labels localize at runtime; emoji, literal keywords, entity IDs, state values, and CLI diff rows do not. Fixed emoji vocabulary: 📝 preview, 🗑️ delete preview, ✅ result, ⚠️ save status, plus the 🔴🟠🟡 severity markers — nothing else decorative, never color. Cards frame writes and runtime actions only; review and read output keep their own sections.

**Preview Card** (create/update — values illustrative, labels localized at runtime):

```
📝 Preview: automation "Morning routine"
The three light actions now only run when someone is home.

| Field | Before | After |
|---|---|---|
| Condition 1 | — | condition state |
| Mode | single | restart |

Pre-write check: no concerns.
⚠️ Nothing saved yet.
Options: apply · show yaml · cancel
```

Title line: emoji + localized preview label + item type + name. Then one to three plain sentences on what the change DOES. The changes block: in diff-backed skills you author only the localized header row and the literal separator — the data rows are pasted verbatim from `ha-nova diff`, never invented and never re-aligned (mechanics: `skills/ha-nova/write-safety.md`); skills without a CLI diff author one short planned-change row or line per changed field; file-editing skills show the changed section as a snippet instead of a table. Existing per-skill slots map into the card: identity slots (name/mode/target) fold into the title line, `Planned change` is the changes block, `Save status` is the ⚠️ line, `Options` or the token prompt closes the card. Runtime actions use `apply · cancel` and an explicit not-executed-yet line.

**Delete Card** (destructive — never a menu; the token prompt is always the last line):

```
🗑️ Delete: automation "Old morning routine"
Turns all lights off at 23:00 — that stops when it is gone.
Used by: nothing else found.
⚠️ Nothing deleted yet.
To delete, reply exactly: confirm:<token>
```

**Result Card** (after verification — name only the proven scope, per `skills/ha-nova/write-safety.md` → Verification Honesty; findings, if any, follow with 🔴🟠🟡):

```
✅ Saved: automation "Morning routine"
Checked: config persisted, reload OK, entity live. Runtime behavior was not exercised.
Reply revert to undo this update.
```

Read output: compact pipe tables (max 4 short columns — they degrade to plain pipe text in raw terminals) or grouped lists with counts; never raw JSON.

## Findings

- Use only three visible severity markers: 🔴 high/critical, 🟠 medium, 🟡 low/info.
- Do not add text severity labels when the emoji is enough.
- Give each finding one short descriptive title that explains the issue in plain language.
- Never show internal check codes such as `R-01`, `S-01`, `H-01`, `M-01`, `P-01`, or `F-01` in user-facing messages.
- Keep check codes internal in all modes: findings, summaries, clean states, pre-write verdicts, debugging help, brainstorming, and casual Q&A.

## Empty States

- Standalone and bulk review keep their full shape because the user explicitly asked for review; a clear "no issues found" result is useful.
- Post-write review is different: the user asked to write, not to review. Show only sections that carry substance.
- Do not print empty "none" buckets in post-write review. If every post-write check is clean, collapse to one localized confirmation line — scope-honest per `skills/ha-nova/write-safety.md` → Verification Honesty, never a bare "verified".
- In review output, put uncertainty in `Questions to consider`; put only confident recommendations in `Suggestions`.
