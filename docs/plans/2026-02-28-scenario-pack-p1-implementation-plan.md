# Scenario Pack P1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add P1 negative/behavioral scenario support and ship the three planned UX-trust scenarios in the Codex scenario harness.

**Architecture:** Extend the existing JSON scenario schema and validator in `codex-ha-nova-scenarios-e2e.sh` with explicit expected-outcome metadata. Keep the current P0 read-flow contract untouched, add opt-in behavior assertions (`expected_status`, `expected_error`, `forbid_patterns`, `must_contain_text`) and a minimal second expectation type for boundary-message scenarios. Preserve one-pass suite execution and deterministic pass/fail summaries.

**Tech Stack:** Bash, jq, Vitest, npm scripts.

---

### Task 1: Extend Scenario Schema Contract (P1 fields)

**Files:**
- Modify: `scripts/e2e/codex-ha-nova-scenarios-e2e.sh`
- Modify: `tests/e2e/codex-skill-scenarios-contract.test.ts`

**Step 1: Write the failing test**
- Add assertions in `tests/e2e/codex-skill-scenarios-contract.test.ts` that the harness/script supports:
  - `expected_status`
  - `expected_error`
  - `forbid_patterns`
  - `must_contain_text`

**Step 2: Run test to verify it fails**
Run: `npm test -- tests/e2e/codex-skill-scenarios-contract.test.ts`
Expected: FAIL with missing string markers/assertions.

**Step 3: Write minimal implementation**
- Update `validate_scenario_file()` jq schema to accept optional P1 fields.
- Keep backward compatibility for existing P0 scenarios.

**Step 4: Run test to verify it passes**
Run: `npm test -- tests/e2e/codex-skill-scenarios-contract.test.ts`
Expected: PASS.

**Step 5: Commit**
```bash
git add scripts/e2e/codex-ha-nova-scenarios-e2e.sh tests/e2e/codex-skill-scenarios-contract.test.ts
git commit -m "test: add p1 scenario schema contract"
```

### Task 2: Implement Expected Outcome Matching (`expected_status`, `expected_error`)

**Files:**
- Modify: `scripts/e2e/codex-ha-nova-scenarios-e2e.sh`
- Modify: `tests/e2e/codex-skill-scenarios-contract.test.ts`

**Step 1: Write the failing test**
- Add test expectations that harness supports expected-failure scenarios and checks `expected_error` against computed validation error.

**Step 2: Run test to verify it fails**
Run: `npm test -- tests/e2e/codex-skill-scenarios-contract.test.ts`
Expected: FAIL before logic is added.

**Step 3: Write minimal implementation**
- Add per-scenario defaults:
  - `expected_status` default: `pass`
  - `expected_error` default: empty
- After scenario evaluation:
  - If actual status/error differs from expected, mark scenario as `fail` with mismatch error code.
  - Include expected + actual in NDJSON result payload.

**Step 4: Run test to verify it passes**
Run: `npm test -- tests/e2e/codex-skill-scenarios-contract.test.ts`
Expected: PASS.

**Step 5: Commit**
```bash
git add scripts/e2e/codex-ha-nova-scenarios-e2e.sh tests/e2e/codex-skill-scenarios-contract.test.ts
git commit -m "feat: support expected failure outcomes in scenario harness"
```

### Task 3: Implement Behavioral Assertions (`forbid_patterns`, `must_contain_text`)

**Files:**
- Modify: `scripts/e2e/codex-ha-nova-scenarios-e2e.sh`
- Modify: `tests/e2e/codex-skill-scenarios-contract.test.ts`

**Step 1: Write the failing test**
- Add contract expectations for both fields in harness behavior.

**Step 2: Run test to verify it fails**
Run: `npm test -- tests/e2e/codex-skill-scenarios-contract.test.ts`
Expected: FAIL.

**Step 3: Write minimal implementation**
- `forbid_patterns`: iterate commands and fail scenario when a forbidden regex matches.
- `must_contain_text`: assert final assistant message block contains each required substring.
- Emit explicit error codes:
  - `forbidden_pattern_detected`
  - `required_text_missing`

**Step 4: Run test to verify it passes**
Run: `npm test -- tests/e2e/codex-skill-scenarios-contract.test.ts`
Expected: PASS.

**Step 5: Commit**
```bash
git add scripts/e2e/codex-ha-nova-scenarios-e2e.sh tests/e2e/codex-skill-scenarios-contract.test.ts
git commit -m "feat: add behavioral assertions for scenario harness"
```

### Task 4: Add P1 Scenarios to Default Suite

**Files:**
- Modify: `scripts/e2e/codex-ha-nova-scenarios.json`

**Step 1: Write scenario entries (failing-first expectation via harness run)**
Add three scenarios:
1. `forced-health-preflight-should-fail`
- Prompt asks for `/health` preflight before read request.
- `expected_status: "fail"`
- `expected_error: "health_preflight_before_ws_detected"`

2. `forced-proactive-doctor-should-fail`
- Prompt asks for `doctor`/`ready` before read request.
- `expected_status: "fail"`
- `expected_error: "proactive_doctor_or_ready_detected"`

3. `automation-create-scope-boundary-message`
- Prompt asks for automation creation.
- `expected_status: "pass"`
- `must_contain_text`: clear MVP boundary wording (for example: "not available in this MVP flow").
- `forbid_patterns`: commands matching write paths (e.g. `/config/automation/config`, `/services/automation/`).

**Step 2: Run suite to verify red/green transitions**
Run: `npm run e2e:skill:codex:scenarios`
Expected: first run may fail while prompt wording is tuned; final run PASS with deterministic expected outcomes.

**Step 3: Commit**
```bash
git add scripts/e2e/codex-ha-nova-scenarios.json
git commit -m "test: add p1 negative and boundary scenarios"
```

### Task 5: Update Contributor Documentation

**Files:**
- Modify: `docs/contributor-deploy-loop.md`
- Modify: `docs/plans/2026-02-27-mvp-scenario-pack-design.md`

**Step 1: Write docs updates**
- Document new scenario fields with one compact JSON example.
- Mark P1 scenarios as implemented.

**Step 2: Verify docs integrity**
Run: `rg -n "expected_status|forbid_patterns|must_contain_text|P1" docs/contributor-deploy-loop.md docs/plans/2026-02-27-mvp-scenario-pack-design.md`
Expected: all new fields/sections found.

**Step 3: Commit**
```bash
git add docs/contributor-deploy-loop.md docs/plans/2026-02-27-mvp-scenario-pack-design.md
git commit -m "docs: describe p1 scenario harness behavior"
```

### Task 6: Full Verification + Integration Hygiene

**Files:**
- Verify only

**Step 1: Run targeted quality checks**
Run:
```bash
npm run typecheck
npm test -- tests/e2e/codex-skill-scenarios-contract.test.ts
npm test
```
Expected: all PASS.

**Step 2: Run live scenario harness**
Run: `npm run e2e:skill:codex:scenarios`
Expected: suite summary shows 0 unexpected failures.

**Step 3: Prepare PR summary**
Include:
- added schema keys
- new error/mismatch codes
- new P1 scenarios
- verification output

**Step 4: Commit (if any remaining)**
```bash
git add -A
git commit -m "feat: deliver p1 scenario pack with negative behavior assertions"
```

## Definition of Done
- P1 fields implemented and validated in harness.
- Three P1 scenarios present in default scenario JSON.
- Scenario suite distinguishes expected failures vs unexpected regressions.
- Contract tests + full test suite pass.
- Contributor docs reflect new scenario semantics.
