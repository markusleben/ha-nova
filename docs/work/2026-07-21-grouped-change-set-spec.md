# Grouped Change Set Spec (#391)

Status: approved plan, Batch 2 of the 2026-07-21 issue wave
Date: 2026-07-21
Trigger: issue #391 — one logical task spanning several non-destructive mutations
repeats a full action menu after every preview; the user cannot tell which reply is
the one decision that authorizes the whole prepared set.

## Contract (new file `skills/ha-nova/grouped-change-set.md`)

Non-destructive sibling of `batch-safety.md` (mirror its section skeleton):

- **Scope/opt-in:** group ONLY operations belonging to one user-requested logical
  task; capability matrix lives inside this contract file (batch-safety pattern —
  "no" skills need zero edits); initially opted-in skills decided here: `write`,
  `helper`, `scene`, `organize`, `service-call` (their non-destructive ops are the
  real multi-op tasks); everything else "no" in v1.
- **Cap:** max 10 operations; larger requests split into explicit separate groups.
- **Mixed families allowed** when part of the same logical task — explicitly
  distinguished from the destructive batch invariant "one family per manifest"
  (which is test-pinned and unchanged).
- **Destructive exclusion:** any operation requiring a confirmation code is rejected
  from the group and keeps its own flow; a task mixing both runs the grouped set
  first, then each destructive op separately.
- **Preview protocol:** every operation gets its full canonical preview (Cards
  contract, unchanged); intermediate previews carry NO action menu; exactly one
  final action block offers the canonical keywords `apply · show yaml · cancel`
  (`show yaml` re-renders full details per operation). Natural-language apply
  confirmation, bound to the exact displayed set (Active Preview Confirmation).
- **Execution:** sequential, fail-fast; fresh drift check immediately before each
  operation (write-safety drift-check mechanism); stop on first failure.
- **Ledger:** per-operation states planned / applied / skipped / failed; partial
  completion reported explicitly; never claim atomicity or rollback guarantees;
  recovery points at the per-op mechanisms (update-revert snapshots, delete flows).

## Edits (beyond the new file)

- `skills/ha-nova/SKILL.md`: Confirmation Tiers (+ grouped tier line) AND the
  line-97 sentence "Multi-target confirmation is valid only where the owning skill
  supports multi-target writes (destructive batches: batch-safety.md)" — extend for
  grouped sets (pin: `ha-safety-contract.test.ts:33` must be updated in lockstep).
- `skills/ha-nova/output-rules.md`: carve-out from the per-op options-block rule
  (lines 33-34) for grouped intermediate previews + new coverage-matrix row
  "Grouped change set (non-destructive)". `output-design-contract.test.ts`
  coverage-matrix pin (:187-215) updated in the same PR.
- `skills/ha-nova/batch-safety.md`: redirect the grouped-manifest pointers
  (line 170 "separate non-destructive tier" and the exclusions note, both pinned in
  `batch-safety-contract.test.ts:134`) to the new contract file.
- `skills/ha-nova/write-safety.md`: Multi-Target Changes cross-links the grouped
  contract as the confirmation vehicle for non-destructive worksets ≤10.
- `skills/service-call/SKILL.md`: promote the Guardrails grouped-manifest one-liner
  (line 223) to a reference to the contract.
- Opt-in reference line in the 5 opted-in skills' SKILL.md; budget ratchets where
  tripped (documented).

## Tests

- NEW file `tests/skills/grouped-change-set-contract.test.ts` (mirrors
  batch-safety-contract: opt-in list, matrix rows, caps, ledger, never-atomic,
  keyword canon) — MUST be registered in `scripts/test/safe-core-files.json`.
- Scenarios per issue acceptance: success, drift abort, partial failure, mixed
  families, ten-operation limit.
- Updated pins: `ha-safety-contract.test.ts` (line-97 sentence),
  `output-design-contract.test.ts` (coverage matrix + carve-out), `batch-safety-contract.test.ts` (redirect sentence).
- **e2e live analyzer: NO for v1** (decision amended during implementation): the
  write-review live suite has no grouped-flow scenario — an analyzer that never
  runs is false comfort, and a new live multi-op scenario is out of proportion
  for v1 (KISS). Follow-up candidate once a grouped scenario exists in the live
  suite. Contract tests carry the coverage.

## Non-goals

- No change to destructive batch semantics, codes, or caps.
- No atomicity/rollback machinery; recovery stays per-operation.
- No relay change.

## Verification

- `npx vitest run` green incl. the new suite (verify it runs via safe-core-files).
- Live: one real grouped task (2-3 non-destructive ops on expendable test entities)
  through the write flow with exactly one final confirmation; drift + partial-failure
  covered by contract tests.
- Side-work: safety.md guarantee row, skill-architecture.md inventory,
  0.20.0-release-body.md claim, breadcrumbs/choices.
