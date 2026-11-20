# WS Readiness Parity Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore old-release relay readiness semantics so setup, resume-state detection, and doctor no longer misdiagnose lazy upstream WS state as an LLAT failure, and keep Windows relay diagnostics usable.

**Architecture:** Keep the relay runtime unchanged and fix the interpretation layer in the CLI. Add one small shared readiness helper that combines `/health` with `/ws` ping fallback, then route setup, resume-state detection, and doctor through it. Keep LLAT-specific wording only when the `/ws` response proves it. Handle the Windows inline relay JSON issue as a separate, small CLI follow-up in the same branch.

**Tech Stack:** Go CLI, existing relay HTTP endpoints, Vitest safe gate, Go unit tests

---

## Chunk 1: Shared Readiness Model

### Task 1: Add failing tests for relay readiness interpretation

**Files:**
- Create: `cli/setup_readiness_test.go`
- Modify: `cli/setup_interactive_test.go`

- [ ] **Step 1: Add a failing test for `health=false + ws ping success`**
- [ ] **Step 2: Add a failing test for `health=false + ws ping returns LLAT error`**
- [ ] **Step 3: Add a failing test for `health=false + generic ws failure`**
- [ ] **Step 4: Add a failing test for resume-state using `health=false + ws ping success`**
- [ ] **Step 5: Run focused Go tests and confirm the intended failures**

### Task 2: Implement one shared readiness helper

**Files:**
- Create: `cli/setup_readiness.go`
- Reuse: `cli/setup_relay_diagnostics.go`

- [ ] **Step 1: Introduce a small readiness result type**
- [ ] **Step 2: Read `/health` first and capture passive WS state**
- [ ] **Step 3: Run `/ws` ping only when health is degraded**
- [ ] **Step 4: Return one normalized readiness verdict plus diagnosis**
- [ ] **Step 5: Expose one small readiness result that setup, doctor, and resume-state can all consume**
- [ ] **Step 6: Run focused Go tests and make them pass**

## Chunk 2: Setup + Doctor + Resume Parity

### Task 3: Route setup Step 3 through shared readiness

**Files:**
- Modify: `cli/setup_interactive.go`
- Test: `cli/setup_interactive_test.go`

- [ ] **Step 1: Replace inline health/ws branching with the shared helper**
- [ ] **Step 2: Keep old-release wording for LLAT-specific vs generic degraded cases**
- [ ] **Step 3: Verify setup still accepts ws ping success as connected**
- [ ] **Step 4: Run focused Go setup tests**

### Task 4: Route `ha-nova doctor` through shared readiness

**Files:**
- Modify: `cli/commands.go`
- Modify: `tests/onboarding/doctor-checks.test.ts`

- [ ] **Step 1: Replace doctor’s passive health-only interpretation with the shared helper**
- [ ] **Step 2: Surface LLAT-specific messaging only when `/ws` proves it**
- [ ] **Step 3: Preserve generic degraded wording for all other ws failures**
- [ ] **Step 4: Add or update tests to prove the new doctor output**
- [ ] **Step 5: Run focused doctor tests**

### Task 5: Route resume-state detection through shared readiness

**Files:**
- Modify: `cli/setup_state.go`
- Test: `cli/setup_state_test.go`

- [ ] **Step 1: Replace passive `ha_ws_connected` interpretation with the shared helper**
- [ ] **Step 2: Preserve early-return behavior when config or relay auth token is missing**
- [ ] **Step 3: Make `WSOK` true when `/health` is false but `/ws` ping succeeds**
- [ ] **Step 4: Keep incomplete state for LLAT-specific and generic ws failures**
- [ ] **Step 5: Run focused setup-state tests**

## Chunk 3: Windows Relay CLI JSON Follow-Up

### Task 6: Fix inline relay JSON handling on Windows

**Files:**
- Modify: `cli/relay.go`
- Modify: `cli/runtime.go`
- Test: `cli/app_runtime_test.go`
- Test: `cli/relay_test.go`

- [ ] **Step 1: Reproduce the current inline `relay ws -d '{\"type\":\"ping\"}'` failure in a focused test**
- [ ] **Step 2: Determine whether the failure is flag parsing, payload loading, or PowerShell-specific quoting normalization**
- [ ] **Step 3: Implement the smallest safe fix or, if the behavior is shell-limited, improve command help and diagnostics**
- [ ] **Step 4: Run focused relay CLI tests**

## Chunk 4: Verification + Docs

### Task 7: Verify and document the restored semantics

**Files:**
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] **Step 1: Record the decision that `/health` stays passive and CLI owns readiness interpretation**
- [ ] **Step 2: Record that setup, doctor, and resume-state now share the same readiness truth**
- [ ] **Step 3: Record the separate Windows inline relay JSON outcome**
- [ ] **Step 4: Run `cd cli && go test ./...`**
- [ ] **Step 5: Run `npm run verify`**
- [ ] **Step 6: Fresh-review the diff before any completion claim**

## Chunk 5: Real-Flow Validation

### Task 8: Revalidate onboarding and one real skill call per supported lane

**Files:**
- Modify: `docs/releasing.md`
- Modify: `docs/breadcrumbs.md`

- [ ] **Step 1: Run the normal Windows installer/userflow with the fixed readiness semantics**
- [ ] **Step 2: Run `ha-nova doctor` after Windows setup and confirm parity**
- [ ] **Step 3: Run one real Windows skill call on the supported client lane**
- [ ] **Step 4: Run the normal macOS installer/userflow with the fixed readiness semantics**
- [ ] **Step 5: Run one real macOS skill call after setup**
- [ ] **Step 6: Record exact supported lanes and any remaining client-specific prerequisites**
