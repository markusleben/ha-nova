# RC Prerelease Publish Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the existing RC workflow optionally publish bundle-based GitHub prereleases for real public installer testing.

**Architecture:** Extend the current manual RC workflow with two inputs and one conditional publish job. Keep the default no-publish RC path unchanged. Document the public RC test flow in release docs.

**Tech Stack:** GitHub Actions YAML, GitHub CLI, Vitest contract tests, Markdown docs

---

### Task 1: Lock the contract first

**Files:**
- Modify: `tests/onboarding/release-contract.test.ts`

- [x] **Step 1: Write the failing test**
- [x] **Step 2: Run test to verify it fails**

### Task 2: Add optional RC prerelease publish

**Files:**
- Modify: `.github/workflows/release-candidate.yml`

- [ ] **Step 1: Add workflow_dispatch inputs**
- [ ] **Step 2: Add validation for prerelease tag input**
- [ ] **Step 3: Add conditional publish job after smoke passes**
- [ ] **Step 4: Use `gh release create` / upload for bundle assets only**

### Task 3: Update docs and project notes

**Files:**
- Modify: `docs/releasing.md`
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] **Step 1: Document the optional RC prerelease path**
- [ ] **Step 2: Document how to public-test installers with `HA_NOVA_VERSION`**
- [ ] **Step 3: Record the chosen KISS scope**

### Task 4: Verify and review

**Files:**
- Modify: `.github/workflows/release-candidate.yml`
- Modify: `tests/onboarding/release-contract.test.ts`
- Modify: `docs/releasing.md`

- [ ] **Step 1: Run `npm test -- tests/onboarding/release-contract.test.ts`**
- [ ] **Step 2: Run `npm run verify`**
- [ ] **Step 3: Request peer review on the workflow change**
