# Safe Test Architecture Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make HA NOVA verification safe by default on a maintainer host while keeping explicit macOS and Windows desktop validation lanes for release proof.

**Architecture:** Split the current mixed test surface into one safe default gate and three explicit desktop/VM lanes. Keep one shared private-RC artifact path and remove host-affecting behavior from the default verification contract.

**Tech Stack:** npm scripts, Vitest, Go test, Bash, PowerShell, private RC bundles, SSH, RDP

---

## Chunk 1: Lock the Default Safe Boundary

### Task 1: Define what `npm run verify` is allowed to run

**Files:**
- Modify: `package.json`
- Modify: `docs/releasing.md`
- Modify: `docs/superpowers/specs/2026-03-15-safe-test-architecture-design.md`

- [ ] List the exact safe suites/scripts allowed in `verify`
- [ ] Remove desktop/host-affecting suites from the default chain
- [ ] Add one sentence in docs: `verify` must never open browser or touch the real keychain

### Task 2: Create explicit desktop script entrypoints

**Files:**
- Modify: `package.json`
- Modify: `docs/releasing.md`

- [ ] Add one explicit macOS desktop command
- [ ] Add one explicit Windows headless command
- [ ] Add one explicit Windows desktop command
- [ ] Keep names short and literal; do not add extra wrapper layers

## Chunk 2: Eliminate Surprise Side Effects

### Task 3: Add a hard no-browser contract

**Files:**
- Modify: `cli/commands.go`
- Modify: `scripts/onboarding/macos-lib.sh`
- Modify: `scripts/onboarding/platform/macos.sh`
- Modify: `tests/...` only where contract coverage is needed

- [ ] Add one environment guard for browser launch
- [ ] Make all automated runners set it
- [ ] Ensure the default verification path cannot open a browser even if a legacy script slips in

### Task 4: Isolate automated secret storage

**Files:**
- Modify: `cli/keyring_*.go`
- Modify: legacy shell secret helpers if still used by tests
- Modify: desktop helper scripts only if needed

- [ ] Ensure automated macOS tests use a dedicated test service or mock path
- [ ] Ensure default verification cannot overwrite real HA NOVA credentials
- [ ] Keep the product path unchanged for real user flows

## Chunk 3: Remove Legacy Shell Tests from the Default Gate

### Task 5: Reclassify legacy macOS shell onboarding tests

**Files:**
- Modify: `package.json`
- Modify: relevant Vitest test files under `tests/onboarding/`
- Modify: `docs/releasing.md`

- [ ] Move legacy shell-execution tests out of the default suite
- [ ] Keep only static/contract coverage in the default suite
- [ ] Put real shell execution behind an explicit manual desktop lane

### Task 6: Add orphan cleanup for interrupted runs

**Files:**
- Create or modify: one small cleanup script under `scripts/dev/`
- Modify: `docs/releasing.md`
- Modify: `docs/breadcrumbs.md`

- [ ] Kill leftover runner trees from interrupted validation
- [ ] Cover only HA NOVA-owned runner processes
- [ ] Document when to use it

## Chunk 4: Keep the Release Proof Minimal

### Task 7: Reduce the lane set in docs to the minimum

**Files:**
- Modify: `docs/releasing.md`
- Modify: `docs/superpowers/plans/2026-03-15-desktop-validation.md`
- Modify: client install docs only if support wording changes

- [ ] Document exactly four lanes: verify-safe, macOS desktop, Windows headless, Windows desktop
- [ ] Remove any wording that suggests ad-hoc host testing
- [ ] Keep Windows support claims tied to the desktop lane only

### Task 8: Final review gate

**Files:**
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] Confirm the design stays KISS
- [ ] Confirm no new workflow layer was introduced unnecessarily
- [ ] Confirm default verification is host-safe
- [ ] Record the final lane contract and remaining release blockers
