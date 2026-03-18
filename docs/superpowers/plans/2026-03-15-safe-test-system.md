# Safe Test System Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the default local/CI test system host-safe while keeping explicit macOS and Windows desktop validation available for release proof.

**Architecture:** Split testing into two tiers: safe default and explicit desktop validation. Enforce safety with a minimal shared guard set instead of adding a large orchestration layer. Remove old shell onboarding flows from the default quality gate instead of trying to harden every historical path equally.

**Tech Stack:** npm scripts, Vitest, Go test, Bash, PowerShell, GitHub Actions

---

## Chunk 1: Define Safe Defaults

### Task 1: Redefine the default contract

**Files:**
- Modify: `package.json`
- Modify: `docs/releasing.md`
- Modify: `CONTRIBUTING.md`
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] Add one canonical statement: `npm test` and `npm run verify` must be host-safe.
- [ ] Add explicit scripts for desktop/manual validation instead of overloading `npm test`.
- [ ] Document that desktop validation is required before release but never part of default local/CI verification.

### Task 2: Inventory and remove risky default tests

**Files:**
- Modify: `package.json`
- Modify: `tests/onboarding/macos-onboarding-script-contract.test.ts`
- Modify: any other test files still shelling into `scripts/onboarding/macos-setup.sh` or equivalent browser/keychain paths

- [ ] Find every test still launching old shell onboarding flows.
- [ ] Remove them from the default `npm test` path.
- [ ] Keep only contract/static coverage for those legacy paths if still needed.
- [ ] Move any true interactive proof into explicit desktop commands/runbooks instead.

## Chunk 2: Add Small Shared Safety Guards

### Task 3: Add a no-browser guard

**Files:**
- Modify: `cli/commands.go`
- Modify: any shared browser-open helper files still used by shell scripts
- Test: add/update focused contract tests

- [ ] Add one explicit environment guard, `HA_NOVA_NO_BROWSER=1`.
- [ ] Make browser-open helpers return quietly when the guard is set.
- [ ] Ensure all safe/default tests set this guard.

### Task 4: Add a test secret-store guard

**Files:**
- Modify: `cli/keyring_darwin.go`
- Modify: `cli/keyring_windows.go`
- Modify: `cli/keyring_linux.go`
- Modify: any shared keyring helper file if present
- Test: Go tests or contract tests for the override behavior

- [ ] Add one explicit test-mode secret-store override.
- [ ] In safe/default tests, secret writes must go to an isolated test target, not the real host store.
- [ ] Keep the mechanism small: one guard, one alternate path, no extra framework.

## Chunk 3: Separate Desktop Validation Cleanly

### Task 5: Define minimal desktop entrypoints

**Files:**
- Modify: `package.json`
- Modify: `docs/releasing.md`
- Modify: `docs/superpowers/plans/2026-03-15-desktop-validation.md`

- [ ] Add explicit commands for:
  - `test:desktop:macos`
  - `test:desktop:windows:headless`
  - `test:desktop:windows:rdp`
- [ ] Make them thin wrappers around the existing runner scripts.
- [ ] Do not add a generalized test runner abstraction.

### Task 6: Add abort-safe cleanup hooks

**Files:**
- Modify: desktop helper scripts in `scripts/dev/`
- Create or modify: one small kill/cleanup helper for macOS if needed
- Modify: `docs/releasing.md`

- [ ] Ensure each desktop helper uses `trap`/`finally`.
- [ ] Add one documented emergency cleanup command for macOS host-side test leftovers.
- [ ] Add one documented cleanup-first step for Windows VM runs.

## Chunk 4: Align CI and Review Gates

### Task 7: Keep CI safe, not broader

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release-candidate.yml` only if needed for naming/clarity
- Test: `tests/onboarding/release-contract.test.ts`

- [ ] Ensure CI uses only the safe default lane.
- [ ] Do not add desktop/browser/keychain validation to normal CI.
- [ ] Keep RC/release docs explicit that desktop proof remains manual.

### Task 8: Final docs and review sweep

**Files:**
- Modify: `docs/releasing.md`
- Modify: `CONTRIBUTING.md`
- Modify: `README.md` only if it currently misstates contributor verification
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] Remove any wording that suggests `npm test` is allowed to open browsers or touch real secure stores.
- [ ] Document the exact split:
  - safe default
  - explicit macOS desktop
  - explicit Windows headless
  - explicit Windows RDP
- [ ] Re-review the final structure for KISS/DRY and delete any redundant script or doc step introduced during the split.

## Verification

- [ ] Run the new safe default path locally and confirm it performs no browser launches.
- [ ] Run the new safe default path locally and confirm it performs no real keychain/credential-manager writes.
- [ ] Run `cd cli && go test ./...`.
- [ ] Run targeted contract tests for package scripts, release docs, and any new browser/keyring guards.
- [ ] Manually inspect `package.json` and CI workflow usage so desktop commands are not reachable through `npm test` or `npm run verify`.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-03-15-safe-test-system.md`. Ready to execute?
