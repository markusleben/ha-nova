# Live E2E Sequence Gate Fix (Pre-Create Read)

## Goal
- Remove false negatives in `scripts/e2e/codex-ha-nova-live-skill-e2e.sh` when the assistant performs a valid pre-create existence `GET` before the first `POST`.

## Scope (MVP)
- Keep all existing evidence guards.
- Change only the sequence gate to allow optional leading read tokens.
- Update the contract test that asserts the regex literal.

## Non-Goals
- No behavior changes to relay evidence extraction.
- No redesign of CRUD evidence model.
- No broad refactor.

## Design
- Current accepted sequence: `^P+G+P+G+D+V+$`.
- New accepted sequence: `^[GV]?P+G+P+G+D+V+$`.
  - `G` = successful `GET 200`.
  - `V` = successful `GET 404` ("verify absent"/not found).
  - Leading `[GV]?` models at most one optional pre-create read token.

## Acceptance Criteria
- Harness accepts flows with exactly one optional leading `G` or `V` before `PGPGDV`.
- Existing `PGPGDV` flow remains accepted.
- `tests/e2e/codex-skill-live-contract.test.ts` passes with updated regex anchor.

## Verification
- `npm test -- tests/e2e/codex-skill-live-contract.test.ts`
- `npm run typecheck`
