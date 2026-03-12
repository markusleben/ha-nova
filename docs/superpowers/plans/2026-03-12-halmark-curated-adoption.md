# HALMark Curated Adoption Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a small, high-signal HALMark-inspired safety subset to HA NOVA as skill policy, review checks, attribution, and contract tests without changing relay runtime.

**Architecture:** Keep all adoption in markdown skills, review catalog entries, and contract tests. Treat HALMark as an external inspiration source, not a runtime dependency or second source of truth. Land the first pass in a small, UX-safe package focused on invalid-premise correction, targeting ambiguity, scope creep, templated event-name review, and destructive verify wording.

**Tech Stack:** Markdown skills, Vitest contract tests, repository docs

**Execution note:** This plan assumes execution in a clean dedicated worktree. If implementation happens in a dirty shared worktree, replace commit checkpoints with targeted `git diff` review and only commit after isolation or explicit user request.

---

## Chunk 1: Policy and Check Surface

### Task 1: Add invalid-premise correction baseline

**Files:**
- Modify: `skills/ha-nova/SKILL.md`
- Modify: `skills/ha-nova-guide/SKILL.md`
- Modify: `tests/skills/ha-safety-contract.test.ts`

- [ ] **Step 1: Write the failing contract expectation**

Add assertions to `tests/skills/ha-safety-contract.test.ts` that require concise invalid-premise correction wording in the context skill and guide skill.

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run tests/skills/ha-safety-contract.test.ts`
Expected: FAIL because the new wording is not present yet.

- [ ] **Step 3: Write minimal implementation**

Update `skills/ha-nova/SKILL.md` and `skills/ha-nova-guide/SKILL.md` so they explicitly require correcting invalid HA premises without becoming lecture-like.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run tests/skills/ha-safety-contract.test.ts`
Expected: PASS

- [ ] **Step 5: Checkpoint review**

If running in a clean dedicated worktree:

```bash
git add skills/ha-nova/SKILL.md skills/ha-nova-guide/SKILL.md tests/skills/ha-safety-contract.test.ts
git commit -m "docs(skills): add invalid premise correction policy"
```

If running in a dirty shared worktree:

```bash
git diff -- skills/ha-nova/SKILL.md skills/ha-nova-guide/SKILL.md tests/skills/ha-safety-contract.test.ts
```

### Task 2: Add targeting ambiguity rule

**Files:**
- Modify: `skills/ha-nova-write/SKILL.md`
- Modify: `skills/ha-nova-service-call/SKILL.md`
- Modify: `tests/skills/service-call-contract.test.ts`
- Modify: `tests/skills/ha-cross-skill-integration.test.ts`

- [ ] **Step 1: Write the failing contract expectation**

Add assertions that require:
- broad-target ambiguity wording in service-call and write flows
- explicit reuse of the existing one-question ambiguity budget
- no second blocking ambiguity question in the same turn

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
npx vitest run tests/skills/service-call-contract.test.ts
npx vitest run tests/skills/ha-cross-skill-integration.test.ts
```
Expected: FAIL because the new wording is not present yet.

- [ ] **Step 3: Write minimal implementation**

Update the skill docs with tight language only:
- broad targets need clarification when a narrower plausible scope exists
- reuse the existing ambiguity budget instead of adding a second blocking question

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
npx vitest run tests/skills/service-call-contract.test.ts
npx vitest run tests/skills/ha-cross-skill-integration.test.ts
```
Expected: PASS

- [ ] **Step 5: Checkpoint review**

If running in a clean dedicated worktree:

```bash
git add skills/ha-nova-write/SKILL.md skills/ha-nova-service-call/SKILL.md tests/skills/service-call-contract.test.ts tests/skills/ha-cross-skill-integration.test.ts
git commit -m "docs(skills): add broad-target ambiguity rule"
```

If running in a dirty shared worktree:

```bash
git diff -- skills/ha-nova-write/SKILL.md skills/ha-nova-service-call/SKILL.md tests/skills/service-call-contract.test.ts tests/skills/ha-cross-skill-integration.test.ts
```

### Task 3: Add scope-creep rule

**Files:**
- Modify: `skills/ha-nova-write/SKILL.md`
- Modify: `skills/ha-nova/safe-refactoring.md`
- Modify: `tests/skills/ha-cross-skill-integration.test.ts`

- [ ] **Step 1: Write the failing contract expectation**

Add assertions that require:
- minimal-diff / no-unrequested-rewrite wording
- explicit constrained-authority wording for small requested edits

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run tests/skills/ha-cross-skill-integration.test.ts`
Expected: FAIL because the new wording is not present yet.

- [ ] **Step 3: Write minimal implementation**

Update the two docs with tight language only:
- write = do not rewrite unrelated structure
- safe refactoring = preserve requested scope

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run tests/skills/ha-cross-skill-integration.test.ts`
Expected: PASS

- [ ] **Step 5: Checkpoint review**

If running in a clean dedicated worktree:

```bash
git add skills/ha-nova-write/SKILL.md skills/ha-nova/safe-refactoring.md tests/skills/ha-cross-skill-integration.test.ts
git commit -m "docs(skills): add minimal-diff stewardship rule"
```

If running in a dirty shared worktree:

```bash
git diff -- skills/ha-nova-write/SKILL.md skills/ha-nova/safe-refactoring.md tests/skills/ha-cross-skill-integration.test.ts
```

### Task 4: Add delete/refactor verification rule

**Files:**
- Modify: `skills/ha-nova-write/SKILL.md`
- Modify: `skills/ha-nova/safe-refactoring.md`
- Create: `tests/skills/write-delete-safety-contract.test.ts`

- [ ] **Step 1: Write the failing contract expectation**

Create a focused contract suite that requires:
- destructive change verification wording in write flow
- blast-radius / dependency verification wording in safe refactoring
- verification expressed in HA NOVA read-back / related-check terms, not Git-only language

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run tests/skills/write-delete-safety-contract.test.ts`
Expected: FAIL because the wording is not present yet.

- [ ] **Step 3: Write minimal implementation**

Update the two docs so destructive success claims require verification of what changed and what still depends on the deleted target.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run tests/skills/write-delete-safety-contract.test.ts`
Expected: PASS

- [ ] **Step 5: Checkpoint review**

If running in a clean dedicated worktree:

```bash
git add skills/ha-nova-write/SKILL.md skills/ha-nova/safe-refactoring.md tests/skills/write-delete-safety-contract.test.ts
git commit -m "docs(skills): add destructive verify rule"
```

If running in a dirty shared worktree:

```bash
git diff -- skills/ha-nova-write/SKILL.md skills/ha-nova/safe-refactoring.md tests/skills/write-delete-safety-contract.test.ts
```

### Task 5: Add the focused review check

**Files:**
- Modify: `skills/ha-nova-review/checks.md`
- Modify: `skills/ha-nova/template-guidelines.md`
- Modify: `tests/skills/review-contract.test.ts`

- [ ] **Step 1: Write the failing contract expectation**

Add assertions requiring a new review-catalog entry for templated event names and one supporting guideline mention.

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run tests/skills/review-contract.test.ts`
Expected: FAIL because the new check is not present yet.

- [ ] **Step 3: Write minimal implementation**

Add one new concrete check in `skills/ha-nova-review/checks.md` and a short example in `skills/ha-nova/template-guidelines.md`.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run tests/skills/review-contract.test.ts`
Expected: PASS

- [ ] **Step 5: Checkpoint review**

If running in a clean dedicated worktree:

```bash
git add skills/ha-nova-review/checks.md skills/ha-nova/template-guidelines.md tests/skills/review-contract.test.ts
git commit -m "docs(review): add templated event name safety check"
```

If running in a dirty shared worktree:

```bash
git diff -- skills/ha-nova-review/checks.md skills/ha-nova/template-guidelines.md tests/skills/review-contract.test.ts
```

## Chunk 2: Attribution and Whole-Repo Verification

### Task 6: Add attribution without creating a second source of truth

**Files:**
- Modify: `README.md`
- Optional Create: `docs/reference/external-influences.md`
- Test: `tests/skills/ha-safety-contract.test.ts` only if an acknowledgment assertion fits naturally

- [ ] **Step 1: Decide the smallest attribution surface**

Prefer a short acknowledgment in `README.md`. Only create `docs/reference/external-influences.md` if the README note would become too cramped.

- [ ] **Step 2: Write minimal implementation**

Add a concise acknowledgment that selected safety/stewardship ideas were inspired by HALMark by Nathan Curtis.

- [ ] **Step 3: Add verification only if needed**

If there is already a suitable contract suite for repo docs, add an assertion. If not, skip new test-file creation to keep scope small.

- [ ] **Step 4: Review for second-source-of-truth risk**

Check that no file claims HA NOVA is synchronized with HALMark or mirrors its spec.

- [ ] **Step 5: Checkpoint review**

If running in a clean dedicated worktree:

```bash
git add README.md
git add docs/reference/external-influences.md 2>/dev/null || true
git add tests/skills/ha-safety-contract.test.ts
git commit -m "docs: acknowledge HALMark inspiration"
```

If running in a dirty shared worktree:

```bash
git diff -- README.md docs/reference/external-influences.md tests/skills/ha-safety-contract.test.ts
```

### Task 7: Run focused verification, then full verification

**Files:**
- No code changes expected

- [ ] **Step 1: Run focused tests**

Run:
```bash
npx vitest run tests/skills/ha-safety-contract.test.ts
npx vitest run tests/skills/service-call-contract.test.ts
npx vitest run tests/skills/ha-cross-skill-integration.test.ts
npx vitest run tests/skills/write-delete-safety-contract.test.ts
npx vitest run tests/skills/review-contract.test.ts
```
Expected: PASS

- [ ] **Step 2: Run whole skill and repo verification**

Run:
```bash
npm test
npm run typecheck
bash scripts/check-docs.sh
```
Expected:
- all tests pass
- typecheck passes
- docs fact-check passes

- [ ] **Step 3: Self-review the changed wording**

Review the final diff for:
- no HALMark harness references
- no relay/runtime coupling
- no extra user friction from over-warning
- no second source of truth wording

- [ ] **Step 4: Checkpoint review**

Always review the final diff:

```bash
git diff -- README.md skills/ha-nova/SKILL.md skills/ha-nova-guide/SKILL.md skills/ha-nova-write/SKILL.md skills/ha-nova-service-call/SKILL.md skills/ha-nova/safe-refactoring.md skills/ha-nova-review/checks.md skills/ha-nova/template-guidelines.md tests/skills/ha-safety-contract.test.ts tests/skills/service-call-contract.test.ts tests/skills/ha-cross-skill-integration.test.ts tests/skills/write-delete-safety-contract.test.ts tests/skills/review-contract.test.ts
```

If running in a clean dedicated worktree and the diff is good, optionally commit:

```bash
git add README.md skills/ha-nova/SKILL.md skills/ha-nova-guide/SKILL.md skills/ha-nova-write/SKILL.md skills/ha-nova-service-call/SKILL.md skills/ha-nova/safe-refactoring.md skills/ha-nova-review/checks.md skills/ha-nova/template-guidelines.md tests/skills/ha-safety-contract.test.ts tests/skills/service-call-contract.test.ts tests/skills/ha-cross-skill-integration.test.ts tests/skills/write-delete-safety-contract.test.ts tests/skills/review-contract.test.ts
git commit -m "docs: adopt curated HALMark-inspired safety rules"
```
