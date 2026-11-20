# Mock Version Wording Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the private HA/relay mock wording explicit so test output cannot be confused with the real HA Relay App version.

**Architecture:** Keep the mock dependency-free. Only rename the reported-version argument/env vars and tighten the printed status lines and contract tests.

**Tech Stack:** Python, shell helpers, Vitest contracts

---

### Task 1: Lock the contract first

**Files:**
- Modify: `tests/onboarding/desktop-validation-contract.test.ts`

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Run the focused contract test and verify it fails**
- [ ] **Step 3: Implement the minimal wording cleanup**
- [ ] **Step 4: Re-run the focused contract test and verify it passes**

### Task 2: Apply the wording cleanup

**Files:**
- Modify: `scripts/dev/mock-ha-relay.py`
- Modify: `scripts/dev/macos-private-rc-setup-all.sh`
- Modify: `scripts/dev/macos-private-rc-client.sh`

- [ ] **Step 1: Rename the mock reported-version argument and env vars**
- [ ] **Step 2: Make startup output explicit about fake `/health` reporting**
- [ ] **Step 3: Keep behavior otherwise unchanged**

### Task 3: Document and verify

**Files:**
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] **Step 1: Record the naming decision**
- [ ] **Step 2: Run focused contracts plus repo verification**
- [ ] **Step 3: Review until no findings remain**
