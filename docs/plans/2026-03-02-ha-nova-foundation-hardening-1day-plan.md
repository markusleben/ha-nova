# 2026-03-02 HA NOVA Foundation Hardening (1 Day)

## Goal
Harden the modular skill foundation to production-grade maintainability with minimal scope and minimal overhead.

## Scope (In)
- Resolve routing/discovery authority split.
- Decouple contract tests by domain.
- Upgrade critical assertions from string-presence to semantic mapping checks.
- Specify write confirmation token contract (TTL + replay/binding rules).
- Reduce contract duplication drift risk between router and core docs.

## Scope (Out)
- No feature expansion (no new HA capabilities).
- No relay/server behavior changes.
- No UI/output redesign beyond contract clarifications.

## KISS Principles
- One canonical source per contract concern.
- Keep router as orchestration/routing only.
- Keep core files as normative contracts.
- Add smallest test utility needed; avoid frameworks.

## Day Plan (Timeboxed)

### Block 1 (90 min): Canonical Intent Contract
Problem:
- Router mentions companion skills while lazy discovery protocol points to discovery-map-only modules.

Deliverables:
1. Add canonical intent matrix file:
   - `skills/ha-nova/core/intents.md`
   - For each intent (`automation|script` x `create|update|delete|read|list`):
     - `required_companions[]`
     - `modules[]`
2. Update router and discovery docs to explicitly reference this matrix as source of truth.
3. Keep existing behavior unchanged; remove ambiguity only.

Definition of Done:
- Single file defines intent mapping fully.
- No contradictory wording remains in router/discovery sections.

### Block 2 (90 min): Contract Test Decomposition
Problem:
- `tests/skills/ha-nova-skill-contract.test.ts` is over-coupled.

Deliverables:
1. Split tests into:
   - `tests/skills/ha-nova-contract.test.ts`
   - `tests/skills/ha-entities-contract.test.ts`
   - `tests/skills/ha-safety-contract.test.ts`
   - `tests/skills/ha-cross-skill-integration.test.ts` (only necessary cross assumptions)
2. Keep current coverage parity (no silent coverage drop).

Definition of Done:
- Domain edits fail only domain tests.
- Cross-file assumptions live only in integration test.

### Block 3 (90 min): Semantic Assertions (High Value)
Problem:
- `toContain` allows contradictory mappings to pass.

Deliverables:
1. Add tiny markdown parsing helper in tests:
   - parse intent blocks from canonical matrix.
2. Replace critical string assertions with exact-set assertions:
   - intent exists
   - exact companion list
   - exact module list
   - no forbidden extras
3. Keep lightweight and deterministic.

Definition of Done:
- Contradictory mappings fail tests deterministically.
- No brittle line-break-dependent assertions for mappings.

### Block 4 (60 min): Confirm Token Contract Hardening
Problem:
- `confirm:<token>` semantics are underspecified.

Deliverables:
1. Extend `skills/ha-nova/core/contracts.md` with normative rules:
   - token TTL (default 10 min)
   - one-time use (replay forbidden)
   - binding to method/path/target
   - binding to preview digest/hash
   - stale/mismatch behavior (hard fail + regenerate preview)
2. Mirror concise summary in router (`skills/ha-nova.md`) as reference only.
3. Add negative contract tests for stale/reuse/mismatch wording presence.

Definition of Done:
- Token lifecycle and failure modes are unambiguous.
- Tests enforce presence of all four safety constraints.

### Block 5 (60 min): Duplication Reduction + Parity Guard
Problem:
- Contract text duplicated across router and core increases drift risk.

Deliverables:
1. Router keeps only:
   - mission
   - orchestration rules
   - routing entry points
   - references to core contracts
2. Move/keep normative contract text in core files only.
3. Add parity tests:
   - router references canonical files
   - core files contain normative sections
   - installed mirror parity remains intact.

Definition of Done:
- No normative contract duplication between router/core.
- Drift detection is test-enforced.

## Execution Order
1. Canonical intent matrix
2. Test decomposition
3. Semantic assertions
4. Token contract hardening
5. Duplication reduction

Reason:
- Stabilize source of truth first, then align tests, then harden safety, then clean docs.

## Risks + Mitigation
- Risk: Over-refactor tests in one day.
  - Mitigation: parity-first split, no new behavior assertions beyond mapped scope.
- Risk: Accidental behavior change while cleaning docs.
  - Mitigation: wording-only edits unless explicitly required; verify with contract tests.
- Risk: New canonical file introduces another drift axis.
  - Mitigation: make tests parse only canonical file for mapping assertions.

## Verification Gate (Mandatory)
Run:
1. `npm test -- tests/skills/*.test.ts`
2. `npm run typecheck`
3. skill mirror check:
   - `skills/ha-nova.md` equals `.agents/skills/ha-nova/SKILL.md`

Accept only if all pass.

## Success Criteria (End of Day)
- One canonical intent contract.
- Tests modular by responsibility.
- Mapping checks semantic, not string-fragile.
- Confirm token contract explicitly hardened.
- Router/core responsibilities clearly separated.

## MVP Decision Log
- Keep everything markdown-first (no codegen pipeline yet).
- Keep helper parser local to tests.
- Do not add runtime enforcement code in this pass; contract-first hardening only.
