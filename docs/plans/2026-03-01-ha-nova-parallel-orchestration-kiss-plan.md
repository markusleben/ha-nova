# 2026-03-01 HA NOVA Parallel Orchestration (KISS/MVP)

## Goal

Make HA NOVA execute all independent work in parallel (when supported), keep write safety strict, and keep responses deterministic and compact.

## Constraints

- KISS first: no runtime architecture rewrite.
- Skill-contract and tests only.
- No enterprise orchestration layer.
- Preserve `preview -> confirm:<token> -> apply -> verify`.

## Brainstorming Outcome

1. Biggest pain points in real runs:
   - missing/unstable parallel fan-out behavior
   - shell quoting mistakes during env load
   - oversized discovery outputs
   - ad-hoc probing loops
2. Minimal solution:
   - hard `MUST` parallel rules for independent steps
   - deterministic sequential fallback when unsupported
   - explicit shell quoting contract
   - shortlist-first discovery output policy

## Execution Plan

### Task 1: Orchestrator Contract

Files:
- `skills/ha-nova.md`
- `.agents/skills/ha-nova/SKILL.md`

Changes:
- switch from read-only parallel guidance to generic independent-task parallel rule
- define fan-out/fan-in orchestration
- define deterministic fallback
- add shell-quoting reliability section (shell-dependent, bash-compatible)
- keep non-parallel safety list for write phases

### Task 2: Entity Discovery Contract

Files:
- `skills/ha-entities.md`

Changes:
- keep object-only parser contract
- keep schema-probing prohibition
- enforce shortlist-first output guidance

### Task 3: Contract Tests

Files:
- `tests/skills/ha-nova-skill-contract.test.ts`

Changes:
- assert generic parallel `MUST` wording
- assert subagent fan-out wording
- assert shell-quoting reliability wording
- assert object-only parser and no-probing rules remain

### Task 4: Docs + Traceability

Files:
- `docs/choices.md`
- `docs/breadcrumbs.md`

Changes:
- record decisions for generic parallel orchestration and shell contract
- record verification evidence and installed skill update requirement

### Task 5: Verification + Global Skill Install

Commands:
- `npm test -- tests/skills/ha-nova-skill-contract.test.ts`
- `npm run install:codex-skill`

Acceptance:
- contract tests pass
- global skill updated
- only expected installer repo-root substitution differs from repo skill file

## Definition of Done

1. Parallel-by-default contract documented for independent tasks.
2. Sequential fallback documented and deterministic.
3. Quoting reliability documented with correct/incorrect examples.
4. Shortlist-first discovery policy documented.
5. Skill contract tests green.
6. Global skill installed and ready for retest.
