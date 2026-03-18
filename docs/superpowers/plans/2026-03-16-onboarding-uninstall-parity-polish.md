# Onboarding + Uninstall Parity Polish Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore the missing active LLAT walkthrough in setup and the missing uninstall preflight / relay-running note without widening the architecture.

**Architecture:** Keep the current Go wizard and uninstall flow. Add one small LLAT guide block in Step 2 and one small uninstall preflight/probe/report path around the existing removal logic.

**Tech Stack:** Go CLI, Go tests

---

## Chunk 1: Lock the Missing UX in Tests

### Task 1: Add failing setup and uninstall tests

**Files:**
- Modify: `cli/setup_interactive_test.go`
- Modify: `cli/uninstall_test.go`
- Reference: `docs/superpowers/specs/2026-03-16-onboarding-uninstall-parity-polish-design.md`

- [ ] **Step 1: Add a failing setup test**
  - require Step 2 output to mention:
    - Home Assistant Access Token
    - Security tab
    - Long-Lived Access Tokens
    - `ha_llat`
    - relay settings page guidance

- [ ] **Step 2: Add a failing uninstall test**
  - require preflight summary before confirmation
  - require post-uninstall relay-running note when the relay probe succeeded before deletion

- [ ] **Step 3: Run focused tests and watch them fail**

Run: `cd cli && go test ./... -run 'TestInteractiveSetupFreshInstallGuidesLLATSetup|TestRunUninstallShowsPreflightAndRelayStillRunningNote'`
Expected: FAIL

## Chunk 2: Implement the Smallest Runtime Changes

### Task 2: Add the LLAT walkthrough

**Files:**
- Modify: `cli/setup_interactive.go`

- [ ] **Step 1: Add a tiny LLAT guidance block after relay-token handling**
- [ ] **Step 2: Keep `back` / `exit` behavior intact**
- [ ] **Step 3: Reuse current browser helpers; do not add new transport logic**

### Task 3: Add uninstall preflight + relay-running note

**Files:**
- Modify: `cli/commands.go`
- Modify: `cli/uninstall_feedback.go`

- [ ] **Step 1: Print a short preflight summary before confirmation**
- [ ] **Step 2: Probe relay reachability before deleting config/token**
- [ ] **Step 3: Print a short post-uninstall note if the relay was still reachable**

## Chunk 3: Verify

### Task 4: Run verification

**Files:**
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] **Step 1: Run focused tests**

Run: `cd cli && go test ./... -run 'TestInteractiveSetupFreshInstallGuidesLLATSetup|TestRunUninstallShowsPreflightAndRelayStillRunningNote'`
Expected: PASS

- [ ] **Step 2: Run full Go tests**

Run: `cd cli && go test ./...`
Expected: PASS

- [ ] **Step 3: Run full project verification**

Run: `npm run verify`
Expected: PASS
