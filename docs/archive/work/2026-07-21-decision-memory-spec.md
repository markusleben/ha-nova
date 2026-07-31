# Decision-Memory Contract Spec (#395)

Status: merged — #407
Date: 2026-07-21
Trigger: issue #395 — long multi-step tasks accumulate explicit requirements and
rejected alternatives; later plans/previews can silently reintroduce a rejected
condition or drop an earlier hard requirement while looking locally valid.

## Placement decision (spec question from plan review)

**Section in `skills/ha-nova/SKILL.md`** ("Decision Memory", placed after
Claim-Evidence Binding), NOT a separate reference file. Rationale: the contract
gates EVERY plan/preview/mutation draft — hot-path like Claim-Evidence Binding and
User-Assisted Readiness, which are sections; the ruleset is compact (no cards, no
matrices); the context skill is budget-exempt; KISS (repo prefers fewer files).
Consequence: no skill-architecture inventory entry needed.

## Contract (new `## Decision Memory` section, context skill)

- Track through a multi-step task, internally and per conversation:
  **hard requirements** (user-stated constraints), **accepted choices**,
  **rejected alternatives**, **unresolved assumptions** — as four distinct kinds,
  never merged.
- **Last-explicit-choice-wins:** a newer explicit user choice replaces exactly the
  older choice it contradicts; unrelated earlier constraints stay active. A
  replaced decision is not a conflict — two still-active contradicting
  requirements are.
- **Validation gate:** before presenting any plan, preview, or mutation request,
  check it against the active set. On conflict: block that output, explain the
  conflict in plain language (which requirement, which part of the draft), and ask
  — never silently pick a side, never quietly drop the older constraint.
- Unresolved assumptions surface in the preview (Uncertain tone per Claim-Evidence)
  instead of hardening into silent facts.
- Carries through Multi-Target Changes and grouped change sets (#391): the group
  preview must satisfy the active set as a whole.
- No persistent store: memory lives in the conversation only. No internal
  requirement identifiers in user-facing output (output-rules → Technical Noise).

## Edits

- `skills/ha-nova/SKILL.md`: the new section (+ one line in Safety Baseline
  referencing it as a write-path gate).
- `skills/ha-nova/write-safety.md`: one line in Multi-Target Changes (plan step 1
  validates against Decision Memory).
- `skills/ha-nova/grouped-change-set.md`: one line in Preview Protocol (group
  preview validates against Decision Memory).

## Tests (extend existing suite — no new file)

- `tests/skills/ha-safety-contract.test.ts` (owns the Safety-Baseline surface):
  pin the four kinds as distinct, last-explicit-choice-wins, replaced-vs-conflict
  distinction, the block-and-explain rule, no-persistent-store, no internal IDs
  in output, and the multi-turn scenario coverage (superseded / retained /
  conflict / unresolved) via the section's fixture examples.
- The section includes a compact worked multi-turn example (correction arrives
  several turns later) that the pins anchor — serving the issue's regression-
  fixture requirement in the skill layer's medium (prose contract + pins).

## Non-goals

- No persistent decision database, no cross-session memory, no client storage.
- No mutation-tool-side conflict resolution (the gate fires before the write).
- No live e2e analyzer (multi-turn constraint tracking is not exercisable in the
  current live scenarios; contract pins carry coverage).

## Verification

- `npx vitest run tests/skills/` green; pins fail on section removal.
- Side-work: safety.md guarantee row (verbatim test title), release-body claim,
  breadcrumbs/choices (Batch-2 close-out).
