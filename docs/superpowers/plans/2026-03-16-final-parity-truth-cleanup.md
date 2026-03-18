# Final Parity + Truth Cleanup Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the last behavior/documentation truth gaps around setup, update sync, Claude marketplace refresh, and onboarding/uninstall contracts.

**Architecture:** Keep the current Go runtime. Fix only the remaining branch-specific truth gaps, then tighten docs/contracts so future regressions are caught earlier.

**Tech Stack:** Go CLI, Vitest contracts, markdown docs

---

## Chunk 1: Lock Remaining Truth Gaps in Tests

### Task 1: Add/adjust failing tests

**Files:**
- Modify: `cli/setup_interactive_test.go`
- Create: `cli/claude_marketplace_test.go`
- Create: `cli/update_sync_test.go`
- Modify: `tests/onboarding/client-install-docs-contract.test.ts`
- Create: `tests/onboarding/go-runtime-contract.test.ts`
- Create: `tests/onboarding/onboarding-skill-contract.test.ts`

- [ ] Update the host+relay-token interactive test to expect the old skip-LLAT fast path.
- [ ] Add a Claude marketplace safety test for the normal GitHub path.
- [ ] Add an update-sync test that proves actual installed clients are refreshed even if `state.json` is empty/stale.
- [ ] Add doc/skill contracts for:
  - README truthfulness
  - legacy shell suite classification
  - onboarding skill `/ws` fallback wording

## Chunk 2: Implement Minimal Runtime Fixes

### Task 2: Restore `origin/main` fast path + safer update truth

**Files:**
- Modify: `cli/setup_interactive.go`
- Modify: `cli/setup_state.go`
- Modify: `cli/claude_marketplace.go`
- Modify: `cli/clients.go`
- Modify: `cli/commands.go`

- [ ] Skip LLAT walkthrough when both host and relay token were already supplied.
- [ ] Make resume summary reflect saved token truth separately from relay-health truth.
- [ ] Make the normal Claude GitHub marketplace path non-destructive by default.
- [ ] Refresh update sync from detected real clients, not only `state.json`.

## Chunk 3: Align Docs + Contracts

### Task 3: Remove legacy drift and overclaims

**Files:**
- Modify: `README.md`
- Modify: `docs/reference/skill-architecture.md`
- Modify: `skills/onboarding/SKILL.md`
- Modify: `tests/onboarding/setup-fresh-install.test.ts`
- Modify: `tests/onboarding/setup-resume.test.ts`
- Modify: `tests/onboarding/setup-relay-failures.test.ts`
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] Mark shell onboarding suites as legacy/reference-only.
- [ ] Add current Go-runtime contract coverage.
- [ ] Make README/support language match the real validation story.
- [ ] Update architecture docs for the actual Claude plugin/marketplace path.
- [ ] Update onboarding skill diagnosis wording to `/health` + `/ws` truth.

## Chunk 4: Verify

### Task 4: Run focused + full verification

- [ ] `cd cli && go test ./... -run 'TestInteractiveSetupWithRelayTokenFlagSkipsLLATWalkthrough|TestInstallClaudePluginSkipsMarketplaceRemoveWhenGitHubMarketplaceAlreadyConfigured|TestInstallClaudePluginPreservesExistingMarketplaceWhenGitHubAddFails|TestPostUpdateSyncRefreshesDetectedInstalledClientsWithoutState'`
- [ ] `npx vitest run tests/onboarding/client-install-docs-contract.test.ts tests/onboarding/go-runtime-contract.test.ts tests/onboarding/onboarding-skill-contract.test.ts`
- [ ] `cd cli && go test ./...`
- [ ] `npm run verify`
