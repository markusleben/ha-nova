# Setup Wizard Parity Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore the Go-based interactive setup flow to at least the old release wizard UX while preserving the current Go runtime architecture.

**Architecture:** Keep `runSetup()` as the coordinator, but move setup-specific UI and setup-state logic into small focused Go files. Port only the old release behavior that materially affects user experience: phased guidance, resume/status, retry/incomplete paths, and the final success/incomplete banners.

**Tech Stack:** Go CLI, existing runtime/config/state/keyring services, Vitest contracts, Go tests.

---

## Chunk 1: Lock Parity Requirements

### Task 1: Add failing tests for wizard framing

**Files:**
- Modify: `cli/setup_ui_test.go`
- Test: `cli/setup_ui_test.go`

- [ ] **Step 1: Add a failing test for setup header rendering**
- [ ] **Step 2: Add a failing test for step rendering (`Step 1 of 4`, etc.)**
- [ ] **Step 3: Add a failing test for success/incomplete banner rendering**
- [ ] **Step 4: Run `cd cli && go test ./... -run 'TestPromptSetupClient|TestSetup'` and verify failure**
- [ ] **Step 5: Commit**

### Task 2: Add failing tests for setup-state summary

**Files:**
- Create: `cli/setup_state_test.go`
- Test: `cli/setup_state_test.go`

- [ ] **Step 1: Add a failing test for “already done” status summary rendering**
- [ ] **Step 2: Add a failing test for “all complete -> exit early” decision**
- [ ] **Step 3: Add a failing test for partial completion skip-summary logic**
- [ ] **Step 4: Run `cd cli && go test ./... -run 'TestSetupState'` and verify failure**
- [ ] **Step 5: Commit**

## Chunk 2: Build Minimal Go Wizard UI

### Task 3: Add setup UI helpers

**Files:**
- Modify: `cli/setup_ui.go`
- Test: `cli/setup_ui_test.go`

- [ ] **Step 1: Implement header rendering helper**
- [ ] **Step 2: Implement step rendering helper**
- [ ] **Step 3: Implement success/info/fail line helpers for setup UX**
- [ ] **Step 4: Implement success/incomplete banner helpers**
- [ ] **Step 5: Run `cd cli && go test ./... -run 'TestPromptSetupClient|TestSetup'`**
- [ ] **Step 6: Commit**

### Task 4: Keep client selection parity

**Files:**
- Modify: `cli/setup_ui.go`
- Test: `cli/setup_ui_test.go`

- [ ] **Step 1: Keep the current numbered client list stable**
- [ ] **Step 2: Add/keep tests for default, numeric, named, and invalid input**
- [ ] **Step 3: Run `cd cli && go test ./... -run 'TestPromptSetupClient'`**
- [ ] **Step 4: Commit**

## Chunk 3: Restore Resume + Guided Flow

### Task 5: Introduce setup-state detection

**Files:**
- Create: `cli/setup_state.go`
- Create: `cli/setup_state_test.go`
- Modify: `cli/commands.go`

- [ ] **Step 1: Add a minimal `setupState` struct for config/token/relay/ws/skills status**
- [ ] **Step 2: Implement detection helpers using existing config/keyring/doctor probes**
- [ ] **Step 3: Wire `runSetup()` to compute setup state before interactive phases**
- [ ] **Step 4: Run `cd cli && go test ./... -run 'TestSetupState'`**
- [ ] **Step 5: Commit**

### Task 6: Show resume summary and early-complete exit

**Files:**
- Modify: `cli/commands.go`
- Modify: `cli/setup_ui.go`
- Test: `cli/setup_state_test.go`

- [ ] **Step 1: Print header + status summary before phase execution**
- [ ] **Step 2: Exit early when all phases are already complete**
- [ ] **Step 3: Show “already done” summary when some phases are skipped**
- [ ] **Step 4: Run `cd cli && go test ./... -run 'TestSetupState|TestSetup'`**
- [ ] **Step 5: Commit**

## Chunk 4: Restore Interactive Verification Behavior

### Task 7: Port guided relay/WS retry behavior

**Files:**
- Modify: `cli/commands.go`
- Modify: `cli/setup_ui.go`
- Test: `cli/setup_state_test.go`

- [ ] **Step 1: Add interactive retry/incomplete loop for relay health failures**
- [ ] **Step 2: Add interactive retry/incomplete loop for websocket degraded state**
- [ ] **Step 3: Keep non-interactive mode fail-fast**
- [ ] **Step 4: Run `cd cli && go test ./... -run 'TestSetupState|TestSetup'`**
- [ ] **Step 5: Commit**

### Task 8: Restore final success/incomplete endings

**Files:**
- Modify: `cli/commands.go`
- Modify: `cli/setup_ui.go`
- Test: `cli/setup_ui_test.go`

- [ ] **Step 1: Add success banner with client-friendly wording**
- [ ] **Step 2: Add incomplete banner variants for relay/ws/skills issues**
- [ ] **Step 3: Run `cd cli && go test ./... -run 'TestSetup'`**
- [ ] **Step 4: Commit**

## Chunk 5: Validate End-to-End

### Task 9: Update contracts/docs

**Files:**
- Modify: `tests/onboarding/windows-installer-contract.test.ts`
- Modify: `tests/onboarding/desktop-validation-contract.test.ts`
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] **Step 1: Extend contracts only where the new wizard behavior is externally visible**
- [ ] **Step 2: Record setup wizard parity decision and implementation breadcrumb**
- [ ] **Step 3: Run `npm test -- tests/onboarding/windows-installer-contract.test.ts tests/onboarding/desktop-validation-contract.test.ts`**
- [ ] **Step 4: Commit**

### Task 10: Full verification + desktop proof

**Files:**
- Modify: none unless fixes are needed

- [ ] **Step 1: Run `npm run verify`**
- [ ] **Step 2: Run `cd cli && go test ./...`**
- [ ] **Step 3: Run the explicit macOS desktop lane**
- [ ] **Step 4: Run the Windows desktop lane for the normal installer/setup userflow**
- [ ] **Step 5: Review results and fix any parity gaps**
- [ ] **Step 6: Commit**
