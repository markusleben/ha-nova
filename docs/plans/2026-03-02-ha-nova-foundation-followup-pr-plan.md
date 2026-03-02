# 2026-03-02 HA NOVA Foundation Follow-up (3 Small PRs)

## Goal
Close the three residual hardening gaps after the 1-day foundation pass, with minimal scope and clear verification.

## Residual Gaps
1. Confirmation token hardening is contract-level only (no executable validator behavior tests).
2. Router-to-intent-matrix behavior is not validated via executable dispatch simulation.
3. Compatibility shim validates file presence only, not semantic strength of split suites.

## Strategy
Use 3 sequential, small PRs. Each PR is independently mergeable and low-risk.

## PR-1: Executable Token Validation Spec Tests

### Objective
Add executable tests for token semantics (TTL, replay, target binding, preview digest binding) without changing relay/server runtime.

### Scope (In)
- Add a tiny test-local token validator helper (pure function) under `tests/skills/helpers/`.
- Add tests for positive and negative token paths.
- Keep docs as normative source; tests prove unambiguous behavior.

### Scope (Out)
- No production runtime/token service implementation.
- No HA/relay API changes.

### Deliverables
1. `tests/skills/helpers/confirm-token-validator.ts`
2. `tests/skills/ha-token-contract.test.ts`
3. Assertions for:
   - valid token accepted within TTL
   - stale token rejected
   - replay rejected
   - method/path/target mismatch rejected
   - preview digest mismatch rejected

### Definition of Done
- Token contract is executable as test logic, not just text assertions.
- `npm test -- tests/skills/*.test.ts` remains green.

## PR-2: Intent Dispatcher Simulation Test

### Objective
Prove router-to-matrix behavior with executable dispatch simulation from canonical intent matrix.

### Scope (In)
- Add a tiny test dispatcher helper that resolves effective load set:
  - `required_companions[] + modules[]`
- Add end-to-end intent resolution tests for all 10 intents.

### Scope (Out)
- No runtime dispatcher integration in production code.

### Deliverables
1. `tests/skills/helpers/intent-dispatcher.ts`
2. `tests/skills/ha-intent-dispatcher.test.ts`
3. Coverage for:
   - exact load set per intent
   - no unexpected extras
   - list/read differences preserved

### Definition of Done
- Intent behavior can be validated end-to-end in tests from canonical matrix.
- Any drift in matrix semantics fails deterministic tests.

## PR-3: Compatibility Shim Strengthening

### Objective
Upgrade shim from existence-only to semantic smoke checks of split suites.

### Scope (In)
- Keep shim lightweight.
- Validate that each split suite includes at least one semantic test anchor.

### Scope (Out)
- No duplication of full suite assertions back into shim.

### Deliverables
1. Enhance `tests/skills/ha-nova-skill-contract.test.ts` to assert:
   - split files exist
   - key semantic anchors exist per suite (matrix parse, token contract, integration mapping)
2. Keep fast runtime.

### Definition of Done
- Shim catches accidental suite hollowing/weakening.
- Still acts as compatibility entrypoint for legacy command paths.

## Execution Order
1. PR-1 token validator tests
2. PR-2 dispatcher simulation
3. PR-3 shim strengthening

Reason:
- Safety semantics first, routing semantics second, guardrail stability third.

## Risk Controls
- Keep helpers test-only.
- No production runtime behavior change.
- Prefer pure functions and table-driven tests.

## Verification Gate (Per PR)
1. `npm test -- tests/skills/*.test.ts`
2. `npm run typecheck`

## Final Acceptance
All three PRs merged => residual hardening risks reduced to documentation/runtime integration gap only (explicitly tracked).

## Optional Next (separate future PR)
- Move token validation and intent dispatch from test helpers into reusable runtime utilities when implementation phase is approved.
