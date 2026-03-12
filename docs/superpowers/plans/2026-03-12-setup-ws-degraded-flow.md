# Setup WS-Degraded Flow Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ha-nova setup` handle degraded relay-to-HA WebSocket connectivity inside setup and avoid false `Setup complete!` banners.

**Architecture:** Keep the existing onboarding structure. Tighten the verify phase so relay health plus upstream WS state produce an explicit setup result (`complete` vs `incomplete`) and reuse existing retry UX patterns instead of inventing a new flow.

**Tech Stack:** Bash onboarding scripts, Vitest onboarding integration tests

---

## Chunk 1: Red Tests

### Task 1: Add degraded-setup expectations

**Files:**
- Modify: `tests/onboarding/setup-fresh-install.test.ts`
- Modify: `tests/onboarding/setup-relay-failures.test.ts`

- [ ] **Step 1: Write the failing tests**
- [ ] **Step 2: Run focused Vitest commands and confirm current behavior is wrong**

### Task 2: Add softer doctor expectation

**Files:**
- Modify: `tests/onboarding/doctor-checks.test.ts`

- [ ] **Step 1: Write the failing test/assertions**
- [ ] **Step 2: Run the focused doctor test and confirm failure**

## Chunk 2: Green Implementation

### Task 3: Tighten setup verify flow

**Files:**
- Modify: `scripts/onboarding/macos-lib.sh`

- [ ] **Step 1: Add a tracked setup result for degraded WS verification**
- [ ] **Step 2: Reuse retry-style prompts for WS degraded verification**
- [ ] **Step 3: Change the final banner to `Setup incomplete` when needed**

### Task 4: Soften degraded WS diagnosis

**Files:**
- Modify: `scripts/onboarding/macos-lib.sh`
- Modify: `scripts/onboarding/lib/relay.sh`

- [ ] **Step 1: Remove unconditional `ha_llat is required` copy**
- [ ] **Step 2: Keep specific LLAT wording only when the `/ws` body proves it**

## Chunk 3: Verification

### Task 5: Focused verification

**Files:**
- Test: `tests/onboarding/setup-fresh-install.test.ts`
- Test: `tests/onboarding/setup-relay-failures.test.ts`
- Test: `tests/onboarding/doctor-checks.test.ts`

- [ ] **Step 1: Run focused tests**
- [ ] **Step 2: Fix any regressions**
- [ ] **Step 3: Run broader `tests/onboarding` verification**
