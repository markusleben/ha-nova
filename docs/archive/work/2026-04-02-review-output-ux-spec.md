# Review Output UX Spec

Date: 2026-04-02

## Goal

Make HA NOVA review output understandable for end users by removing internal rule-code language from user-facing review text.

## Scope

- standalone single-target review wording
- write pre-write verdict wording
- post-write review wording
- live harness prompts and scenario contracts that currently force code-shaped output

## Non-Goals

- no check-logic changes
- no severity changes
- no bulk-review redesign
- no release/workflow/policy changes

## Decisions

- Keep `skills/ha-nova/SKILL.md` as the global wording policy source.
- Keep `skills/review/SKILL.md` as the single-target review wording SSOT.
- Do not add a new shared output-style file.
- Keep the stable 8-section standalone review shape, including `Instant help`.
- Keep bulk review structurally unchanged.
- Keep post-write structurally compact: `Findings`, `Collision check`, `Advisory`.
- Allow exact pinned headings / machine markers for maintainer machine-check runs only.
- Do not allow internal rule codes in normal user-facing prose, clean states, or pre-write verdicts.

## User-Facing Wording Contract

- Findings use:
  - severity emoji + short title
  - `Why: ...`
  - `Fix: ...`
- Clean and empty states are generic and section-specific.
- Pre-write safe verdict: localized equivalent of `Pre-write check: no issues worth flagging before save.`
- Pre-write warned verdict: localized equivalent of `Pre-write check: this draft may not behave as intended.`
- Post-write mode never emits `Questions to consider`, `Suggestions`, or `Instant help`.

## Test Intent

- Replace scenario strings such as `No R-19 risk detected` and `PREWRITE CHECK: R-19 detected`.
- Add negative guards for rule-code leakage in user-facing review/write-review scenarios.
- Align standalone live review scenarios to the authoritative 8-section shape.
