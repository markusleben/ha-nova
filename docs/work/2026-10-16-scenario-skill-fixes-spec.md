# Skill text fixes from the 2026-10-16 agent scenario suite — Spec

Status: active

## Problem

End-to-end agent scenarios on the local test bed showed seven skill-text defects:

1. After a clarifying question, the user's answer was taken as consent: `plan write` and `apply` ran in one turn without a preview reply (safety).
2. Scene create resolved entities from the raw registry, missing aliases.
3. "Turn off all the lights" took a pre-question plus the preview (two rounds).
4. Helper default suggestions cost a round-trip before the preview.
5. A typed-tier result ("guest mode on") did not restate the consequence the code was for (a door unlocks).
6. Repo-style `skills/<x>/…` references failed on the first read in the installed tree layout.
7. Review rated an in-order `variables` dependency as high-risk; Home Assistant renders a mapping top-down.

## Change

- Safety Core bullet 3 (all mutation skills, word-neutral) and the context skill's Active Preview Confirmation: anything before the preview — answers to questions included — never confirms it; only a reply to the preview does. Write Phase 2: every new or changed plan's preview ends the turn (Phase 3 duration re-plans excepted).
- Scene create: `ha-nova find` (aliases) and `ha-nova get` for states.
- Service call: whole-domain requests enumerate with `ha-nova find --domain <d> --state <s> --limit 200` and go through one `plan call` preview (never the ordinary fast path, which runs up to 10 explicit ids at once); a typed `apply` result restates the effect the code was for and whether it was verified.
- Helper: value defaults fill only fields the user left unset and ride the preview card; `skip`/numbers drop them and re-preview; feature offers stay opt-in and numbered apart.
- Session bootstrap: one path rule (a relative `skills/<x>/<file>` on disk = `../<x>/<file>` from the citing file; absolute paths stay). The context skill cites its index as a sibling file.
- R-18: MEDIUM only when a variable is used above the sibling it reads; B above A is no finding. The "storage reorders keys" premise is removed from write, checks, template guidelines, and skill-architecture.

No CLI tier or confirmation rule changes. Word budgets held by trimming duplicates.

## Verification

Skill contract tests (new pins in `tests/skills/agent-scenario-regressions-contract.test.ts`), typecheck, `scripts/check-docs.sh`, and a re-run of the affected scenarios on the test bed.
